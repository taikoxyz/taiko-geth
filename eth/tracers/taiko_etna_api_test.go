package tracers

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// etnaTraceTip is the priority fee of the traced golden-touch transaction.
	etnaTraceTip = 7
	// balanceDeltaTracer and balanceDeltaTracerJS name the same tracer,
	// registered for the native and the parallel (JS) block-tracing paths.
	balanceDeltaTracer   = "taikoEtnaBalanceDelta"
	balanceDeltaTracerJS = "taikoEtnaBalanceDeltaJS"
)

var etnaTraceBeneficiary = common.HexToAddress("0x00000000000000000000000000000000000000bb")

// newBalanceDeltaTracer returns a tracer whose result maps every account whose
// balance changed to its net balance change.
func newBalanceDeltaTracer(*Context, json.RawMessage, *params.ChainConfig) (*Tracer, error) {
	deltas := make(map[common.Address]*big.Int)
	return &Tracer{
		Hooks: &tracing.Hooks{
			OnBalanceChange: func(addr common.Address, prev, next *big.Int, _ tracing.BalanceChangeReason) {
				delta, ok := deltas[addr]
				if !ok {
					delta = new(big.Int)
					deltas[addr] = delta
				}
				delta.Add(delta, new(big.Int).Sub(next, prev))
			},
		},
		GetResult: func() (json.RawMessage, error) { return json.Marshal(deltas) },
		Stop:      func(error) {},
	}, nil
}

// etnaTraceMinerBackend serves the chain to the miner that seals the test block.
type etnaTraceMinerBackend struct{ chain *core.BlockChain }

func (b etnaTraceMinerBackend) BlockChain() *core.BlockChain { return b.chain }
func (b etnaTraceMinerBackend) TxPool() *txpool.TxPool       { return nil }

// newEtnaTraceBackend returns a test backend whose chain runs the Taiko engine
// with every fork through Etna active from genesis, and the chain's block 1.
// The block's only transaction is a golden-touch call to the treasury paying
// a 7 wei tip, and its extraData shares pctg percent of the base fee with the
// beneficiary.
func newEtnaTraceBackend(t *testing.T, pctg byte) (*testBackend, *types.Block) {
	t.Helper()
	zero := uint64(0)
	config := *params.TaikoChainConfig
	config.ChainID = big.NewInt(167000)
	config.ShanghaiTime, config.CancunTime, config.PragueTime, config.OsakaTime = &zero, &zero, &zero, &zero
	config.ShastaTime, config.UnzenTime, config.EtnaTime = &zero, &zero, &zero

	goldenKey, err := crypto.HexToECDSA("92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38")
	if err != nil || crypto.PubkeyToAddress(goldenKey.PublicKey) != taiko.GoldenTouchAccount {
		t.Fatalf("golden touch key: %v", err)
	}
	gspec := &core.Genesis{
		Config:    &config,
		Timestamp: 1000,
		GasLimit:  30_000_000,
		BaseFee:   big.NewInt(params.ShastaInitialBaseFee),
		Alloc: types.GenesisAlloc{
			taiko.GoldenTouchAccount:     {Balance: big.NewInt(params.Ether)},
			params.BeaconRootsAddress:    {Nonce: 1, Code: params.BeaconRootsCode},
			params.HistoryStorageAddress: {Nonce: 1, Code: params.HistoryStorageCode},
		},
	}
	db := rawdb.NewMemoryDatabase()
	eng := taiko.New(&config, db)
	chain, err := core.NewBlockChain(db, gspec, eng, &core.BlockChainConfig{ArchiveMode: true})
	if err != nil {
		t.Fatalf("core.NewBlockChain: %v", err)
	}
	t.Cleanup(chain.Stop)

	treasury := core.TaikoTreasuryAddress(config.ChainID)
	golden := types.MustSignNewTx(goldenKey, types.LatestSigner(&config), &types.DynamicFeeTx{
		ChainID: config.ChainID, Nonce: 0, GasTipCap: big.NewInt(etnaTraceTip), GasFeeCap: big.NewInt(2 * params.ShastaInitialBaseFee),
		Gas: 1_000_000, To: &treasury,
	})
	txList, err := rlp.EncodeToBytes(types.Transactions{golden})
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	parent := chain.CurrentBlock()
	root := common.HexToHash("0xe7")
	block, err := miner.New(etnaTraceMinerBackend{chain}, miner.DefaultConfig, eng).SealBlockWith(parent, 0, &engine.PayloadAttributes{
		Timestamp:             parent.Time + 1,
		Random:                common.HexToHash("0x0a"),
		SuggestedFeeRecipient: etnaTraceBeneficiary,
		Withdrawals:           []*types.Withdrawal{},
		BeaconRoot:            &root,
		BaseFeePerGas:         big.NewInt(params.ShastaInitialBaseFee),
		BlockMetadata: &engine.BlockMetadata{
			Beneficiary: etnaTraceBeneficiary,
			GasLimit:    30_000_000,
			Timestamp:   parent.Time + 1,
			TxList:      txList,
			ExtraData:   []byte{pctg, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1},
		},
	})
	if err != nil {
		t.Fatalf("SealBlockWith: %v", err)
	}
	if len(block.Transactions()) != 1 {
		t.Fatalf("sealed %d transactions, want 1", len(block.Transactions()))
	}
	if _, err := chain.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("InsertChain: %v", err)
	}
	return &testBackend{chainConfig: &config, engine: eng, chaindb: db, chain: chain}, block
}

