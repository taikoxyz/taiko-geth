package miner

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"math/big"
	mrand "math/rand"
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

// TestBuildTransactionsLists_SkipsTxThatDoesNotFitCurrentList checks that a
// transaction that fits in a list of its own but not after the transactions
// already selected is left out of that list without cutting it short: tail
// pruning would otherwise remove it together with every lower-tip
// transaction behind it. With a second list it goes there.
func TestBuildTransactionsLists_SkipsTxThatDoesNotFitCurrentList(t *testing.T) {
	for _, maxLists := range []uint64{1, 2} {
		config := newUnzenTestChainConfig()
		gspec := newUnzenTestGenesis(config)
		gspec.GasLimit = 30_000_000
		firstKey, largeKey := fundedTestKey(t, gspec), fundedTestKey(t, gspec)
		w, b := newUnzenTxListTestWorker(t, gspec)

		first := signTxListTestTx(t, config, firstKey, 0, testUserAddress, params.TxGas, 20, nil)
		addTxListTestTx(t, b, first)
		// Incompressible calldata just under the limit, ranked right after first.
		data := make([]byte, txListTestMaxBytes-200)
		if _, err := rand.Read(data); err != nil {
			t.Fatalf("rand: %v", err)
		}
		large := signTxListTestTx(t, config, largeKey, 0, testUserAddress, 6_000_000, 19, data)
		if fits, err := fitsEmptyTxList(large, txListTestMaxBytes); err != nil || !fits {
			t.Fatalf("large tx should fit in a list of its own: fits=%v err=%v", fits, err)
		}
		addTxListTestTx(t, b, large)
		users := addUserTransfers(t, b, config, 5)

		lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, maxLists)
		if err != nil {
			t.Fatalf("BuildTransactionsLists: %v", err)
		}
		want := [][]common.Hash{append([]common.Hash{first.Hash()}, users...)}
		if maxLists > 1 {
			want = append(want, []common.Hash{large.Hash()})
		}
		if len(lists) != len(want) {
			t.Fatalf("maxLists %d: got %d tx lists, want %d", maxLists, len(lists), len(want))
		}
		for i := range want {
			assertSingleTxList(t, lists[i:i+1], want[i])
			if lists[i].BytesLength > txListTestMaxBytes {
				t.Fatalf("maxLists %d: tx list %d is %d bytes, over the %d byte limit", maxLists, i, lists[i].BytesLength, txListTestMaxBytes)
			}
		}
	}
}

// TestTxListFit_ListsStayWithinLimit fills lists through txListFit with a mix
// of small, compressible and incompressible transactions. A list must never
// exceed the limit, which is what lets tx-list building skip a transaction
// instead of pruning everything after it, and it must not stop well short of
// the limit.
func TestTxListFit_ListsStayWithinLimit(t *testing.T) {
	config := newUnzenTestChainConfig()
	rng := mrand.New(mrand.NewSource(1))
	var nonce uint64
	newTx := func() *types.Transaction {
		var data []byte
		switch kind := rng.Intn(10); {
		case kind < 5: // transfer
		case kind < 7: // short random calldata
			data = make([]byte, rng.Intn(600))
			rng.Read(data)
		case kind < 9: // compressible calldata
			data = make([]byte, rng.Intn(20_000))
			for i := range data {
				data[i] = byte(i % 7)
			}
		default: // large random calldata
			data = make([]byte, 1_000+rng.Intn(60_000))
			rng.Read(data)
		}
		nonce++
		return signTxListTestTx(t, config, testBankKey, nonce, common.Address{byte(rng.Intn(16))}, 1_000_000, 1, data)
	}

	for trial := range 10 {
		var list types.Transactions
		fit := newTxListFit(list, txListTestMaxBytes)
		for {
			tx := newTx()
			fits, err := fit.fits(list, tx)
			if err != nil {
				t.Fatalf("fits: %v", err)
			}
			if !fits {
				if fit.full() {
					break
				}
				continue
			}
			list = append(list, tx)
			fit.add(tx)
			size, err := compressedTxListSize(list)
			if err != nil {
				t.Fatalf("compress: %v", err)
			}
			if size > txListTestMaxBytes {
				t.Fatalf("trial %d: list of %d txs is %d bytes, over the %d byte limit", trial, len(list), size, txListTestMaxBytes)
			}
		}
		size, err := compressedTxListSize(list)
		if err != nil {
			t.Fatalf("compress: %v", err)
		}
		if size < txListTestMaxBytes-txListTestMaxBytes/32 {
			t.Fatalf("trial %d: list stopped at %d bytes, more than a 32nd below the %d byte limit", trial, size, txListTestMaxBytes)
		}
	}
}

