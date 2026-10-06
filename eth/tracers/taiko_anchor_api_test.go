package tracers

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// newAnchorTraceBackend returns a test backend whose chain runs the Taiko
// engine with every fork through Unzen active from genesis and Etna
// unscheduled, and the chain's block 1. The golden touch holds no funds, so
// only the anchor exemption lets the block's first transaction, its anchor,
// execute; the second is a transfer from a funded account. The block shares
// none of its base fee with the beneficiary, so what the tests check does not
// depend on the base-fee share pre-Etna tracing leaves out.
func newAnchorTraceBackend(t *testing.T) (*testBackend, *types.Block) {
	t.Helper()
	zero := uint64(0)
	config := *params.TaikoChainConfig
	config.ChainID = big.NewInt(167000)
	config.ShanghaiTime, config.CancunTime, config.PragueTime, config.OsakaTime = &zero, &zero, &zero, &zero
	config.ShastaTime, config.UnzenTime, config.EtnaTime = &zero, &zero, nil

	goldenKey, err := crypto.HexToECDSA("92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38")
	if err != nil || crypto.PubkeyToAddress(goldenKey.PublicKey) != taiko.GoldenTouchAccount {
		t.Fatalf("golden touch key: %v", err)
	}
	senderKey, _ := crypto.GenerateKey()
	gspec := &core.Genesis{
		Config:    &config,
		Timestamp: 1000,
		GasLimit:  30_000_000,
		BaseFee:   big.NewInt(params.ShastaInitialBaseFee),
		Alloc: types.GenesisAlloc{
			crypto.PubkeyToAddress(senderKey.PublicKey): {Balance: big.NewInt(params.Ether)},
			params.BeaconRootsAddress:                   {Nonce: 1, Code: params.BeaconRootsCode},
			params.HistoryStorageAddress:                {Nonce: 1, Code: params.HistoryStorageCode},
		},
	}
	db := rawdb.NewMemoryDatabase()
	eng := taiko.New(&config, db)
	chain, err := core.NewBlockChain(db, gspec, eng, &core.BlockChainConfig{ArchiveMode: true})
	if err != nil {
		t.Fatalf("core.NewBlockChain: %v", err)
	}
	t.Cleanup(chain.Stop)

	parent := chain.CurrentBlock()
	// Before Etna the sealer recomputes the base fee with EIP-4396, and the
	// anchor must pay exactly that base fee.
	baseFee := misc.CalcEIP4396BaseFee(&config, parent, 0)
	signer := types.LatestSigner(&config)
	taikoL2 := core.TaikoTreasuryAddress(config.ChainID)
	anchor := types.MustSignNewTx(goldenKey, signer, &types.DynamicFeeTx{
		ChainID: config.ChainID, GasTipCap: common.Big0, GasFeeCap: baseFee,
		Gas: taiko.AnchorV3V4GasLimit, To: &taikoL2, Data: taiko.AnchorV4Selector,
	})
	transfer := types.MustSignNewTx(senderKey, signer, &types.DynamicFeeTx{
		ChainID: config.ChainID, GasTipCap: big.NewInt(etnaTraceTip), GasFeeCap: new(big.Int).Mul(baseFee, common.Big2),
		Gas: params.TxGas, To: &etnaTraceBeneficiary, Value: common.Big1,
	})
	txList, err := rlp.EncodeToBytes(types.Transactions{anchor, transfer})
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	block, err := miner.New(etnaTraceMinerBackend{chain}, miner.DefaultConfig, eng).SealBlockWith(parent, 0, &engine.PayloadAttributes{
		Timestamp:             parent.Time + 1,
		Random:                common.HexToHash("0x0a"),
		SuggestedFeeRecipient: etnaTraceBeneficiary,
		Withdrawals:           []*types.Withdrawal{},
		BaseFeePerGas:         baseFee,
		BlockMetadata: &engine.BlockMetadata{
			Beneficiary: etnaTraceBeneficiary,
			GasLimit:    30_000_000,
			Timestamp:   parent.Time + 1,
			MixHash:     common.HexToHash("0x0b"),
			TxList:      txList,
			ExtraData:   []byte{0, 0, 0, 0, 0, 0, 1},
		},
	})
	if err != nil {
		t.Fatalf("SealBlockWith: %v", err)
	}
	if len(block.Transactions()) != 2 {
		t.Fatalf("sealed %d transactions, want 2", len(block.Transactions()))
	}
	if _, err := chain.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("InsertChain: %v", err)
	}
	return &testBackend{chainConfig: &config, engine: eng, chaindb: db, chain: chain}, block
}

// checkAnchorUnmarked fails if the anchor transaction of block, or of the
// chain's copy of it, is marked.
func checkAnchorUnmarked(t *testing.T, backend *testBackend, block *types.Block) {
	t.Helper()
	for _, b := range []*types.Block{block, backend.chain.GetBlockByHash(block.Hash())} {
		if b.Transactions()[0].IsAnchor() {
			t.Fatal("the block's anchor transaction is marked")
		}
	}
}

