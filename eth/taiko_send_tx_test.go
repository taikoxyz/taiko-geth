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

// newTaikoSendTxBackend is initBackend on a Taiko chain, with the local
// transaction tracker enabled when withLocal is set (the configuration SendTx
// special-cases).
func newTaikoSendTxBackend(t *testing.T, withLocal bool) *EthAPIBackend {
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

	eth := &Ethereum{
		blockchain: chain,
		txPool:     pool,
	}
	if withLocal {
		eth.localTxTracker = locals.New("", time.Minute, &config, pool)
	}
	return &EthAPIBackend{eth: eth}
}

// newOversizedTx returns a transaction from the funded test account whose
// payload exceeds the txpool's size limit.
func newOversizedTx(t *testing.T) *types.Transaction {
	t.Helper()
	tx, err := types.SignTx(
		types.NewTransaction(0, common.Address{}, common.Big0, 4_000_000, big.NewInt(params.GWei), make([]byte, 256*1024)),
		signer,
		key,
	)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tx
}

// TestSendTxTaikoReturnsPoolRejections checks that on Taiko networks SendTx
// reports transactions the pool refuses instead of tracking them as local
// transactions, while resubmitting a transaction the pool already holds still
// succeeds.
func TestSendTxTaikoReturnsPoolRejections(t *testing.T) {
	b := newTaikoSendTxBackend(t, true)
	tracker := b.eth.localTxTracker
	ctx := context.Background()

	unfunded, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	unfundedTx := makeTx(0, nil, nil, unfunded)
	if err := b.SendTx(ctx, unfundedTx); !errors.Is(err, core.ErrInsufficientFunds) {
		t.Fatalf("unfunded sender: got %v, want %v", err, core.ErrInsufficientFunds)
	}
	if tracker.Has(unfundedTx.Hash()) {
		t.Fatal("rejected unfunded tx is tracked as a local tx")
	}

	oversized := newOversizedTx(t)
	if err := b.SendTx(ctx, oversized); !errors.Is(err, txpool.ErrOversizedData) {
		t.Fatalf("oversized tx: got %v, want %v", err, txpool.ErrOversizedData)
	}
	if tracker.Has(oversized.Hash()) {
		t.Fatal("rejected oversized tx is tracked as a local tx")
	}

	accepted := makeTx(0, nil, nil, key)
	if err := b.SendTx(ctx, accepted); err != nil {
		t.Fatalf("first submission: %v", err)
	}
	if err := b.SendTx(ctx, accepted); err != nil {
		t.Fatalf("resubmitting a known tx should succeed on Taiko, got %v", err)
	}
	if !b.eth.txPool.Has(accepted.Hash()) {
		t.Fatal("accepted tx missing from the pool")
	}
	if !tracker.Has(accepted.Hash()) {
		t.Fatal("accepted tx is not tracked as a local tx")
	}

	t.Run("without local tracker", func(t *testing.T) {
		b := newTaikoSendTxBackend(t, false)
		if err := b.SendTx(ctx, newOversizedTx(t)); !errors.Is(err, txpool.ErrOversizedData) {
			t.Fatalf("oversized tx: got %v, want %v", err, txpool.ErrOversizedData)
		}
	})
}
