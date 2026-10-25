package miner

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"math/big"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// txListTestMaxBytes is one blob's worth of compressed tx-list data, the
// limit proposers request.
const txListTestMaxBytes = (params.BlobTxBytesPerFieldElement - 1) * params.BlobTxFieldElementsPerBlob

// newUnzenTxListTestWorker is newUnzenTestWorker with a transaction pool,
// which the tx-list builder reads pending transactions from. The pool is
// closed before the chain stops.
func newUnzenTxListTestWorker(t *testing.T, gspec *core.Genesis) (*Miner, *testWorkerBackend) {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	eng := taiko.New(gspec.Config, db)
	chain, err := core.NewBlockChain(db, gspec, eng, &core.BlockChainConfig{ArchiveMode: true})
	if err != nil {
		t.Fatalf("core.NewBlockChain failed: %v", err)
	}
	t.Cleanup(chain.Stop)
	pool, err := txpool.New(testTxPoolConfig.PriceLimit, chain, []txpool.SubPool{legacypool.New(testTxPoolConfig, chain)})
	if err != nil {
		t.Fatalf("txpool.New failed: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	backend := &testWorkerBackend{db: db, chain: chain, txPool: pool, genesis: gspec}
	return New(backend, testConfig, eng), backend
}

// signTxListTestTx signs a dynamic-fee transaction for the tx-list tests.
func signTxListTestTx(t *testing.T, config *params.ChainConfig, key *ecdsa.PrivateKey, nonce uint64, to common.Address, gas uint64, tipGwei int64, data []byte) *types.Transaction {
	t.Helper()
	return types.MustSignNewTx(key, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     nonce,
		To:        &to,
		Gas:       gas,
		GasTipCap: big.NewInt(tipGwei * params.GWei),
		GasFeeCap: big.NewInt((tipGwei + 5) * params.GWei),
		Data:      data,
	})
}

// addUserTransfers adds n low-tip transfers from the test bank to the pool.
func addUserTransfers(t *testing.T, b *testWorkerBackend, config *params.ChainConfig, n int) []common.Hash {
	t.Helper()
	var hashes []common.Hash
	for i := range n {
		tx := signTxListTestTx(t, config, testBankKey, uint64(i), testUserAddress, params.TxGas, 1, nil)
		addTxListTestTx(t, b, tx)
		hashes = append(hashes, tx.Hash())
	}
	return hashes
}

func addTxListTestTx(t *testing.T, b *testWorkerBackend, tx *types.Transaction) {
	t.Helper()
	if err := b.txPool.Add([]*types.Transaction{tx}, true)[0]; err != nil {
		t.Fatalf("failed to add tx %v to the pool: %v", tx.Hash(), err)
	}
}

func txListHashes(list *PreBuiltTxList) []common.Hash {
	hashes := make([]common.Hash, len(list.TxList))
	for i, tx := range list.TxList {
		hashes[i] = tx.Hash()
	}
	return hashes
}

func assertSingleTxList(t *testing.T, lists []*PreBuiltTxList, want []common.Hash) {
	t.Helper()
	if len(lists) != 1 {
		t.Fatalf("got %d tx lists, want 1", len(lists))
	}
	got := txListHashes(lists[0])
	if len(got) != len(want) {
		t.Fatalf("tx list has %d transactions %v, want %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tx list = %v, want %v", got, want)
		}
	}
}

func fundedTestKey(t *testing.T, gspec *core.Genesis) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	gspec.Alloc[crypto.PubkeyToAddress(key.PublicKey)] = types.Account{Balance: testBankFunds}
	return key
}

// TestBuildTransactionsLists_SkipsTxTooLargeForAnyList checks that a
// transaction whose compressed size alone exceeds the tx-list limit is left
// out without emptying the list: tail pruning would otherwise remove it
// together with every lower-tip transaction behind it.
func TestBuildTransactionsLists_SkipsTxTooLargeForAnyList(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec := newUnzenTestGenesis(config)
	gspec.GasLimit = 30_000_000
	sender := fundedTestKey(t, gspec)
	w, b := newUnzenTxListTestWorker(t, gspec)

	// Incompressible calldata a little over the limit, within the pool's own
	// size limit, and with the highest tip in the pool.
	data := make([]byte, txListTestMaxBytes+512)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand: %v", err)
	}
	oversized := signTxListTestTx(t, config, sender, 0, testUserAddress, 6_000_000, 15, data)
	addTxListTestTx(t, b, oversized)
	users := addUserTransfers(t, b, config, 5)

	lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
	if err != nil {
		t.Fatalf("BuildTransactionsLists: %v", err)
	}
	assertSingleTxList(t, lists, users)
	if lists[0].BytesLength > txListTestMaxBytes {
		t.Fatalf("tx list is %d bytes, over the %d byte limit", lists[0].BytesLength, txListTestMaxBytes)
	}
}