// TestTxListFit_IncompressibleTxAfterCompressibleList pins the case the
// estimate's margin is for. Incompressible data appended to a small
// compressible list compresses to about 3.9 KiB more than its encoded size,
// since zlib cannot store it raw, so the largest such transaction let in must
// still leave the list within the limit.
func TestTxListFit_IncompressibleTxAfterCompressibleList(t *testing.T) {
	config := newUnzenTestChainConfig()
	var nonce uint64
	newTx := func(data []byte) *types.Transaction {
		nonce++
		return signTxListTestTx(t, config, testBankKey, nonce, testUserAddress, 1_000_000, 1, data)
	}
	var list types.Transactions
	fit := newTxListFit(list, txListTestMaxBytes)
	for range 4 {
		data := make([]byte, 500)
		for i := range data {
			data[i] = byte(i * 7 % 13)
		}
		tx := newTx(data)
		if fits, err := fit.fits(list, tx); err != nil || !fits {
			t.Fatalf("small compressible tx should fit: fits=%v err=%v", fits, err)
		}
		list = append(list, tx)
		fit.add(tx)
	}

	data := make([]byte, txListTestMaxBytes)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand: %v", err)
	}
	for n := len(data); n > 0; n -= 256 {
		tx := newTx(data[:n])
		fits, err := fit.fits(list, tx)
		if err != nil {
			t.Fatalf("fits: %v", err)
		}
		if !fits {
			continue
		}
		size, err := compressedTxListSize(append(list, tx))
		if err != nil {
			t.Fatalf("compress: %v", err)
		}
		if size > txListTestMaxBytes {
			t.Fatalf("a %d byte incompressible tx was let in, leaving the list at %d bytes, over the %d byte limit", tx.Size(), size, txListTestMaxBytes)
		}
		return
	}
	t.Fatal("no incompressible tx fit")
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

// zkOverflowState returns the execution state w remembers tx last
// overflowing at.
func zkOverflowState(w *Miner, tx *types.Transaction) (common.Hash, bool) {
	return w.txListZkOverflows.state(tx.Hash())
}

// buildOneTxList calls BuildTransactionsLists for one list with the limits
// the zk-gas tests use.
func buildOneTxList(t *testing.T, w *Miner) []*PreBuiltTxList {
	t.Helper()
	lists, err := w.BuildTransactionsLists(testUserAddress, big.NewInt(params.InitialBaseFee), 30_000_000, txListTestMaxBytes, nil, 1)
	if err != nil {
		t.Fatalf("BuildTransactionsLists: %v", err)
	}
	return lists
}

// TestBuildTransactionsLists_BoundsZkOverflowWork checks the cap on work spent
// running transactions that overflow the zk-gas budget, which are reverted and
// pay nothing: selection stops once their gas limits reach blockMaxGasLimit,
// and those that overflowed are not run again by later calls at the same
// state.
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

	// The first three overflows add up to the 30M gas limit and stop selection.
	if lists := buildOneTxList(t, w); len(lists) != 0 {
		t.Fatalf("first call: got %d tx lists, want none", len(lists))
	}
	for i, tx := range heavy {
		if _, ok := zkOverflowState(w, tx); ok != (i < 3) {
			t.Fatalf("heavy tx %d remembered = %v, want %v", i, ok, i < 3)
		}
	}
	// The next call skips them unrun, so one overflow is left before the users.
	assertSingleTxList(t, buildOneTxList(t, w), users)
	if _, ok := zkOverflowState(w, heavy[3]); !ok {
		t.Fatal("last heavy tx should be remembered after the second call")
	}
}

