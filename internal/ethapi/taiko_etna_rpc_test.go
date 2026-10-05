package ethapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/internal/ethapi/override"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// etnaRPCBaseFee is the base fee the calls pay: 21,000 gas at this base fee
// is 210,000,000,000 wei.
const etnaRPCBaseFee = 10_000_000

var (
	etnaRPCCoinbase = common.HexToAddress("0x00000000000000000000000000000000000000bb")
	etnaRPCProbe    = common.HexToAddress("0x0000000000000000000000000000000000000033")
)

// etnaRPCChainConfig returns a Taiko chain config with Shanghai through Osaka,
// Shasta and Unzen active from genesis, and Etna at etnaTime (nil: never).
func etnaRPCChainConfig(etnaTime *uint64) *params.ChainConfig {
	zero := uint64(0)
	config := *params.TaikoChainConfig
	config.ChainID = big.NewInt(167000)
	config.ShanghaiTime, config.CancunTime, config.PragueTime, config.OsakaTime = &zero, &zero, &zero, &zero
	config.ShastaTime, config.UnzenTime = &zero, &zero
	config.EtnaTime = etnaTime
	return &config
}

// etnaRPCProbeCode returns the balances of the treasury and the coinbase, as
// two 32-byte words.
func etnaRPCProbeCode(treasury common.Address) []byte {
	code := append([]byte{0x73}, treasury.Bytes()...) // PUSH20 treasury
	return append(code,
		0x31, 0x60, 0x00, 0x52, // BALANCE PUSH1 0 MSTORE
		0x41, 0x31, 0x60, 0x20, 0x52, // COINBASE BALANCE PUSH1 32 MSTORE
		0x60, 0x40, 0x60, 0x00, 0xf3, // PUSH1 64 PUSH1 0 RETURN
	)
}

// newEtnaRPCTestBackend returns a backend whose chain is only the genesis, on
// the Taiko engine, with the given extraData, the coinbase etnaRPCCoinbase, a
// funded sender and the balance probe.
func newEtnaRPCTestBackend(t *testing.T, config *params.ChainConfig, extra []byte, sender common.Address) *testBackend {
	t.Helper()
	treasury := core.TaikoTreasuryAddress(config.ChainID)
	gspec := &core.Genesis{
		Config:    config,
		Timestamp: 1000,
		GasLimit:  30_000_000,
		BaseFee:   big.NewInt(etnaRPCBaseFee),
		Coinbase:  etnaRPCCoinbase,
		ExtraData: extra,
		Alloc: types.GenesisAlloc{
			sender:                       {Balance: big.NewInt(params.Ether)},
			etnaRPCProbe:                 {Code: etnaRPCProbeCode(treasury)},
			params.BeaconRootsAddress:    {Nonce: 1, Code: params.BeaconRootsCode},
			params.HistoryStorageAddress: {Nonce: 1, Code: params.HistoryStorageCode},
		},
	}
	db := rawdb.NewMemoryDatabase()
	chain, err := core.NewBlockChain(db, gspec, taiko.New(config, db), core.DefaultConfig().WithArchive(true))
	if err != nil {
		t.Fatalf("core.NewBlockChain: %v", err)
	}
	t.Cleanup(chain.Stop)
	return &testBackend{db: db, chain: chain}
}

// etnaRPCTransfer returns a 1-wei transfer that pays the base fee and no tip.
func etnaRPCTransfer(from common.Address) TransactionArgs {
	to := common.HexToAddress("0x00000000000000000000000000000000000000cc")
	gas := hexutil.Uint64(params.TxGas)
	return TransactionArgs{
		From:                 &from,
		To:                   &to,
		Value:                (*hexutil.Big)(common.Big1),
		Gas:                  &gas,
		MaxFeePerGas:         (*hexutil.Big)(big.NewInt(etnaRPCBaseFee)),
		MaxPriorityFeePerGas: (*hexutil.Big)(common.Big0),
	}
}