// TestFitsEmptyTxList pins the exact compressed-size check near the limit and
// the shortcut for transactions far below it.
func TestFitsEmptyTxList(t *testing.T) {
	config := newUnzenTestChainConfig()
	data := make([]byte, 20_000)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand: %v", err)
	}
	tx := signTxListTestTx(t, config, testBankKey, 0, testUserAddress, 1_000_000, 1, data)
	b, err := encodeAndCompressTxList(types.Transactions{tx})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	compressed := uint64(len(b))

	for _, tc := range []struct {
		limit uint64
		want  bool
	}{
		{limit: compressed - 1, want: false},
		{limit: compressed, want: true},
		{limit: tx.Size() + txListZlibGuardBytes + 1, want: true},
	} {
		got, err := fitsEmptyTxList(tx, tc.limit)
		if err != nil {
			t.Fatalf("limit %d: %v", tc.limit, err)
		}
		if got != tc.want {
			t.Fatalf("limit %d (compressed size %d): fits = %v, want %v", tc.limit, compressed, got, tc.want)
		}
	}
}

// TestBuildTransactionsLists_SkipsTxOverZkGasBudget checks that tx-list
// building meters Unzen zk gas: a transaction that needs more than the block
// budget is left out, so the derived block is not cut short before the
// transactions listed after it. Before Unzen there is no budget to meter.
func TestBuildTransactionsLists_SkipsTxOverZkGasBudget(t *testing.T) {
	for _, tc := range []struct {
		name         string
		unzen        bool
		wantIncluded bool
	}{
		{name: "unzen", unzen: true, wantIncluded: false},
		{name: "pre-unzen", unzen: false, wantIncluded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := newUnzenTestChainConfig()
			if !tc.unzen {
				config.UnzenTime = nil
			}
			gspec, keys := newZkGasTestGenesis(t, config, 1)
			w, b := newUnzenTxListTestWorker(t, gspec)

			heavy := signTxListTestTx(t, config, keys[0], 0, hashLoopAddress, 10_000_000, 15, nil)
			addTxListTestTx(t, b, heavy)
			users := addUserTransfers(t, b, config, 5)

			lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
			if err != nil {
				t.Fatalf("BuildTransactionsLists: %v", err)
			}
			want := users
			if tc.wantIncluded {
				want = append([]common.Hash{heavy.Hash()}, users...)
			}
			assertSingleTxList(t, lists, want)
		})
	}
}

// hashLoopAddress holds hashLoopCode in the zk-gas tests below.
var hashLoopAddress = common.HexToAddress("0x000000000000000000000000000000000000c0de")

// hashLoopCode is JUMPDEST PUSH3 0x010000 PUSH1 0 KECCAK256 POP PUSH1 0 JUMP:
// it hashes 64 KiB in a loop until it runs out of gas. Nearly all of that gas
// is KECCAK256, so at its Unzen multiplier of 31, 10M gas is far past the
// zk-gas budget and 1M gas is about 30M zk gas. The tests below that count
// how many such transactions fit depend on that multiplier.
var hashLoopCode = common.FromHex("0x5b6201000060002050600056")

// newZkGasTestGenesis is newUnzenTestGenesis with a 30M gas limit, the hash
// loop contract, and n funded senders.
func newZkGasTestGenesis(t *testing.T, config *params.ChainConfig, n int) (*core.Genesis, []*ecdsa.PrivateKey) {
	t.Helper()
	gspec := newUnzenTestGenesis(config)
	gspec.GasLimit = 30_000_000
	gspec.Alloc[hashLoopAddress] = types.Account{Code: hashLoopCode, Balance: common.Big0}
	keys := make([]*ecdsa.PrivateKey, n)
	for i := range keys {
		keys[i] = fundedTestKey(t, gspec)
	}
	return gspec, keys
}