// TestBuildTransactionsLists_ZkOverflowBehindCommitNotRerun checks that
// over-budget transactions listed behind a committed one stop selection at
// most once: they are remembered with the state they ran at, so the next
// call, with the same transaction ahead of them, skips them unrun and lists
// the lower-tip transactions they kept out.
func TestBuildTransactionsLists_ZkOverflowBehindCommitNotRerun(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 4)
	w, b := newUnzenTxListTestWorker(t, gspec)

	first := signTxListTestTx(t, config, keys[0], 0, testUserAddress, params.TxGas, 20, nil)
	addTxListTestTx(t, b, first)
	var heavy []*types.Transaction
	for i, key := range keys[1:] {
		tx := signTxListTestTx(t, config, key, 0, hashLoopAddress, 10_000_000, int64(18-i), nil)
		addTxListTestTx(t, b, tx)
		heavy = append(heavy, tx)
	}
	users := addUserTransfers(t, b, config, 5)

	// Their gas limits add up to the 30M cap, so the first call stops after them.
	assertSingleTxList(t, buildOneTxList(t, w), []common.Hash{first.Hash()})
	for i, tx := range heavy {
		if _, ok := zkOverflowState(w, tx); !ok {
			t.Fatalf("heavy tx %d should be remembered", i)
		}
	}
	for call := 2; call <= 3; call++ {
		assertSingleTxList(t, buildOneTxList(t, w), append([]common.Hash{first.Hash()}, users...))
	}
}

// TestBuildTransactionsLists_RerunsZkOverflowThatNowFits checks that a
// remembered transaction is run again once the state ahead of it changes,
// here because a transaction ahead makes it cheap, and that a miner that
// remembers it then builds the same list as one that does not.
func TestBuildTransactionsLists_RerunsZkOverflowThatNowFits(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 2)
	// Empty calldata: STOP if slot 0 is set, else hash in a loop like
	// hashLoopCode. Other calldata: set slot 0.
	switchAddress := common.HexToAddress("0x000000000000000000000000000000000000c0df")
	gspec.Alloc[switchAddress] = types.Account{Code: common.FromHex("0x3615600b576001600055005b60005415601457005b6201000060002050601456"), Balance: common.Big0}
	warm, warmBackend := newUnzenTxListTestWorker(t, gspec)
	fresh, freshBackend := newUnzenTxListTestWorker(t, gspec)

	heavy := signTxListTestTx(t, config, keys[0], 0, switchAddress, 10_000_000, 15, nil)
	addTxListTestTx(t, warmBackend, heavy)
	if lists := buildOneTxList(t, warm); len(lists) != 0 {
		t.Fatalf("first call: got %d tx lists, want none", len(lists))
	}
	if _, ok := zkOverflowState(warm, heavy); !ok {
		t.Fatal("heavy tx should be remembered")
	}

	setter := signTxListTestTx(t, config, keys[1], 0, switchAddress, 100_000, 20, []byte{1})
	addTxListTestTx(t, warmBackend, setter)
	addTxListTestTx(t, freshBackend, heavy)
	addTxListTestTx(t, freshBackend, setter)

	want := []common.Hash{setter.Hash(), heavy.Hash()}
	assertSingleTxList(t, buildOneTxList(t, fresh), want)
	assertSingleTxList(t, buildOneTxList(t, warm), want)
	if _, ok := zkOverflowState(warm, heavy); ok {
		t.Fatal("heavy tx should be forgotten once it fits")
	}
}

// TestBuildTransactionsLists_BoundsZkOverflowReruns checks that re-running
// remembered transactions at a changed state never stops selection, and that
// the re-runs a call makes stop once their gas limits reach blockMaxGasLimit.
func TestBuildTransactionsLists_BoundsZkOverflowReruns(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 6)
	w, b := newUnzenTxListTestWorker(t, gspec)

	var heavy []*types.Transaction
	for i, key := range keys[:5] {
		tx := signTxListTestTx(t, config, key, 0, hashLoopAddress, 10_000_000, int64(18-i), nil)
		addTxListTestTx(t, b, tx)
		heavy = append(heavy, tx)
	}
	users := addUserTransfers(t, b, config, 5)

	// The first three stop the first call; the second call runs the other two.
	if lists := buildOneTxList(t, w); len(lists) != 0 {
		t.Fatalf("first call: got %d tx lists, want none", len(lists))
	}
	assertSingleTxList(t, buildOneTxList(t, w), users)
	states := make([]common.Hash, len(heavy))
	for i, tx := range heavy {
		state, ok := zkOverflowState(w, tx)
		if !ok {
			t.Fatalf("heavy tx %d should be remembered", i)
		}
		states[i] = state
	}

	// A new transaction ahead changes the state all five run at. Re-running
	// the first three reaches the 30M allowance; the other two are skipped.
	first := signTxListTestTx(t, config, keys[5], 0, testUserAddress, params.TxGas, 20, nil)
	addTxListTestTx(t, b, first)
	assertSingleTxList(t, buildOneTxList(t, w), append([]common.Hash{first.Hash()}, users...))
	for i, tx := range heavy {
		state, _ := zkOverflowState(w, tx)
		if rerun := state != states[i]; rerun != (i < 3) {
			t.Fatalf("heavy tx %d re-run = %v, want %v", i, rerun, i < 3)
		}
	}
}

