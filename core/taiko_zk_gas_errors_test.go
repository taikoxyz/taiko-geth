package core

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

func TestIsZkGasValidationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"limit exceeded", vm.ErrZkGasLimitExceeded, true},
		{"wrapped limit exceeded", fmt.Errorf("could not apply tx 0 [0x01]: %w", vm.ErrZkGasLimitExceeded), true},
		{"body past truncation", fmt.Errorf("%w: body has 1 transactions but execution committed 0", ErrZkGasBodyPastTruncation), true},
		{"difficulty mismatch", fmt.Errorf("%w: header has 1, recomputed 0", ErrZkGasDifficultyMismatch), true},
		{"invalid transaction", fmt.Errorf("could not apply tx 0 [0x01]: %w", ErrNonceTooHigh), false},
		{"same text, different error", errors.New("zk gas limit exceeded"), false},
	}
	for _, tt := range tests {
		if got := IsZkGasValidationError(tt.err); got != tt.want {
			t.Errorf("%s: IsZkGasValidationError(%v) = %v, want %v", tt.name, tt.err, got, tt.want)
		}
	}
}

// insertZkGasTestBlock imports block 1, carrying txs and the given header
// difficulty, on top of a genesis in which the golden touch is funded and
// etnaTestHasherCode is deployed at hasher, and returns the import error.
// Block 1 is canonical in every header field, so only execution can fail it.
func insertZkGasTestBlock(t *testing.T, etnaTime uint64, hasher common.Address, difficulty uint64, txs func(*params.ChainConfig) []*types.Transaction) error {
	t.Helper()
	config := etnaProcessConfig(t, etnaTime)
	gspec := &Genesis{
		Config:   config,
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(params.ShastaInitialBaseFee),
		Alloc: types.GenesisAlloc{
			taiko.GoldenTouchAccount:     {Balance: big.NewInt(1_000_000_000_000_000_000)},
			hasher:                       {Code: etnaTestHasherCode},
			params.BeaconRootsAddress:    {Code: params.BeaconRootsCode},
			params.HistoryStorageAddress: {Code: params.HistoryStorageCode},
		},
	}
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, gspec, taiko.New(config, db), DefaultConfig())
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	defer chain.Stop()

	parent := chain.Genesis()
	root := etnaTestL1StateRoot
	extra := make([]byte, params.ShastaExtraDataLen)
	if config.IsEtna(1) {
		extra = make([]byte, params.EtnaExtraDataLen)
	} else {
		root = common.Hash{}
	}
	zero := uint64(0)
	header := &types.Header{
		ParentHash:       parent.Hash(),
		Number:           big.NewInt(1),
		Time:             parent.Time() + 1,
		GasLimit:         30_000_000,
		BaseFee:          big.NewInt(params.ShastaInitialBaseFee),
		Coinbase:         etnaTestCoinbase,
		Difficulty:       new(big.Int).SetUint64(difficulty),
		Extra:            extra,
		ParentBeaconRoot: &root,
		BlobGasUsed:      &zero,
		ExcessBlobGas:    &zero,
		RequestsHash:     &types.EmptyRequestsHash,
	}
	block := types.NewBlock(header, &types.Body{Transactions: txs(config), Withdrawals: types.Withdrawals{}}, nil, trie.NewStackTrie(nil))
	_, err = chain.InsertBlockWithoutSetHead(context.Background(), block, false)
	return err
}

// TestIsZkGasValidationErrorThroughInsertion pins that the zk-gas failures of
// block import stay recognisable through BlockChain.InsertBlockWithoutSetHead,
// before and from Etna on, and that other execution failures do not match.
func TestIsZkGasValidationErrorThroughInsertion(t *testing.T) {
	hasher := common.HexToAddress("0x0000000000000000000000000000000000000022")
	treasury := func(config *params.ChainConfig) common.Address { return TaikoTreasuryAddress(config.ChainID) }
	feeCap := int64(params.ShastaInitialBaseFee)

	tests := []struct {
		name       string
		etnaTime   uint64
		difficulty uint64
		txs        func(*params.ChainConfig) []*types.Transaction
		wantIs     error
		wantZkGas  bool
	}{
		{
			name: "etna exhaustion at index 0", etnaTime: 0,
			txs: func(c *params.ChainConfig) []*types.Transaction {
				return []*types.Transaction{goldenTouchTx(t, c, 0, hasher, feeCap, 5_000_000)}
			},
			wantIs: ErrZkGasBodyPastTruncation, wantZkGas: true,
		},
		{
			name: "pre-etna exhaustion by the anchor", etnaTime: 100,
			txs: func(c *params.ChainConfig) []*types.Transaction {
				return []*types.Transaction{goldenTouchTx(t, c, 0, hasher, feeCap, 5_000_000)}
			},
			wantIs: vm.ErrZkGasLimitExceeded, wantZkGas: true,
		},
		{
			name: "pre-etna exhaustion at index 1", etnaTime: 100, difficulty: vm.TxIntrinsicZkGas,
			txs: func(c *params.ChainConfig) []*types.Transaction {
				return []*types.Transaction{
					goldenTouchTx(t, c, 0, treasury(c), feeCap, 1_000_000),
					goldenTouchTx(t, c, 1, hasher, feeCap, 5_000_000),
				}
			},
			wantIs: ErrZkGasBodyPastTruncation, wantZkGas: true,
		},
		{
			name: "etna difficulty mismatch", etnaTime: 0, difficulty: vm.TxIntrinsicZkGas + 1,
			txs: func(c *params.ChainConfig) []*types.Transaction {
				return []*types.Transaction{goldenTouchTx(t, c, 0, treasury(c), feeCap, 1_000_000)}
			},
			wantIs: ErrZkGasDifficultyMismatch, wantZkGas: true,
		},
		{
			name: "pre-etna difficulty mismatch", etnaTime: 100, difficulty: vm.TxIntrinsicZkGas + 1,
			txs: func(c *params.ChainConfig) []*types.Transaction {
				return []*types.Transaction{goldenTouchTx(t, c, 0, treasury(c), feeCap, 1_000_000)}
			},
			wantIs: ErrZkGasDifficultyMismatch, wantZkGas: true,
		},
		{
			name: "etna invalid first transaction", etnaTime: 0, difficulty: vm.TxIntrinsicZkGas,
			txs: func(c *params.ChainConfig) []*types.Transaction {
				return []*types.Transaction{goldenTouchTx(t, c, 5, treasury(c), feeCap, 1_000_000)}
			},
			wantIs: ErrNonceTooHigh, wantZkGas: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := insertZkGasTestBlock(t, tt.etnaTime, hasher, tt.difficulty, tt.txs)
			if !errors.Is(err, tt.wantIs) {
				t.Fatalf("import error = %v, want %v", err, tt.wantIs)
			}
			if got := IsZkGasValidationError(err); got != tt.wantZkGas {
				t.Fatalf("IsZkGasValidationError(%v) = %v, want %v", err, got, tt.wantZkGas)
			}
		})
	}
}