// TestBuildTransactionsLists_BoundsZkOverflowWork checks the cap on work spent
// running transactions that overflow the zk-gas budget, which are reverted and
// pay nothing: selection stops once their gas limits reach blockMaxGasLimit,
// and those that overflowed with nothing else committed are not run again by
// later calls.
func TestBuildTransactionsLists_BoundsZkOverflowWork(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 4)
	w, b := newUnzenTxListTestWorker(t, gspec)

	var heavy []*types.Transaction
	for i, key := range keys {
		tx := signTxListTestTx(t, config, key, 0, hashLoopAddress, 10_000_000, int64(18-i), nil)
		addTxListTestTx(t, b, tx)
		heavy = append(heavy, tx)
	}
	users := addUserTransfers(t, b, config, 5)
	build := func() []*PreBuiltTxList {
		t.Helper()
		lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
		if err != nil {
			t.Fatalf("BuildTransactionsLists: %v", err)
		}
		return lists
	}

	// The first three overflows add up to the 30M gas limit and stop selection.
	if lists := build(); len(lists) != 0 {
		t.Fatalf("first call: got %d tx lists, want none", len(lists))
	}
	for i, tx := range heavy {
		if got, want := w.txListZkUnfit.contains(tx.Hash()), i < 3; got != want {
			t.Fatalf("heavy tx %d remembered = %v, want %v", i, got, want)
		}
	}
	// The next call skips them unrun, so one overflow is left before the users.
	assertSingleTxList(t, build(), users)
	if !w.txListZkUnfit.contains(heavy[3].Hash()) {
		t.Fatal("last heavy tx should be remembered after the second call")
	}
}

// TestBuildTransactionsLists_ZkOverflowAfterCommitNotRemembered pins what the
// cross-call memory covers: a transaction that overflows after others were
// committed might fit in a block of its own, so it is skipped for this call
// only.
func TestBuildTransactionsLists_ZkOverflowAfterCommitNotRemembered(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 2)
	w, b := newUnzenTxListTestWorker(t, gspec)

	first := signTxListTestTx(t, config, keys[0], 0, testUserAddress, params.TxGas, 20, nil)
	addTxListTestTx(t, b, first)
	heavy := signTxListTestTx(t, config, keys[1], 0, hashLoopAddress, 10_000_000, 15, nil)
	addTxListTestTx(t, b, heavy)
	users := addUserTransfers(t, b, config, 5)

	lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
	if err != nil {
		t.Fatalf("BuildTransactionsLists: %v", err)
	}
	assertSingleTxList(t, lists, append([]common.Hash{first.Hash()}, users...))
	if w.txListZkUnfit.contains(heavy.Hash()) {
		t.Fatal("a tx that overflowed after others were committed must not be remembered")
	}
}

// TestTxListZkUnfitSet_KeepsEntriesWhileInPool pins that remembered
// transactions are dropped only once they leave the pool, never to make
// room: otherwise a sender with more over-budget transactions than the set
// holds could cycle them back in to be run again.
func TestTxListZkUnfitSet_KeepsEntriesWhileInPool(t *testing.T) {
	set := newTxListZkUnfitSet()
	const n = 10_000
	for i := range n {
		set.add(common.BigToHash(big.NewInt(int64(i))))
	}
	set.prune(func(common.Hash) bool { return true })
	for i := range n {
		if !set.contains(common.BigToHash(big.NewInt(int64(i)))) {
			t.Fatalf("entry %d dropped while its transaction is still in the pool", i)
		}
	}
	// Transactions with an odd hash have left the pool.
	set.prune(func(h common.Hash) bool { return h[len(h)-1]%2 == 0 })
	for i := range n {
		if got, want := set.contains(common.BigToHash(big.NewInt(int64(i)))), i%2 == 0; got != want {
			t.Fatalf("entry %d remembered = %v, want %v", i, got, want)
		}
	}
}

// TestBuildTransactionsLists_ForgetsZkUnfitTxOnceItLeavesPool checks that a
// remembered transaction is forgotten when its sender replaces it, and that
// the replacement is evaluated normally.
func TestBuildTransactionsLists_ForgetsZkUnfitTxOnceItLeavesPool(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 1)
	w, b := newUnzenTxListTestWorker(t, gspec)

	heavy := signTxListTestTx(t, config, keys[0], 0, hashLoopAddress, 10_000_000, 15, nil)
	addTxListTestTx(t, b, heavy)
	users := addUserTransfers(t, b, config, 5)
	build := func() []*PreBuiltTxList {
		t.Helper()
		lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
		if err != nil {
			t.Fatalf("BuildTransactionsLists: %v", err)
		}
		return lists
	}

	assertSingleTxList(t, build(), users)
	if !w.txListZkUnfit.contains(heavy.Hash()) {
		t.Fatal("heavy tx should be remembered")
	}
	// Same nonce, a plain transfer with fees bumped past the replacement threshold.
	replacement := signTxListTestTx(t, config, keys[0], 0, testUserAddress, params.TxGas, 17, nil)
	addTxListTestTx(t, b, replacement)
	if b.txPool.Has(heavy.Hash()) {
		t.Fatal("heavy tx should have been replaced in the pool")
	}

	assertSingleTxList(t, build(), append([]common.Hash{replacement.Hash()}, users...))
	if w.txListZkUnfit.contains(heavy.Hash()) {
		t.Fatal("heavy tx should be forgotten once it left the pool")
	}
}