// TestTraceBlockEtnaFirstTransactionPaysFees pins that tracing an Etna block,
// block by block on the native and the parallel path and as part of a chain
// range, charges its first transaction, a golden-touch call to the treasury,
// ordinary fees.
func TestTraceBlockEtnaFirstTransactionPaysFees(t *testing.T) {
	backend, block := newEtnaTraceBackend(t, 0)
	DefaultDirectory.Register(balanceDeltaTracer, newBalanceDeltaTracer, false)
	DefaultDirectory.Register(balanceDeltaTracerJS, newBalanceDeltaTracer, true)
	api := NewAPI(backend)

	gasUsed := new(big.Int).SetUint64(backend.chain.GetReceiptsByHash(block.Hash())[0].GasUsed)
	treasury := core.TaikoTreasuryAddress(backend.chainConfig.ChainID)
	want := map[common.Address]*big.Int{
		taiko.GoldenTouchAccount: new(big.Int).Neg(new(big.Int).Mul(gasUsed, big.NewInt(params.ShastaInitialBaseFee+etnaTraceTip))),
		treasury:                 new(big.Int).Mul(gasUsed, big.NewInt(params.ShastaInitialBaseFee)),
		etnaTraceBeneficiary:     new(big.Int).Mul(gasUsed, big.NewInt(etnaTraceTip)),
	}
	check := func(t *testing.T, results []*txTraceResult) {
		t.Helper()
		if len(results) != 1 || results[0].Error != "" {
			t.Fatalf("results = %+v, want one successful trace", results)
		}
		var got map[common.Address]*big.Int
		if err := json.Unmarshal(results[0].Result.(json.RawMessage), &got); err != nil {
			t.Fatalf("decode trace: %v", err)
		}
		for addr, delta := range want {
			if got[addr] == nil || got[addr].Cmp(delta) != 0 {
				t.Fatalf("balance change of %v = %v, want %v", addr, got[addr], delta)
			}
		}
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
}

// TestIntermediateRootsEtnaFirstTransactionPaysFees pins that the
// intermediate root after an Etna block's only transaction, a golden-touch
// call to the treasury that shares a quarter of its base fee with the
// beneficiary, is the block's state root.
func TestIntermediateRootsEtnaFirstTransactionPaysFees(t *testing.T) {
	backend, block := newEtnaTraceBackend(t, 25)
	roots, err := NewAPI(backend).IntermediateRoots(context.Background(), block.Hash(), nil)
	if err != nil {
		t.Fatalf("IntermediateRoots: %v", err)
	}
	if len(roots) != 1 || roots[0] != block.Root() {
		t.Fatalf("roots = %v, want [%v]", roots, block.Root())
	}
}