// TestNewTxListZkBudget_StateCoversBlockContext checks that the execution
// state a call starts from covers the parent block and the block context the
// simulation sets, so a new head or other request parameters lead to re-runs.
func TestNewTxListZkBudget_StateCoversBlockContext(t *testing.T) {
	config := newUnzenTestChainConfig()
	w, b := newUnzenTxListTestWorker(t, newUnzenTestGenesis(config))
	env, err := w.prepareWork(context.Background(), unzenGenerateParams(b.chain.CurrentBlock()), false)
	if err != nil {
		t.Fatalf("prepareWork: %v", err)
	}
	defer env.discard()
	state := func(gasLimit uint64) common.Hash {
		t.Helper()
		budget, err := newTxListZkBudget(env, nil, gasLimit)
		if err != nil {
			t.Fatalf("newTxListZkBudget: %v", err)
		}
		return budget.state
	}

	base := state(30_000_000)
	if state(30_000_000) != base {
		t.Fatal("same context, different state")
	}
	if state(31_000_000) == base {
		t.Fatal("state ignores the block gas limit")
	}
	for name, change := range map[string]func(){
		"parent":   func() { env.header.ParentHash = common.Hash{1} },
		"coinbase": func() { env.coinbase = common.Address{1} },
		"base fee": func() { env.header.BaseFee = big.NewInt(params.InitialBaseFee + 1) },
	} {
		change()
		next := state(30_000_000)
		if next == base {
			t.Fatalf("state ignores the %s", name)
		}
		base = next
	}
}

// TestTxListZkOverflowSet_KeepsEntriesWhileInPool pins that remembered
// transactions are dropped only once they leave the pool or fit, never to
// make room: otherwise a sender with more over-budget transactions than the
// set holds could cycle them back in to be run as new.
func TestTxListZkOverflowSet_KeepsEntriesWhileInPool(t *testing.T) {
	set := newTxListZkOverflowSet()
	const n = 10_000
	for i := range n {
		set.record(common.BigToHash(big.NewInt(int64(i))), common.Hash{1})
	}
	set.prune(func(common.Hash) bool { return true })
	for i := range n {
		if _, ok := set.state(common.BigToHash(big.NewInt(int64(i)))); !ok {
			t.Fatalf("entry %d dropped while its transaction is still in the pool", i)
		}
	}
	// Transactions with an odd hash have left the pool.
	set.prune(func(h common.Hash) bool { return h[len(h)-1]%2 == 0 })
	for i := range n {
		if _, ok := set.state(common.BigToHash(big.NewInt(int64(i)))); ok != (i%2 == 0) {
			t.Fatalf("entry %d remembered = %v, want %v", i, ok, i%2 == 0)
		}
	}
	set.forget(common.BigToHash(big.NewInt(0)))
	if _, ok := set.state(common.BigToHash(big.NewInt(0))); ok {
		t.Fatal("forgotten entry still remembered")
	}
}

// TestBuildTransactionsLists_ForgetsZkOverflowOnceItLeavesPool checks that a
// remembered transaction is forgotten when its sender replaces it, and that
// the replacement is evaluated normally.
func TestBuildTransactionsLists_ForgetsZkOverflowOnceItLeavesPool(t *testing.T) {
	config := newUnzenTestChainConfig()
	gspec, keys := newZkGasTestGenesis(t, config, 1)
	w, b := newUnzenTxListTestWorker(t, gspec)

	heavy := signTxListTestTx(t, config, keys[0], 0, hashLoopAddress, 10_000_000, 15, nil)
	addTxListTestTx(t, b, heavy)
	users := addUserTransfers(t, b, config, 5)

	assertSingleTxList(t, buildOneTxList(t, w), users)
	if _, ok := zkOverflowState(w, heavy); !ok {
		t.Fatal("heavy tx should be remembered")
	}
	// Same nonce, a plain transfer with fees bumped past the replacement threshold.
	replacement := signTxListTestTx(t, config, keys[0], 0, testUserAddress, params.TxGas, 17, nil)
	addTxListTestTx(t, b, replacement)
	if b.txPool.Has(heavy.Hash()) {
		t.Fatal("heavy tx should have been replaced in the pool")
	}

	assertSingleTxList(t, buildOneTxList(t, w), append([]common.Hash{replacement.Hash()}, users...))
	if _, ok := zkOverflowState(w, heavy); ok {
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
