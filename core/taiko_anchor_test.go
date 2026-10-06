package core

import (
	"errors"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// anchorTestDust is the golden touch's balance in the anchor tests: far too
// little to buy the anchor's gas, so only the anchor exemption lets it run.
const anchorTestDust = 316_794_861_226

// unsupportedAnchorTxs returns one golden-touch transaction of every type that
// cannot be an anchor.
func unsupportedAnchorTxs(t *testing.T, config *params.ChainConfig) map[string]*types.Transaction {
	t.Helper()
	signer := types.LatestSigner(config)
	treasury := TaikoTreasuryAddress(config.ChainID)
	chainID := uint256.MustFromBig(config.ChainID)
	return map[string]*types.Transaction{
		"legacy": types.MustSignNewTx(etnaTestGoldenTouchKey, signer, &types.LegacyTx{
			GasPrice: big.NewInt(10), Gas: 1_000_000, To: &treasury,
		}),
		"access list": types.MustSignNewTx(etnaTestGoldenTouchKey, signer, &types.AccessListTx{
			ChainID: config.ChainID, GasPrice: big.NewInt(10), Gas: 1_000_000, To: &treasury,
		}),
		"blob": types.MustSignNewTx(etnaTestGoldenTouchKey, signer, &types.BlobTx{
			ChainID: chainID, GasTipCap: uint256.NewInt(0), GasFeeCap: uint256.NewInt(10), Gas: 1_000_000, To: treasury,
			Value: uint256.NewInt(0), BlobFeeCap: uint256.NewInt(1), BlobHashes: []common.Hash{{0x01}},
		}),
		"set code": types.MustSignNewTx(etnaTestGoldenTouchKey, signer, &types.SetCodeTx{
			ChainID: chainID, GasTipCap: uint256.NewInt(0), GasFeeCap: uint256.NewInt(10), Gas: 1_000_000, To: treasury,
			Value: uint256.NewInt(0), AuthList: []types.SetCodeAuthorization{{}},
		}),
	}
}

// TestProcessFlagsAnchorOnMessage pins that block processing grants the
// pre-Etna anchor its exemptions through its message: the golden touch, which
// cannot pay for the anchor's gas, keeps its balance, and the block's
// transaction, which concurrent readers such as the state prefetcher share, is
// left unmarked.
func TestProcessFlagsAnchorOnMessage(t *testing.T) {
	config := etnaProcessConfig(t, 2)
	statedb := newEtnaTestState(t, anchorTestDust)
	tx := goldenTouchTx(t, config, 0, TaikoTreasuryAddress(config.ChainID), 10_000_000, 1_000_000)
	if _, err := processEtnaTestBlock(t, config, statedb, 10_000_000, 25, vm.TxIntrinsicZkGas, tx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := statedb.GetBalance(taiko.GoldenTouchAccount).Uint64(); got != anchorTestDust {
		t.Fatalf("golden touch balance = %d, want the fee-exempt anchor to leave %d", got, uint64(anchorTestDust))
	}
	if tx.IsAnchor() {
		t.Fatal("processing the block marked its shared anchor transaction")
	}
}

// TestProcessRejectsAnchorOfUnsupportedType pins that before Etna a first
// transaction that cannot be an anchor fails the block with
// ErrTxTypeNotSupported, while from Etna on the first position is ordinary.
func TestProcessRejectsAnchorOfUnsupportedType(t *testing.T) {
	t.Run("before etna", func(t *testing.T) {
		config := etnaProcessConfig(t, 2)
		for name, tx := range unsupportedAnchorTxs(t, config) {
			if name == "blob" {
				// Unzen rejects a blob transaction before any anchor handling.
				continue
			}
			t.Run(name, func(t *testing.T) {
				_, err := processEtnaTestBlock(t, config, newEtnaTestState(t, anchorTestDust), 10, 25, vm.TxIntrinsicZkGas, tx)
				if !errors.Is(err, types.ErrTxTypeNotSupported) {
					t.Fatalf("expected %v, got %v", types.ErrTxTypeNotSupported, err)
				}
			})
		}
	})
	t.Run("etna", func(t *testing.T) {
		config := etnaProcessConfig(t, 0)
		tx := unsupportedAnchorTxs(t, config)["legacy"]
		if _, err := processEtnaTestBlock(t, config, newEtnaTestState(t, 1_000_000_000), 10, 25, vm.TxIntrinsicZkGas, tx); err != nil {
			t.Fatalf("process: %v", err)
		}
	})
}

// TestAnchorTransactionToMessage pins that the anchor flag lives on the
// message: converting the anchor flags its message and leaves the transaction
// unmarked, a plain conversion of the same transaction is not flagged, and a
// transaction type that cannot be an anchor is rejected.
func TestAnchorTransactionToMessage(t *testing.T) {
	config := etnaProcessConfig(t, 2)
	signer := types.LatestSigner(config)
	baseFee := big.NewInt(10)

	tx := goldenTouchTreasuryTx(t, config)
	msg, err := AnchorTransactionToMessage(tx, signer, baseFee)
	if err != nil {
		t.Fatalf("AnchorTransactionToMessage: %v", err)
	}
	if !msg.IsAnchor || msg.From != taiko.GoldenTouchAccount {
		t.Fatalf("message from %v, IsAnchor %v; want the golden touch's anchor", msg.From, msg.IsAnchor)
	}
	if tx.IsAnchor() {
		t.Fatal("conversion marked the transaction")
	}
	plain, err := TransactionToMessage(tx, signer, baseFee)
	if err != nil {
		t.Fatalf("TransactionToMessage: %v", err)
	}
	if plain.IsAnchor {
		t.Fatal("a plain conversion is flagged as the anchor")
	}

	for name, tx := range unsupportedAnchorTxs(t, config) {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAnchorTxType(tx); !errors.Is(err, types.ErrTxTypeNotSupported) {
				t.Fatalf("ValidateAnchorTxType: expected %v, got %v", types.ErrTxTypeNotSupported, err)
			}
			if _, err := AnchorTransactionToMessage(tx, signer, baseFee); !errors.Is(err, types.ErrTxTypeNotSupported) {
				t.Fatalf("AnchorTransactionToMessage: expected %v, got %v", types.ErrTxTypeNotSupported, err)
			}
		})
	}
}