// TestDoCallEtnaBasefeeSharing pins the base-fee split of eth_call: at an
// Etna block, extraData[0] of a 13-byte extraData goes to the coinbase and the
// rest to the treasury, and an Etna genesis without the layout redistributes
// nothing. Before Etna the whole base fee goes to the treasury.
func TestDoCallEtnaBasefeeSharing(t *testing.T) {
	zero := uint64(0)
	for _, tt := range []struct {
		name               string
		etnaTime           *uint64
		extra              []byte
		coinbase, treasury uint64
	}{
		{"etna fee share", &zero, []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 52_500_000_000, 157_500_000_000},
		{"etna zero fee share", &zero, make([]byte, params.EtnaExtraDataLen), 0, 210_000_000_000},
		{"etna genesis without layout", &zero, nil, 0, 0},
		{"before etna", nil, []byte{25, 0, 0, 0, 0, 0, 1}, 0, 210_000_000_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := newTestAccount().addr
			config := etnaRPCChainConfig(tt.etnaTime)
			b := newEtnaRPCTestBackend(t, config, tt.extra, sender)
			statedb, header, err := b.StateAndHeaderByNumber(context.Background(), rpc.LatestBlockNumber)
			if err != nil {
				t.Fatalf("StateAndHeaderByNumber: %v", err)
			}
			result, err := doCall(context.Background(), b, etnaRPCTransfer(sender), statedb, header, nil, nil, 0, b.RPCGasCap())
			if err != nil || result.Failed() {
				t.Fatalf("doCall: %v, %v", err, result)
			}
			if got := statedb.GetBalance(etnaRPCCoinbase).Uint64(); got != tt.coinbase {
				t.Fatalf("coinbase balance = %d, want %d", got, tt.coinbase)
			}
			if got := statedb.GetBalance(core.TaikoTreasuryAddress(config.ChainID)).Uint64(); got != tt.treasury {
				t.Fatalf("treasury balance = %d, want %d", got, tt.treasury)
			}
		})
	}
}

// TestEstimateGasEtnaGenesis pins that eth_estimateGas runs at an Etna
// genesis that carries no base-fee share. It is a smoke test: a single
// estimate cannot observe its own fee distribution.
func TestEstimateGasEtnaGenesis(t *testing.T) {
	zero := uint64(0)
	sender := newTestAccount().addr
	b := newEtnaRPCTestBackend(t, etnaRPCChainConfig(&zero), nil, sender)
	args := etnaRPCTransfer(sender)
	args.Gas = nil
	latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	gas, err := NewBlockChainAPI(b).EstimateGas(context.Background(), args, &latest, nil, nil)
	if err != nil || uint64(gas) != params.TxGas {
		t.Fatalf("estimate = %d, %v; want %d", gas, err, params.TxGas)
	}
}

// taikoSimulateOverRPC calls eth_simulateV1 with opts on the latest block of
// b over JSON-RPC and returns its error.
func taikoSimulateOverRPC(t *testing.T, b Backend, opts simOpts) error {
	t.Helper()
	server := rpc.NewServer()
	t.Cleanup(server.Stop)
	if err := server.RegisterName("eth", NewBlockChainAPI(b)); err != nil {
		t.Fatalf("register eth API: %v", err)
	}
	client := rpc.DialInProc(server)
	defer client.Close()
	return client.Call(new(json.RawMessage), "eth_simulateV1", opts, "latest")
}

// taikoRPCErrorCode returns the JSON-RPC error code of err, or 0 if it has
// none.
func taikoRPCErrorCode(err error) int {
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.ErrorCode()
	}
	return 0
}

// TestSimulateV1PreEtnaErrorCode pins that a failing pre-Etna simulation
// keeps the default -32000: a pre-Etna block's first transaction must be its
// anchor, so a block with an ordinary call fails to assemble.
func TestSimulateV1PreEtnaErrorCode(t *testing.T) {
	etnaTime := uint64(5000)
	sender := newTestAccount().addr
	b := newEtnaRPCTestBackend(t, etnaRPCChainConfig(&etnaTime), []byte{25, 0, 0, 0, 0, 0, 1}, sender)
	err := taikoSimulateOverRPC(t, b, simOpts{BlockStateCalls: []simBlock{{
		BlockOverrides: &override.BlockOverrides{BaseFeePerGas: (*hexutil.Big)(big.NewInt(etnaRPCBaseFee))},
		Calls:          []TransactionArgs{etnaRPCTransfer(sender)},
	}}})
	if code := taikoRPCErrorCode(err); err == nil || code != -32000 || err.Error() != taiko.ErrAnchorTxNotFound.Error() {
		t.Fatalf("pre-Etna simulation: err = %v (code %d), want %q (code -32000)", err, code, taiko.ErrAnchorTxNotFound)
	}
}

