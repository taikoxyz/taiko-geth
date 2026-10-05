package core

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

// etnaTestChainContext is the minimal chain context Process needs to execute
// one block on top of an empty state.
type etnaTestChainContext struct {
	config *params.ChainConfig
	engine consensus.Engine
}

func (c *etnaTestChainContext) Config() *params.ChainConfig                 { return c.config }
func (c *etnaTestChainContext) CurrentHeader() *types.Header                { return nil }
func (c *etnaTestChainContext) GetHeader(common.Hash, uint64) *types.Header { return nil }
func (c *etnaTestChainContext) GetHeaderByNumber(uint64) *types.Header      { return nil }
func (c *etnaTestChainContext) GetHeaderByHash(common.Hash) *types.Header   { return nil }
func (c *etnaTestChainContext) Engine() consensus.Engine                    { return c.engine }

var (
	// etnaTestGoldenTouchKey is the publicly known golden-touch key.
	etnaTestGoldenTouchKey, _ = crypto.HexToECDSA("92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38")
	etnaTestCoinbase          = common.HexToAddress("0x00000000000000000000000000000000000000bb")
	// etnaTestL1StateRoot stands in for the state root of the L1 block at the
	// final anchorBlockNumber, which an Etna header carries as its parent
	// beacon root.
	etnaTestL1StateRoot = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000e7")
	etnaTestParentHash  = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000a1")
	// etnaTestHasherCode runs JUMPDEST; PUSH3 0x010000; PUSH1 0; KECCAK256;
	// POP; PUSH1 0; JUMP, hashing 64 KiB per iteration until the Unzen zk gas
	// budget runs out.
	etnaTestHasherCode = common.FromHex("0x5b6201000060002050600056")
)

// etnaProcessConfig returns a Taiko config with Shasta and Unzen active from
// genesis and Etna activating at etnaTime.
func etnaProcessConfig(t *testing.T, etnaTime uint64) *params.ChainConfig {
	t.Helper()
	config := unzenTestChainConfig(t)
	zero := uint64(0)
	config.ShastaTime = &zero
	config.EtnaTime = &etnaTime
	return config
}

// newEtnaTestState returns a state in which only the golden touch holds the
// given balance.
func newEtnaTestState(t *testing.T, goldenTouchBalance uint64) *state.StateDB {
	t.Helper()
	// The tests sign with etnaTestGoldenTouchKey as the golden touch. Any other
	// key would silently turn them into ordinary-sender tests.
	golden := crypto.PubkeyToAddress(etnaTestGoldenTouchKey.PublicKey)
	if golden != taiko.GoldenTouchAccount {
		t.Fatalf("test key controls %v, want the golden touch %v", golden, taiko.GoldenTouchAccount)
	}
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	statedb.SetBalance(golden, uint256.NewInt(goldenTouchBalance), tracing.BalanceChangeUnspecified)
	return statedb
}

// etnaTestExtra returns the extraData of block 1 at timestamp 1 for config:
// the 13-byte Etna layout once Etna is active, the 7-byte Shasta layout
// before, with pctg as the base fee sharing percentage.
func etnaTestExtra(config *params.ChainConfig, pctg byte) []byte {
	extra := make([]byte, params.ShastaExtraDataLen)
	if config.IsEtna(1) {
		extra = make([]byte, params.EtnaExtraDataLen)
	}
	extra[0] = pctg
	return extra
}

// processEtnaTestBlock executes block 1 at timestamp 1, on top of
// etnaTestParentHash, with the given base fee, sharing percentage and header
// difficulty, on statedb.
func processEtnaTestBlock(t *testing.T, config *params.ChainConfig, statedb *state.StateDB, baseFee int64, pctg byte, difficulty uint64, txs ...*types.Transaction) (*ProcessResult, error) {
	t.Helper()
	header := &types.Header{
		ParentHash:       etnaTestParentHash,
		Number:           big.NewInt(1),
		Time:             1,
		GasLimit:         30_000_000,
		BaseFee:          big.NewInt(baseFee),
		Coinbase:         etnaTestCoinbase,
		Difficulty:       new(big.Int).SetUint64(difficulty),
		Extra:            etnaTestExtra(config, pctg),
		ParentBeaconRoot: &etnaTestL1StateRoot,
	}
	block := types.NewBlock(header, &types.Body{Transactions: txs}, nil, trie.NewStackTrie(nil))
	chain := &etnaTestChainContext{config: config, engine: taiko.New(config, rawdb.NewMemoryDatabase())}
	return NewStateProcessor(chain).Process(context.Background(), block, statedb, vm.Config{})
}