// TestApplyAnchorTransaction pins that ApplyAnchorTransaction grants the
// anchor exemptions that ApplyTransaction does not, without marking the
// transaction, and rejects a transaction type that cannot be an anchor.
func TestApplyAnchorTransaction(t *testing.T) {
	config := etnaProcessConfig(t, 2)
	header := &types.Header{
		ParentHash:       etnaTestParentHash,
		Number:           big.NewInt(1),
		Time:             1,
		GasLimit:         30_000_000,
		BaseFee:          big.NewInt(10_000_000),
		Coinbase:         etnaTestCoinbase,
		Difficulty:       common.Big0,
		Extra:            etnaTestExtra(config, 25),
		ParentBeaconRoot: &etnaTestL1StateRoot,
	}
	chain := &etnaTestChainContext{config: config, engine: taiko.New(config, rawdb.NewMemoryDatabase())}
	type applyFunc func(*vm.EVM, *GasPool, *state.StateDB, *types.Header, *types.Transaction) (*types.Receipt, error)
	apply := func(t *testing.T, applyTx applyFunc, tx *types.Transaction) (*types.Receipt, error) {
		t.Helper()
		statedb := newEtnaTestState(t, anchorTestDust)
		statedb.SetTxContext(tx.Hash(), 0)
		evm := vm.NewEVM(NewEVMBlockContext(header, chain, nil), statedb, config, vm.Config{})
		receipt, err := applyTx(evm, NewGasPool(header.GasLimit), statedb, header, tx)
		if err == nil {
			if got := statedb.GetBalance(crypto.PubkeyToAddress(etnaTestGoldenTouchKey.PublicKey)).Uint64(); got != anchorTestDust {
				t.Fatalf("golden touch balance = %d, want the fee-exempt anchor to leave %d", got, uint64(anchorTestDust))
			}
		}
		return receipt, err
	}
	tx := goldenTouchTx(t, config, 0, TaikoTreasuryAddress(config.ChainID), 10_000_000, 1_000_000)
	receipt, err := apply(t, ApplyAnchorTransaction, tx)
	if err != nil {
		t.Fatalf("ApplyAnchorTransaction: %v", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful || receipt.GasUsed != params.TxGas {
		t.Fatalf("receipt status %d, gas used %d; want a successful %d-gas call", receipt.Status, receipt.GasUsed, params.TxGas)
	}
	if tx.IsAnchor() {
		t.Fatal("ApplyAnchorTransaction marked the transaction")
	}
	if _, err := apply(t, ApplyTransaction, tx); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("ApplyTransaction: expected %v, got %v", ErrInsufficientFunds, err)
	}
	if _, err := apply(t, ApplyAnchorTransaction, unsupportedAnchorTxs(t, config)["legacy"]); !errors.Is(err, types.ErrTxTypeNotSupported) {
		t.Fatalf("ApplyAnchorTransaction of a legacy transaction: expected %v, got %v", types.ErrTxTypeNotSupported, err)
	}
}

// TestPrefetchFlagsAnchorOnMessage pins that the state prefetcher executes the
// pre-Etna anchor with its exemptions: the anchor of the golden touch, which
// holds no funds, reaches the EVM and warms the state it touches. From Etna on
// the same first transaction fails to buy gas before execution.
func TestPrefetchFlagsAnchorOnMessage(t *testing.T) {
	for _, tt := range []struct {
		name     string
		etnaTime uint64
		calls    int64
	}{
		{"before etna", 2, 1},
		{"etna", 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := etnaProcessConfig(t, tt.etnaTime)
			db := rawdb.NewMemoryDatabase()
			gspec := &Genesis{Config: config, GasLimit: 30_000_000, BaseFee: big.NewInt(10_000_000)}
			chain, err := NewBlockChain(db, gspec, taiko.New(config, db), DefaultConfig())
			if err != nil {
				t.Fatalf("new blockchain: %v", err)
			}
			defer chain.Stop()
			statedb, err := chain.State()
			if err != nil {
				t.Fatalf("state: %v", err)
			}
			tx := goldenTouchTx(t, config, 0, TaikoTreasuryAddress(config.ChainID), 10_000_000, 1_000_000)
			block := types.NewBlockWithHeader(&types.Header{
				ParentHash: chain.Genesis().Hash(),
				Number:     big.NewInt(1),
				Time:       1,
				GasLimit:   30_000_000,
				BaseFee:    big.NewInt(10_000_000),
				Coinbase:   etnaTestCoinbase,
				Difficulty: common.Big0,
			}).WithBody(types.Body{Transactions: types.Transactions{tx}})

			var calls atomic.Int64
			hooks := &tracing.Hooks{OnEnter: func(depth int, _ byte, _, _ common.Address, _ []byte, _ uint64, _ *big.Int) {
				if depth == 0 {
					calls.Add(1)
				}
			}}
			newStatePrefetcher(config, chain.hc).Prefetch(block, statedb, vm.Config{Tracer: hooks}, nil)
			if got := calls.Load(); got != tt.calls {
				t.Fatalf("prefetch executed %d top-level calls, want %d", got, tt.calls)
			}
			if tx.IsAnchor() {
				t.Fatal("prefetching marked the anchor transaction")
			}
		})
	}
}
