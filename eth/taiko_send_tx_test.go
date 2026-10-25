package eth

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/txpool/locals"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// newTaikoSendTxBackend is initBackend on a Taiko chain with the local
// transaction tracker enabled, the configuration SendTx special-cases.
func newTaikoSendTxBackend(t *testing.T) *EthAPIBackend {
	t.Helper()
	config := *params.MergedTestChainConfig
	config.Taiko = true
	genesis := &core.Genesis{
		Config:     &config,
		Alloc:      types.GenesisAlloc{address: {Balance: funds}},
		Difficulty: common.Big0,
		BaseFee:    big.NewInt(params.InitialBaseFee),
	}
	chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), genesis, beacon.New(ethash.NewFaker()), nil)
	if err != nil {
		t.Fatalf("core.NewBlockChain failed: %v", err)
	}
	t.Cleanup(chain.Stop)

	txconfig := legacypool.DefaultConfig
	txconfig.Journal = ""
	pool, err := txpool.New(txconfig.PriceLimit, chain, []txpool.SubPool{legacypool.New(txconfig, chain)})
	if err != nil {
		t.Fatalf("txpool.New failed: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	return &EthAPIBackend{eth: &Ethereum{
		blockchain:     chain,
		txPool:         pool,
		localTxTracker: locals.New("", time.Minute, &config, pool),
	}}
}

// TestSendTxTaikoReturnsPoolRejections checks that on Taiko networks SendTx
// reports transactions the pool refuses instead of tracking them as local
// transactions, while resubmitting a transaction the pool already holds still
// succeeds.
func TestSendTxTaikoReturnsPoolRejections(t *testing.T) {
	b := newTaikoSendTxBackend(t)
	ctx := context.Background()

	unfunded, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := b.SendTx(ctx, makeTx(0, nil, nil, unfunded)); !errors.Is(err, core.ErrInsufficientFunds) {
		t.Fatalf("unfunded sender: got %v, want %v", err, core.ErrInsufficientFunds)
	}

	oversized, err := types.SignTx(
		types.NewTransaction(0, common.Address{}, common.Big0, 4_000_000, big.NewInt(params.GWei), make([]byte, 256*1024)),
		signer,
		key,
	)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := b.SendTx(ctx, oversized); !errors.Is(err, txpool.ErrOversizedData) {
		t.Fatalf("oversized tx: got %v, want %v", err, txpool.ErrOversizedData)
	}

	tx := makeTx(0, nil, nil, key)
	if err := b.SendTx(ctx, tx); err != nil {
		t.Fatalf("first submission: %v", err)
	}
	if err := b.SendTx(ctx, tx); err != nil {
		t.Fatalf("resubmitting a known tx should succeed on Taiko, got %v", err)
	}
	if !b.eth.txPool.Has(tx.Hash()) {
		t.Fatal("accepted tx missing from the pool")
	}
}