// goldenTouchTx signs a zero-value golden-touch call to `to` with a zero tip,
// so it pays exactly the base fee.
func goldenTouchTx(t *testing.T, config *params.ChainConfig, nonce uint64, to common.Address, feeCap int64, gas uint64) *types.Transaction {
	t.Helper()
	return types.MustSignNewTx(etnaTestGoldenTouchKey, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     nonce,
		GasTipCap: common.Big0,
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
	})
}

// goldenTouchTreasuryTx signs a zero-value golden-touch transfer to the
// treasury that pays exactly the base fee: the shape of a legacy anchor.
func goldenTouchTreasuryTx(t *testing.T, config *params.ChainConfig) *types.Transaction {
	t.Helper()
	return goldenTouchTx(t, config, 0, TaikoTreasuryAddress(config.ChainID), 10, 1_000_000)
}

// TestProcessEtnaFirstTransactionPaysFees pins that Etna has no anchor
// exemption: a golden-touch transfer to the treasury in the first position
// buys gas and shares its base fee like any transaction, while before Etna the
// same transaction executes as the fee-exempt anchor.
func TestProcessEtnaFirstTransactionPaysFees(t *testing.T) {
	const balance = 1_000_000_000
	golden := crypto.PubkeyToAddress(etnaTestGoldenTouchKey.PublicKey)

	t.Run("etna", func(t *testing.T) {
		config := etnaProcessConfig(t, 0)
		statedb := newEtnaTestState(t, balance)
		if _, err := processEtnaTestBlock(t, config, statedb, 10, 25, vm.TxIntrinsicZkGas, goldenTouchTreasuryTx(t, config)); err != nil {
			t.Fatalf("process: %v", err)
		}
		// 21,000 gas at the 10 wei base fee: 75% to the treasury, 25% to the coinbase.
		if got := statedb.GetBalance(golden).Uint64(); got != balance-210_000 {
			t.Fatalf("golden touch balance = %d, want %d", got, balance-210_000)
		}
		if got := statedb.GetBalance(TaikoTreasuryAddress(config.ChainID)).Uint64(); got != 157_500 {
			t.Fatalf("treasury balance = %d, want 157500", got)
		}
		if got := statedb.GetBalance(etnaTestCoinbase).Uint64(); got != 52_500 {
			t.Fatalf("coinbase balance = %d, want 52500", got)
		}
	})
	t.Run("before etna", func(t *testing.T) {
		config := etnaProcessConfig(t, 2)
		statedb := newEtnaTestState(t, balance)
		if _, err := processEtnaTestBlock(t, config, statedb, 10, 25, vm.TxIntrinsicZkGas, goldenTouchTreasuryTx(t, config)); err != nil {
			t.Fatalf("process: %v", err)
		}
		if got := statedb.GetBalance(golden).Uint64(); got != balance {
			t.Fatalf("golden touch balance = %d, want the fee-exempt anchor to leave %d", got, balance)
		}
		if got := statedb.GetBalance(TaikoTreasuryAddress(config.ChainID)); !got.IsZero() {
			t.Fatalf("treasury balance = %v, want 0", got)
		}
		if got := statedb.GetBalance(etnaTestCoinbase); !got.IsZero() {
			t.Fatalf("coinbase balance = %v, want 0", got)
		}
	})
}