// TestSimulateV1Etna pins eth_simulateV1 for an Etna target: it needs a
// non-zero beaconRoot override, its extraData defaults to the parent-derived
// 13 bytes, and its calls share the base fee as that extraData does.
func TestSimulateV1Etna(t *testing.T) {
	zero := uint64(0)
	root := common.HexToHash("0xe7")
	for _, tt := range []struct {
		name               string
		parentExtra        []byte
		wantExtra          []byte
		coinbase, treasury uint64
	}{
		{"thirteen-byte parent", []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 52_500_000_000, 157_500_000_000},
		{"empty genesis parent", nil, make([]byte, params.EtnaExtraDataLen), 0, 210_000_000_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := newTestAccount().addr
			b := newEtnaRPCTestBackend(t, etnaRPCChainConfig(&zero), tt.parentExtra, sender)
			api := NewBlockChainAPI(b)
			block := func(beaconRoot *common.Hash) simBlock {
				return simBlock{
					BlockOverrides: &override.BlockOverrides{
						BaseFeePerGas: (*hexutil.Big)(big.NewInt(etnaRPCBaseFee)),
						FeeRecipient:  &etnaRPCCoinbase,
						BeaconRoot:    beaconRoot,
					},
					Calls: []TransactionArgs{etnaRPCTransfer(sender), {From: &sender, To: &etnaRPCProbe}},
				}
			}

			const wantErr = "invalid parent beacon root: Etna block 1 requires a non-zero root"
			if _, err := api.SimulateV1(context.Background(), simOpts{BlockStateCalls: []simBlock{block(nil)}}, nil); err == nil || err.Error() != wantErr {
				t.Fatalf("simulating an Etna block without a beacon root: err = %v, want %q", err, wantErr)
			}
			// Like the reference client, the missing root is an internal error,
			// found before the block's calls run: it takes precedence over a
			// call that fails validation.
			zeroRoot := common.Hash{}
			badNonce := block(&zeroRoot)
			badNonce.Calls = []TransactionArgs{etnaRPCTransfer(sender)}
			badNonce.Calls[0].Nonce = (*hexutil.Uint64)(new(uint64))
			*badNonce.Calls[0].Nonce = 99
			for _, tt := range []struct {
				name string
				opts simOpts
			}{
				{"no root", simOpts{BlockStateCalls: []simBlock{block(nil)}}},
				{"zero root", simOpts{BlockStateCalls: []simBlock{block(&zeroRoot)}}},
				{"zero root and a failing call", simOpts{BlockStateCalls: []simBlock{badNonce}, Validation: true}},
			} {
				err := taikoSimulateOverRPC(t, b, tt.opts)
				if code := taikoRPCErrorCode(err); err == nil || err.Error() != wantErr || code != -32603 {
					t.Fatalf("%s over RPC: err = %v (code %d), want %q (code -32603)", tt.name, err, code, wantErr)
				}
			}

			results, err := api.SimulateV1(context.Background(), simOpts{BlockStateCalls: []simBlock{block(&root)}}, nil)
			if err != nil {
				t.Fatalf("SimulateV1: %v", err)
			}
			if got := results[0].Block.Extra(); !bytes.Equal(got, tt.wantExtra) {
				t.Fatalf("extraData = %x, want %x", got, tt.wantExtra)
			}
			probe := results[0].Calls[1].ReturnValue
			if len(probe) != 64 {
				t.Fatalf("probe returned %x, want two words", probe)
			}
			if got := new(big.Int).SetBytes(probe[:32]).Uint64(); got != tt.treasury {
				t.Fatalf("treasury balance = %d, want %d", got, tt.treasury)
			}
			if got := new(big.Int).SetBytes(probe[32:]).Uint64(); got != tt.coinbase {
				t.Fatalf("coinbase balance = %d, want %d", got, tt.coinbase)
			}
		})
	}
}

// TestSimulateV1EtnaExtraDataLength pins that eth_simulateV1 rejects an Etna
// block whose extraData is not 13 bytes with -32603, like the reference
// client's block executor. The block derives its extraData from a 32-byte
// base block, which the derivation passes through unchanged.
func TestSimulateV1EtnaExtraDataLength(t *testing.T) {
	zero := uint64(0)
	root := common.HexToHash("0xe7")
	sender := newTestAccount().addr
	b := newEtnaRPCTestBackend(t, etnaRPCChainConfig(&zero), bytes.Repeat([]byte{25}, 32), sender)
	simulate := func(beaconRoot *common.Hash) error {
		return taikoSimulateOverRPC(t, b, simOpts{BlockStateCalls: []simBlock{{
			BlockOverrides: &override.BlockOverrides{
				BaseFeePerGas: (*hexutil.Big)(big.NewInt(etnaRPCBaseFee)),
				FeeRecipient:  &etnaRPCCoinbase,
				BeaconRoot:    beaconRoot,
			},
			Calls: []TransactionArgs{etnaRPCTransfer(sender)},
		}}})
	}
	const wantErr = "Etna block 1 requires 13-byte extraData, got 32 bytes"
	if err := simulate(&root); err == nil || err.Error() != wantErr || taikoRPCErrorCode(err) != -32603 {
		t.Fatalf("simulating an Etna block with 32-byte extraData: err = %v (code %d), want %q (code -32603)", err, taikoRPCErrorCode(err), wantErr)
	}
	// The root is checked first, as in the reference client.
	const wantRootErr = "invalid parent beacon root: Etna block 1 requires a non-zero root"
	if err := simulate(nil); err == nil || err.Error() != wantRootErr || taikoRPCErrorCode(err) != -32603 {
		t.Fatalf("simulating an Etna block with 32-byte extraData and no root: err = %v (code %d), want %q (code -32603)", err, taikoRPCErrorCode(err), wantRootErr)
	}
}