// TestBuildTransactionsLists_UnzenListSealsWithoutTruncation is the end-to-end
// check: a list built under Unzen, sealed after an anchor, keeps every
// transaction and imports. Three ~30M zk-gas transactions fill most of the
// budget; the fourth no longer fits and must be left out, or sealing would
// cut the block before it and drop the transfers listed after it.
func TestBuildTransactionsLists_UnzenListSealsWithoutTruncation(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 4)
	w, b := newUnzenTxListTestWorker(t, gspec)

	var heavy []common.Hash
	for i, key := range keys {
		tx := signTxListTestTx(t, config, key, 0, hashLoopAddress, 1_000_000, int64(18-i), nil)
		addTxListTestTx(t, b, tx)
		heavy = append(heavy, tx.Hash())
	}
	users := addUserTransfers(t, b, config, 5)

	lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
	if err != nil {
		t.Fatalf("BuildTransactionsLists: %v", err)
	}
	assertSingleTxList(t, lists, append(heavy[:3:3], users...))

	parent := b.chain.CurrentBlock()
	baseFee := big.NewInt(params.InitialBaseFee)
	taikoL2Address := core.TaikoTreasuryAddress(config.ChainID)
	anchorTx := types.MustSignNewTx(goldenTouchTestKey(t), types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     0,
		GasTipCap: common.Big0,
		GasFeeCap: baseFee,
		Gas:       taiko.AnchorGasLimit,
		To:        &taikoL2Address,
		Data:      taiko.AnchorSelector,
	})
	txList, err := rlp.EncodeToBytes(append(types.Transactions{anchorTx}, lists[0].TxList...))
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	block, err := w.sealBlockWith(parent, parent.Time+1, 0, &engine.BlockMetadata{
		Beneficiary: testUserAddress,
		GasLimit:    parent.GasLimit,
		Timestamp:   parent.Time + 1,
		TxList:      txList,
		ExtraData:   []byte{},
	}, baseFee, nil)
	if err != nil {
		t.Fatalf("sealBlockWith: %v", err)
	}
	if got, want := len(block.Transactions()), 1+len(lists[0].TxList); got != want {
		t.Fatalf("sealed block has %d transactions, want %d: the list overflowed the zk-gas budget", got, want)
	}
	if zk := block.Difficulty().Uint64(); zk > vm.BlockZkGasLimit {
		t.Fatalf("sealed block zk gas %d over the %d budget", zk, vm.BlockZkGasLimit)
	}
	if n, err := b.chain.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("importing the sealed block failed (inserted %d): %v", n, err)
	}
}

// TestCommitL2Transactions_StoppedZkBudgetSelectsNothing pins that reaching
// the cap on zk-overflow work ends selection for the rest of the call: later
// lists of the same call keep their presets but select nothing new.
func TestCommitL2Transactions_StoppedZkBudgetSelectsNothing(t *testing.T) {
	config := newUnzenTestChainConfig()
	w, b := newUnzenTxListTestWorker(t, newUnzenTestGenesis(config))
	addUserTransfers(t, b, config, 3)

	env, err := w.prepareWork(context.Background(), unzenGenerateParams(b.chain.CurrentBlock()), false)
	if err != nil {
		t.Fatalf("prepareWork: %v", err)
	}
	defer env.discard()
	budget, err := newTxListZkBudget(env, nil, 30_000_000)
	if err != nil {
		t.Fatalf("newTxListZkBudget: %v", err)
	}
	budget.stopped = true

	baseFee := big.NewInt(params.InitialBaseFee)
	signer := types.LatestSigner(config)
	local, remote := w.getPendingTxs(nil, baseFee)
	result, err := w.commitL2Transactions(
		env, nil, nil,
		newTransactionsByPriceAndNonce(signer, local, baseFee),
		newTransactionsByPriceAndNonce(signer, remote, baseFee),
		txListTestMaxBytes, 0, budget,
	)
	if err != nil {
		t.Fatalf("commitL2Transactions: %v", err)
	}
	if len(result.TxsRemaining) != 0 {
		t.Fatalf("a stopped zk budget selected %d transactions", len(result.TxsRemaining))
	}
}