// TestProcessEtnaGoldenTouchNeedsFunds pins that under Etna the golden touch
// goes through the balance check like any sender: with the dust balance it
// holds on mainnet it cannot buy 1,000,000 gas at 10,000,000 wei, so the block
// is rejected. Before Etna the same transaction is the fee-exempt anchor.
func TestProcessEtnaGoldenTouchNeedsFunds(t *testing.T) {
	const dust = 316_794_861_226
	treasury := func(config *params.ChainConfig) common.Address { return TaikoTreasuryAddress(config.ChainID) }

	t.Run("etna", func(t *testing.T) {
		config := etnaProcessConfig(t, 0)
		tx := goldenTouchTx(t, config, 0, treasury(config), 10_000_000, 1_000_000)
		_, err := processEtnaTestBlock(t, config, newEtnaTestState(t, dust), 10_000_000, 25, vm.TxIntrinsicZkGas, tx)
		if !errors.Is(err, ErrInsufficientFunds) {
			t.Fatalf("expected %v, got %v", ErrInsufficientFunds, err)
		}
	})
	t.Run("before etna", func(t *testing.T) {
		config := etnaProcessConfig(t, 2)
		statedb := newEtnaTestState(t, dust)
		tx := goldenTouchTx(t, config, 0, treasury(config), 10_000_000, 1_000_000)
		if _, err := processEtnaTestBlock(t, config, statedb, 10_000_000, 25, vm.TxIntrinsicZkGas, tx); err != nil {
			t.Fatalf("process: %v", err)
		}
		if got := statedb.GetBalance(crypto.PubkeyToAddress(etnaTestGoldenTouchKey.PublicKey)).Uint64(); got != dust {
			t.Fatalf("golden touch balance = %d, want the fee-exempt anchor to leave %d", got, uint64(dust))
		}
	})
}

// TestProcessEtnaFirstTransactionZkExhaustionRejectsBlock pins how import
// handles a first transaction that exhausts the zk gas budget. Etna can
// truncate at index 0, so execution ends before it and a body that still
// carries it is rejected. Before Etna it is the anchor, which is never
// truncated, so the block fails with the zk gas error itself. Both are zk-gas
// validation errors.
func TestProcessEtnaFirstTransactionZkExhaustionRejectsBlock(t *testing.T) {
	process := func(t *testing.T, etnaTime uint64) error {
		t.Helper()
		config := etnaProcessConfig(t, etnaTime)
		statedb := newEtnaTestState(t, 1_000_000_000_000)
		hasher := common.HexToAddress("0x0000000000000000000000000000000000000022")
		statedb.SetCode(hasher, etnaTestHasherCode, tracing.CodeChangeGenesis)
		tx := goldenTouchTx(t, config, 0, hasher, 10, 5_000_000)
		_, err := processEtnaTestBlock(t, config, statedb, 10, 0, 0, tx)
		return err
	}

	t.Run("etna", func(t *testing.T) {
		err := process(t, 0)
		if !errors.Is(err, ErrZkGasBodyPastTruncation) || !IsZkGasValidationError(err) {
			t.Fatalf("expected the truncated body to be rejected, got %v", err)
		}
	})
	t.Run("before etna", func(t *testing.T) {
		err := process(t, 2)
		if !errors.Is(err, vm.ErrZkGasLimitExceeded) || !strings.Contains(err.Error(), "could not apply tx 0") || !IsZkGasValidationError(err) {
			t.Fatalf("expected the anchor's zk gas error to reject the block, got %v", err)
		}
	})
}