// TestSimulateV1EtnaActivation pins eth_simulateV1 across the Etna activation:
// a simulated pre-Etna block keeps an empty extraData, and the Etna block
// after it derives its extraData from the base block's 7 bytes, padded to 13,
// so its calls share the base fee as that extraData does.
func TestSimulateV1EtnaActivation(t *testing.T) {
	etnaTime := uint64(1020)
	root := common.HexToHash("0xe7")
	sender := newTestAccount().addr
	b := newEtnaRPCTestBackend(t, etnaRPCChainConfig(&etnaTime), []byte{25, 0, 0, 0, 0, 0, 1}, sender)
	preEtnaTime, etnaBlockTime := hexutil.Uint64(1010), hexutil.Uint64(etnaTime)
	results, err := NewBlockChainAPI(b).SimulateV1(context.Background(), simOpts{BlockStateCalls: []simBlock{
		{BlockOverrides: &override.BlockOverrides{Time: &preEtnaTime}},
		{
			BlockOverrides: &override.BlockOverrides{
				Time:          &etnaBlockTime,
				BaseFeePerGas: (*hexutil.Big)(big.NewInt(etnaRPCBaseFee)),
				FeeRecipient:  &etnaRPCCoinbase,
				BeaconRoot:    &root,
			},
			Calls: []TransactionArgs{etnaRPCTransfer(sender), {From: &sender, To: &etnaRPCProbe}},
		},
	}}, nil)
	if err != nil {
		t.Fatalf("SimulateV1: %v", err)
	}
	if got := results[0].Block.Extra(); len(got) != 0 {
		t.Fatalf("pre-Etna extraData = %x, want empty", got)
	}
	if got, want := results[1].Block.Extra(), []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("Etna extraData = %x, want %x", got, want)
	}
	probe := results[1].Calls[1].ReturnValue
	if len(probe) != 64 {
		t.Fatalf("probe returned %x, want two words", probe)
	}
	if got, want := new(big.Int).SetBytes(probe[:32]).Uint64(), uint64(157_500_000_000); got != want {
		t.Fatalf("treasury balance = %d, want %d", got, want)
	}
	if got, want := new(big.Int).SetBytes(probe[32:]).Uint64(), uint64(52_500_000_000); got != want {
		t.Fatalf("coinbase balance = %d, want %d", got, want)
	}
}

// etnaStateRecordingBackend records the state of the last EVM it creates, so a
// test can read the post-state of eth_createAccessList's final simulation.
type etnaStateRecordingBackend struct {
	*testBackend
	last *state.StateDB
}

func (b *etnaStateRecordingBackend) GetEVM(ctx context.Context, statedb *state.StateDB, header *types.Header, vmConfig *vm.Config, blockCtx *vm.BlockContext) *vm.EVM {
	b.last = statedb
	return b.testBackend.GetEVM(ctx, statedb, header, vmConfig, blockCtx)
}

// TestAccessListEtnaBasefeeSharing pins that eth_createAccessList simulates
// its call with the base-fee split of eth_call at an Etna block.
func TestAccessListEtnaBasefeeSharing(t *testing.T) {
	zero := uint64(0)
	for _, tt := range []struct {
		name               string
		extra              []byte
		coinbase, treasury uint64
	}{
		{"etna fee share", []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 52_500_000_000, 157_500_000_000},
		{"etna genesis without layout", nil, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := newTestAccount().addr
			config := etnaRPCChainConfig(&zero)
			b := &etnaStateRecordingBackend{testBackend: newEtnaRPCTestBackend(t, config, tt.extra, sender)}
			_, _, vmErr, err := AccessList(context.Background(), b, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber), etnaRPCTransfer(sender), nil)
			if err != nil || vmErr != nil {
				t.Fatalf("AccessList: %v, %v", err, vmErr)
			}
			if got := b.last.GetBalance(etnaRPCCoinbase).Uint64(); got != tt.coinbase {
				t.Fatalf("coinbase balance = %d, want %d", got, tt.coinbase)
			}
			if got := b.last.GetBalance(core.TaikoTreasuryAddress(config.ChainID)).Uint64(); got != tt.treasury {
				t.Fatalf("treasury balance = %d, want %d", got, tt.treasury)
			}
		})
	}
}