// countTriePrefetcherGoroutines counts the running trie prefetcher workers.
func countTriePrefetcherGoroutines() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), "state.(*subfetcher).loop(")
		}
		buf = make([]byte, 2*len(buf))
	}
}

// settledTriePrefetcherCount returns the prefetcher worker count once it stops
// changing, so that workers stopped by earlier tests have finished exiting.
func settledTriePrefetcherCount() int {
	n := countTriePrefetcherGoroutines()
	for deadline := time.Now().Add(2 * time.Second); n > 0 && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		next := countTriePrefetcherGoroutines()
		if next == n {
			break
		}
		n = next
	}
	return n
}

// waitForTriePrefetcherGoroutines waits until at most want prefetcher workers
// are running. Stopped workers exit asynchronously.
func waitForTriePrefetcherGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := countTriePrefetcherGoroutines()
		if n <= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d trie prefetcher goroutines still running, want at most %d", n, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestBuildTransactionsLists_StopsPrefetcher checks that the trie prefetcher
// started for the simulation is stopped once the lists are built. Its
// workers would otherwise wait for work forever, holding the tries they
// loaded.
func TestBuildTransactionsLists_StopsPrefetcher(t *testing.T) {
	config := newUnzenTestChainConfig()
	w, b := newUnzenTxListTestWorker(t, newUnzenTestGenesis(config))
	addUserTransfers(t, b, config, 3)
	baseline := settledTriePrefetcherCount()

	// The count must see the workers prepareWork starts, or this test checks nothing.
	env, err := w.prepareWork(context.Background(), unzenGenerateParams(b.chain.CurrentBlock()), false)
	if err != nil {
		t.Fatalf("prepareWork: %v", err)
	}
	n := countTriePrefetcherGoroutines()
	env.discard()
	if n <= baseline {
		t.Fatalf("found %d trie prefetcher goroutines after prepareWork, baseline %d: the stack pattern no longer matches", n, baseline)
	}
	waitForTriePrefetcherGoroutines(t, baseline)

	for range 10 {
		lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
		if err != nil {
			t.Fatalf("BuildTransactionsLists: %v", err)
		}
		if len(lists) != 1 || len(lists[0].TxList) != 3 {
			t.Fatalf("unexpected tx lists %v", lists)
		}
	}
	waitForTriePrefetcherGoroutines(t, baseline)
}

// TestSealBlockWith_ErrorStopsPrefetcher checks that sealBlockWith stops the
// prefetcher when it fails after preparing the block. On success computing
// the state root stops it; the error returns used to leave it running.
func TestSealBlockWith_ErrorStopsPrefetcher(t *testing.T) {
	config := newUnzenTestChainConfig()
	w, b := newUnzenTestWorker(t, newUnzenTestGenesis(config))
	parent := b.chain.CurrentBlock()
	baseline := settledTriePrefetcherCount()

	// An anchor with a nonce the golden-touch account has not reached fails,
	// which aborts sealing after prepareWork.
	baseFee := big.NewInt(params.InitialBaseFee)
	taikoL2Address := core.TaikoTreasuryAddress(config.ChainID)
	anchorTx := types.MustSignNewTx(goldenTouchTestKey(t), types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     7,
		GasTipCap: common.Big0,
		GasFeeCap: baseFee,
		Gas:       taiko.AnchorGasLimit,
		To:        &taikoL2Address,
		Data:      taiko.AnchorSelector,
	})
	txList, err := rlp.EncodeToBytes(types.Transactions{anchorTx})
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	for range 5 {
		_, err := w.sealBlockWith(parent, parent.Time+1, 0, &engine.BlockMetadata{
			Beneficiary: testUserAddress,
			GasLimit:    parent.GasLimit,
			Timestamp:   parent.Time + 1,
			TxList:      txList,
			ExtraData:   []byte{},
		}, baseFee, nil)
		if !errors.Is(err, core.ErrNonceTooHigh) {
			t.Fatalf("sealBlockWith: got %v, want %v", err, core.ErrNonceTooHigh)
		}
	}
	waitForTriePrefetcherGoroutines(t, baseline)
}