// TestProcessEtnaFeeShareAboveHundredSaturates pins that from Etna on a base
// fee sharing percentage above 100 leaves the treasury share at zero instead
// of wrapping around, while the coinbase still receives its full share. Before
// Etna the treasury share still wraps around.
func TestProcessEtnaFeeShareAboveHundredSaturates(t *testing.T) {
	const balance = 1_000_000_000
	golden := crypto.PubkeyToAddress(etnaTestGoldenTouchKey.PublicKey)

	t.Run("etna", func(t *testing.T) {
		config := etnaProcessConfig(t, 0)
		statedb := newEtnaTestState(t, balance)
		if _, err := processEtnaTestBlock(t, config, statedb, 10, 200, vm.TxIntrinsicZkGas, goldenTouchTreasuryTx(t, config)); err != nil {
			t.Fatalf("process: %v", err)
		}
		// The sender pays 21,000 gas at the 10 wei base fee, yet the coinbase is
		// credited 200% of that.
		if got := statedb.GetBalance(golden).Uint64(); got != balance-210_000 {
			t.Fatalf("golden touch balance = %d, want %d", got, balance-210_000)
		}
		if got := statedb.GetBalance(TaikoTreasuryAddress(config.ChainID)); !got.IsZero() {
			t.Fatalf("treasury balance = %v, want 0", got)
		}
		if got := statedb.GetBalance(etnaTestCoinbase).Uint64(); got != 420_000 {
			t.Fatalf("coinbase balance = %d, want 420000", got)
		}
	})
	t.Run("before etna", func(t *testing.T) {
		// Index 0 is the fee-exempt anchor; the second transaction pays fees.
		config := etnaProcessConfig(t, 2)
		statedb := newEtnaTestState(t, balance)
		treasury := TaikoTreasuryAddress(config.ChainID)
		txs := []*types.Transaction{
			goldenTouchTreasuryTx(t, config),
			goldenTouchTx(t, config, 1, treasury, 10, 1_000_000),
		}
		if _, err := processEtnaTestBlock(t, config, statedb, 10, 200, 2*vm.TxIntrinsicZkGas, txs...); err != nil {
			t.Fatalf("process: %v", err)
		}
		// 210,000 - 420,000 wraps around modulo 2^256.
		want := new(uint256.Int).Sub(uint256.NewInt(210_000), uint256.NewInt(420_000))
		if got := statedb.GetBalance(treasury); !got.Eq(want) {
			t.Fatalf("treasury balance = %v, want the wrapped %v", got, want)
		}
		if got := statedb.GetBalance(etnaTestCoinbase).Uint64(); got != 420_000 {
			t.Fatalf("coinbase balance = %d, want 420000", got)
		}
	})
}

// TestProcessEtnaGoldenTouchPaysAndIsRefunded pins the golden-touch fee
// numbers under Etna at a 10,000,000-wei gas price and base fee, with
// extraData[0] splitting the base fee between the treasury and the coinbase:
//   - a call that clears a storage slot uses 21,206 gas after the refund and
//     debits 212,060,000,000 wei;
//   - a plain call to the treasury with a 1,000,000 gas limit uses 21,000 gas,
//     so the 979,000 unused gas is refunded and it debits 210,000,000,000 wei.
func TestProcessEtnaGoldenTouchPaysAndIsRefunded(t *testing.T) {
	const balance = 100_000_000_000_000
	golden := crypto.PubkeyToAddress(etnaTestGoldenTouchKey.PublicKey)
	clearer := common.HexToAddress("0x00000000000000000000000000000000000000c0")
	tests := []struct {
		name               string
		storageClear       bool
		gas                uint64
		zkGas              uint64 // the call's zk gas: the intrinsic charge plus its opcodes
		pctg               byte
		gasUsed            uint64
		treasury, coinbase uint64
	}{
		{"storage clear, 25%", true, 5_000_000, 268_054, 25, 21_206, 159_045_000_000, 53_015_000_000},
		{"storage clear, 0%", true, 5_000_000, 268_054, 0, 21_206, 212_060_000_000, 0},
		{"treasury call, 25%", false, 1_000_000, vm.TxIntrinsicZkGas, 25, 21_000, 157_500_000_000, 52_500_000_000},
		{"treasury call, 0%", false, 1_000_000, vm.TxIntrinsicZkGas, 0, 21_000, 210_000_000_000, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := etnaProcessConfig(t, 0)
			statedb := newEtnaTestState(t, balance)
			to := TaikoTreasuryAddress(config.ChainID)
			if tt.storageClear {
				to = clearer
				// PUSH1 0; PUSH1 0; SSTORE; STOP clears slot 0, whose committed value is 1.
				statedb.SetCode(clearer, []byte{0x60, 0, 0x60, 0, 0x55, 0}, tracing.CodeChangeGenesis)
				statedb.SetState(clearer, common.Hash{}, common.BigToHash(common.Big1))
				statedb.Finalise(true)
				statedb.IntermediateRoot(true)
			}
			tx := goldenTouchTx(t, config, 0, to, 10_000_000, tt.gas)
			result, err := processEtnaTestBlock(t, config, statedb, 10_000_000, tt.pctg, tt.zkGas, tx)
			if err != nil {
				t.Fatalf("process: %v", err)
			}
			if result.GasUsed != tt.gasUsed {
				t.Fatalf("gas used = %d, want %d", result.GasUsed, tt.gasUsed)
			}
			debit := tt.gasUsed * 10_000_000
			if got := statedb.GetBalance(golden).Uint64(); got != balance-debit {
				t.Fatalf("golden touch balance = %d, want %d", got, balance-debit)
			}
			if got := statedb.GetBalance(TaikoTreasuryAddress(config.ChainID)).Uint64(); got != tt.treasury {
				t.Fatalf("treasury balance = %d, want %d", got, tt.treasury)
			}
			if got := statedb.GetBalance(etnaTestCoinbase).Uint64(); got != tt.coinbase {
				t.Fatalf("coinbase balance = %d, want %d", got, tt.coinbase)
			}
		})
	}
}