// checkAnchorTraceExempt fails unless the balance-delta trace result of the
// anchor leaves the golden touch untouched.
func checkAnchorTraceExempt(t *testing.T, result any) {
	t.Helper()
	var deltas map[common.Address]*big.Int
	if err := json.Unmarshal(result.(json.RawMessage), &deltas); err != nil {
		t.Fatalf("decode trace: %v", err)
	}
	if delta := deltas[taiko.GoldenTouchAccount]; delta != nil && delta.Sign() != 0 {
		t.Fatalf("golden touch balance change = %v, want the fee-exempt anchor to leave it untouched", delta)
	}
}

// TestTraceBlockPreEtnaAnchorIsExempt pins that tracing a pre-Etna block,
// block by block on the native and the parallel path and as part of a chain
// range, executes its first transaction as the fee-exempt anchor without
// marking it.
func TestTraceBlockPreEtnaAnchorIsExempt(t *testing.T) {
	backend, block := newAnchorTraceBackend(t)
	DefaultDirectory.Register(balanceDeltaTracer, newBalanceDeltaTracer, false)
	DefaultDirectory.Register(balanceDeltaTracerJS, newBalanceDeltaTracer, true)
	api := NewAPI(backend)

	check := func(t *testing.T, results []*txTraceResult) {
		t.Helper()
		if len(results) != 2 || results[0].Error != "" || results[1].Error != "" {
			t.Fatalf("results = %+v, want two successful traces", results)
		}
		checkAnchorTraceExempt(t, results[0].Result)
	}
	for _, name := range []string{balanceDeltaTracer, balanceDeltaTracerJS} {
		t.Run(name, func(t *testing.T) {
			tracer := name
			results, err := api.TraceBlockByHash(context.Background(), block.Hash(), &TraceConfig{Tracer: &tracer})
			if err != nil {
				t.Fatalf("TraceBlockByHash: %v", err)
			}
			check(t, results)
		})
	}
	t.Run("chain", func(t *testing.T) {
		tracer := balanceDeltaTracer
		genesis := backend.chain.GetBlockByNumber(0)
		var traced int
		for result := range api.traceChain(genesis, block, &TraceConfig{Tracer: &tracer}, nil) {
			check(t, result.Traces)
			traced++
		}
		if traced != 1 {
			t.Fatalf("traced %d blocks, want 1", traced)
		}
	})
	checkAnchorUnmarked(t, backend, block)
}

// TestTraceTransactionPreEtnaAnchorIsExempt pins that tracing the anchor of a
// pre-Etna block on its own executes it as the fee-exempt anchor without
// marking it.
func TestTraceTransactionPreEtnaAnchorIsExempt(t *testing.T) {
	backend, block := newAnchorTraceBackend(t)
	DefaultDirectory.Register(balanceDeltaTracer, newBalanceDeltaTracer, false)

	tracer := balanceDeltaTracer
	result, err := NewAPI(backend).TraceTransaction(context.Background(), block.Transactions()[0].Hash(), &TraceConfig{Tracer: &tracer})
	if err != nil {
		t.Fatalf("TraceTransaction: %v", err)
	}
	checkAnchorTraceExempt(t, result)
	checkAnchorUnmarked(t, backend, block)
}

// TestIntermediateRootsPreEtnaAnchorIsExempt pins that the intermediate root
// after the last transaction of a pre-Etna block, which starts with the
// fee-exempt anchor, is the block's state root.
func TestIntermediateRootsPreEtnaAnchorIsExempt(t *testing.T) {
	backend, block := newAnchorTraceBackend(t)
	roots, err := NewAPI(backend).IntermediateRoots(context.Background(), block.Hash(), nil)
	if err != nil {
		t.Fatalf("IntermediateRoots: %v", err)
	}
	if len(roots) != 2 || roots[1] != block.Root() {
		t.Fatalf("roots = %v, want two ending in %v", roots, block.Root())
	}
	checkAnchorUnmarked(t, backend, block)
}

// TestStandardTraceBlockToFilePreEtnaAnchorIsExempt pins that dumping the
// standard traces of a pre-Etna block executes its first transaction as the
// fee-exempt anchor, whether the anchor is traced or only executed on the way
// to the traced transaction.
func TestStandardTraceBlockToFilePreEtnaAnchorIsExempt(t *testing.T) {
	backend, block := newAnchorTraceBackend(t)
	api := NewAPI(backend)
	for name, tt := range map[string]struct {
		config *StdTraceConfig
		files  int
	}{
		"every transaction": {nil, 2},
		"the transfer only": {&StdTraceConfig{TxHash: block.Transactions()[1].Hash()}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			files, err := api.StandardTraceBlockToFile(context.Background(), block.Hash(), tt.config)
			for _, file := range files {
				os.Remove(file)
			}
			if err != nil {
				t.Fatalf("StandardTraceBlockToFile: %v", err)
			}
			if len(files) != tt.files {
				t.Fatalf("wrote %d trace files, want %d", len(files), tt.files)
			}
		})
	}
	checkAnchorUnmarked(t, backend, block)
}