// TestProcessEtnaInvalidFirstTransactionRejectsBlock pins that import rejects
// an Etna block whose first transaction is invalid. Only block building skips
// invalid transactions.
func TestProcessEtnaInvalidFirstTransactionRejectsBlock(t *testing.T) {
	config := etnaProcessConfig(t, 0)
	statedb := newEtnaTestState(t, 1_000_000_000)
	tx := goldenTouchTx(t, config, 5, TaikoTreasuryAddress(config.ChainID), 10, 1_000_000) // the account nonce is 0
	_, err := processEtnaTestBlock(t, config, statedb, 10, 0, vm.TxIntrinsicZkGas, tx)
	if !errors.Is(err, ErrNonceTooHigh) || !strings.Contains(err.Error(), "could not apply tx 0") {
		t.Fatalf("expected the block to be rejected with %v, got %v", ErrNonceTooHigh, err)
	}
	if IsZkGasValidationError(err) {
		t.Fatalf("an invalid transaction is not a zk-gas validation error: %v", err)
	}
}

// TestProcessEtnaEmptyBlock pins that an Etna block may be empty. It still runs
// the EIP-4788 system call with the L1 state root and the EIP-2935 system call
// with the parent hash, it uses no gas and no zk gas, and it leaves no
// golden-touch account behind.
func TestProcessEtnaEmptyBlock(t *testing.T) {
	config := etnaProcessConfig(t, 0)
	// Only the two system contracts exist: no golden touch account.
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	statedb.SetCode(params.BeaconRootsAddress, params.BeaconRootsCode, tracing.CodeChangeGenesis)
	statedb.SetCode(params.HistoryStorageAddress, params.HistoryStorageCode, tracing.CodeChangeGenesis)
	// Difficulty 0 is the block's zk gas: any other value fails the import.
	result, err := processEtnaTestBlock(t, config, statedb, 10, 0, 0)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if result.GasUsed != 0 || len(result.Receipts) != 0 {
		t.Fatalf("empty block used %d gas in %d receipts, want none", result.GasUsed, len(result.Receipts))
	}
	// The block timestamp is 1: the contract stores it at slot 1 % 8191 = 1 and
	// the root 8191 slots later.
	if got := statedb.GetState(params.BeaconRootsAddress, common.BigToHash(common.Big1)); got != common.BigToHash(common.Big1) {
		t.Fatalf("timestamp slot = %v, want 1", got)
	}
	if got := statedb.GetState(params.BeaconRootsAddress, common.BigToHash(big.NewInt(1+8191))); got != etnaTestL1StateRoot {
		t.Fatalf("root slot = %v, want %v", got, etnaTestL1StateRoot)
	}
	// The block number is 1: the history contract stores the parent hash at
	// slot (1-1) % 8191 = 0.
	if got := statedb.GetState(params.HistoryStorageAddress, common.Hash{}); got != etnaTestParentHash {
		t.Fatalf("parent hash slot = %v, want %v", got, etnaTestParentHash)
	}
	if statedb.Exist(taiko.GoldenTouchAccount) {
		t.Fatalf("golden touch %v exists after an empty block, want it untouched", taiko.GoldenTouchAccount)
	}
	// Any other header difficulty fails the import as a zk-gas validation error.
	statedb, err = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if _, err := processEtnaTestBlock(t, config, statedb, 10, 0, 1); !errors.Is(err, ErrZkGasDifficultyMismatch) || !IsZkGasValidationError(err) {
		t.Fatalf("expected %v, got %v", ErrZkGasDifficultyMismatch, err)
	}
}
