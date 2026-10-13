package miner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"math"
	"math/big"
	"runtime"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// preselectionTestBaseFee is the base fee the preselection tests simulate
// with: 21,000 gas at this base fee is 210,000,000,000 wei.
const preselectionTestBaseFee = 10_000_000

var (
	preselectionTestBeneficiary = common.HexToAddress("0x00000000000000000000000000000000000000bb")
	preselectionTestMixDigest   = common.HexToHash("0x0c")
	preselectionTestCallerKey   = mustPreselectionKey("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
)

func mustPreselectionKey(hex string) *ecdsa.PrivateKey {
	key, err := crypto.HexToECDSA(hex)
	if err != nil {
		panic(err)
	}
	return key
}

// newPreselectionTestGenesis extends the Etna test genesis with the EIP-2935
// history contract, a non-zero mix digest, the given extraData, a funded
// second sender, and a gas limit the pool admits the zk gas hasher call under.
func newPreselectionTestGenesis(config *params.ChainConfig, extra []byte) *core.Genesis {
	gspec := newEtnaTestGenesis(config)
	gspec.GasLimit = 30_000_000
	gspec.Alloc[params.HistoryStorageAddress] = types.Account{Code: params.HistoryStorageCode, Balance: common.Big0}
	gspec.Alloc[crypto.PubkeyToAddress(preselectionTestCallerKey.PublicKey)] = types.Account{Balance: testBankFunds}
	gspec.Mixhash = preselectionTestMixDigest
	gspec.ExtraData = extra
	return gspec
}

// newPreselectionTestWorker builds a miner on the Taiko consensus engine with a
// transaction pool, and adds txs to the pool.
func newPreselectionTestWorker(t *testing.T, gspec *core.Genesis, txs ...*types.Transaction) *Miner {
	t.Helper()
	_, b := newUnzenTestWorker(t, gspec)
	pool := legacypool.New(testTxPoolConfig, b.chain)
	txPool, err := txpool.New(testTxPoolConfig.PriceLimit, b.chain, []txpool.SubPool{pool})
	if err != nil {
		t.Fatalf("txpool.New: %v", err)
	}
	// Cleanups run last-in first-out: the pool stops before the chain.
	t.Cleanup(func() { txPool.Close() })
	b.txPool = txPool
	for i, err := range txPool.Add(txs, true) {
		if err != nil {
			t.Fatalf("adding transaction %d to the pool: %v", i, err)
		}
	}
	return New(b, testConfig, b.chain.Engine())
}

// preselectionTransfer signs a 1-wei transfer paying the given tip on top of
// the preselection base fee.
func preselectionTransfer(t *testing.T, config *params.ChainConfig, key *ecdsa.PrivateKey, nonce uint64, tip int64) *types.Transaction {
	t.Helper()
	return types.MustSignNewTx(key, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(tip),
		GasFeeCap: big.NewInt(preselectionTestBaseFee + tip),
		Gas:       params.TxGas,
		To:        &testUserAddress,
		Value:     common.Big1,
	})
}

// listHashes returns the transaction hashes of every list, in order.
func listHashes(lists []*PreBuiltTxList) [][]common.Hash {
	hashes := make([][]common.Hash, len(lists))
	for i, list := range lists {
		for _, tx := range list.TxList {
			hashes[i] = append(hashes[i], tx.Hash())
		}
	}
	return hashes
}

func TestEtnaPreselectionGasLimit(t *testing.T) {
	for _, tt := range []struct {
		name     string
		baseFee  *big.Int
		gasLimit uint64
		lists    uint64
		want     uint64 // ignored when wantErr is set
		wantErr  bool
	}{
		{"single list", big.NewInt(1), 30_000_000, 1, 30_000_000, false},
		{"combined lists", big.NewInt(1), 30_000_000, 3, 90_000_000, false},
		{"largest product", big.NewInt(1), math.MaxUint64, 1, math.MaxUint64, false},
		{"zero lists", big.NewInt(1), 30_000_000, 0, 0, true},
		{"product overflows", big.NewInt(1), math.MaxUint64, 2, 0, true},
		{"missing base fee", nil, 30_000_000, 1, 0, true},
		{"base fee above 64 bits", new(big.Int).Lsh(common.Big1, 64), 30_000_000, 1, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := etnaPreselectionGasLimit(tt.baseFee, tt.gasLimit, tt.lists)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidPreselectionParams) {
					t.Fatalf("error = %v, want ErrInvalidPreselectionParams", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("gas limit = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

// TestPrepareEtnaPreselectionWork_Environment pins the simulated child of an
// Etna parent: the parent's timestamp and mix digest, the caller's
// beneficiary, base fee and combined gas limit, no parent beacon root, a zk gas
// meter with nothing reserved, and no EIP-4788 or EIP-2935 system call.
func TestPrepareEtnaPreselectionWork_Environment(t *testing.T) {
	config := newEtnaTestChainConfig()
	w := newPreselectionTestWorker(t, newPreselectionTestGenesis(config, nil))
	parent := w.chain.CurrentBlock()

	env, err := w.prepareEtnaPreselectionWork(parent, preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 90_000_000)
	if err != nil {
		t.Fatalf("prepareEtnaPreselectionWork: %v", err)
	}
	defer env.discard()

	if env.header.Time != parent.Time || env.evm.Context.Time != parent.Time {
		t.Fatalf("time = %d (EVM %d), want the parent's %d", env.header.Time, env.evm.Context.Time, parent.Time)
	}
	if env.header.Number.Uint64() != parent.Number.Uint64()+1 {
		t.Fatalf("number = %v, want %d", env.header.Number, parent.Number.Uint64()+1)
	}
	if env.evm.Context.Coinbase != preselectionTestBeneficiary {
		t.Fatalf("coinbase = %v, want %v", env.evm.Context.Coinbase, preselectionTestBeneficiary)
	}
	if env.evm.Context.Random == nil || *env.evm.Context.Random != preselectionTestMixDigest {
		t.Fatalf("PREVRANDAO = %v, want the parent's mix digest %v", env.evm.Context.Random, preselectionTestMixDigest)
	}
	if env.evm.Context.GasLimit != 90_000_000 {
		t.Fatalf("GASLIMIT = %d, want the combined 90000000", env.evm.Context.GasLimit)
	}
	if env.evm.Context.BaseFee.Cmp(big.NewInt(preselectionTestBaseFee)) != 0 {
		t.Fatalf("base fee = %v, want %d", env.evm.Context.BaseFee, preselectionTestBaseFee)
	}
	if env.header.ParentBeaconRoot != nil {
		t.Fatalf("parent beacon root = %v, want none", env.header.ParentBeaconRoot)
	}
	meter := env.evm.Config.ZkGasMeter
	if meter == nil || meter.Schedule().BlockLimit != vm.UnzenZkGasSchedule.BlockLimit || meter.BlockZkGasUsed() != 0 {
		t.Fatalf("zk gas meter = %+v, want a fresh Unzen meter", meter)
	}
	for _, slot := range []common.Hash{beaconRootsSlot(parent.Time), beaconRootStorageSlot(parent.Time)} {
		if got := env.state.GetState(params.BeaconRootsAddress, slot); got != (common.Hash{}) {
			t.Fatalf("EIP-4788 slot %v = %v, want untouched", slot, got)
		}
	}
	historySlot := common.BigToHash(new(big.Int).SetUint64(parent.Number.Uint64() % params.HistoryServeWindow))
	if got := env.state.GetState(params.HistoryStorageAddress, historySlot); got != (common.Hash{}) {
		t.Fatalf("EIP-2935 slot %v = %v, want untouched", historySlot, got)
	}

	// Control: sealing the same child writes both system contracts, so the
	// checks above are not vacuous.
	genParams := unzenGenerateParams(parent)
	root := etnaTestRoot
	genParams.beaconRoot = &root
	sealEnv, err := w.prepareWork(context.Background(), genParams, false)
	if err != nil {
		t.Fatalf("prepareWork: %v", err)
	}
	defer sealEnv.discard()
	if got := sealEnv.state.GetState(params.BeaconRootsAddress, beaconRootStorageSlot(genParams.timestamp)); got != etnaTestRoot {
		t.Fatalf("sealing EIP-4788 root slot = %v, want %v", got, etnaTestRoot)
	}
	if got := sealEnv.state.GetState(params.HistoryStorageAddress, historySlot); got != parent.Hash() {
		t.Fatalf("sealing EIP-2935 slot = %v, want the parent hash %v", got, parent.Hash())
	}
}

// TestPrepareEtnaPreselectionWork_ExtraData pins that the simulated child
// takes the parent's extraData verbatim, at every length, without aliasing it.
func TestPrepareEtnaPreselectionWork_ExtraData(t *testing.T) {
	for _, tt := range []struct {
		name  string
		extra []byte
	}{
		{"empty genesis", nil},
		{"seven bytes", []byte{25, 0, 0, 0, 0, 0, 7}},
		{"thirteen bytes", []byte{25, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 9}},
		{"other length", []byte{25, 1, 2, 3, 4}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := newPreselectionTestWorker(t, newPreselectionTestGenesis(newEtnaTestChainConfig(), tt.extra))
			parent := w.chain.CurrentBlock()
			env, err := w.prepareEtnaPreselectionWork(parent, preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 30_000_000)
			if err != nil {
				t.Fatalf("prepareEtnaPreselectionWork: %v", err)
			}
			defer env.discard()
			if !bytes.Equal(env.header.Extra, tt.extra) {
				t.Fatalf("extraData = %x, want the parent's %x", env.header.Extra, tt.extra)
			}
			if len(tt.extra) > 0 {
				env.header.Extra[0]++
				if parent.Extra[0] != tt.extra[0] {
					t.Fatal("the simulated extraData aliases the parent's")
				}
			}
		})
	}
}

// TestPrepareEtnaPreselectionWork_FeeShare pins the base-fee split of the
// simulated child: extraData[0] of a 13-byte parent extraData goes to the
// beneficiary and the rest to the treasury, and any other length, an Etna
// genesis with an empty or 7-byte extraData included, burns the base fee.
func TestPrepareEtnaPreselectionWork_FeeShare(t *testing.T) {
	for _, tt := range []struct {
		name        string
		extra       []byte
		beneficiary uint64
		treasury    uint64
	}{
		{"thirteen-byte fee share", []byte{25, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 9}, 52_500_000_000, 157_500_000_000},
		{"thirteen-byte zero fee share", make([]byte, params.EtnaExtraDataLen), 0, 210_000_000_000},
		{"empty genesis burns", nil, 0, 0},
		{"seven-byte genesis burns", []byte{25, 0, 0, 0, 0, 0, 7}, 0, 0},
		{"other length burns", []byte{25, 1, 2, 3, 4}, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := newEtnaTestChainConfig()
			w := newPreselectionTestWorker(t, newPreselectionTestGenesis(config, tt.extra))
			env, err := w.prepareEtnaPreselectionWork(w.chain.CurrentBlock(), preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 30_000_000)
			if err != nil {
				t.Fatalf("prepareEtnaPreselectionWork: %v", err)
			}
			defer env.discard()
			if err := w.commitTransaction(context.Background(), env, preselectionTransfer(t, config, testBankKey, 0, 0)); err != nil {
				t.Fatalf("commitTransaction: %v", err)
			}
			if got := env.state.GetBalance(preselectionTestBeneficiary).Uint64(); got != tt.beneficiary {
				t.Fatalf("beneficiary balance = %d, want %d", got, tt.beneficiary)
			}
			if got := env.state.GetBalance(core.TaikoTreasuryAddress(config.ChainID)).Uint64(); got != tt.treasury {
				t.Fatalf("treasury balance = %d, want %d", got, tt.treasury)
			}
			// The sender pays 21,000 gas at the base fee, plus the 1 wei value.
			paid := new(big.Int).Sub(testBankFunds, env.state.GetBalance(testBankAddress).ToBig())
			if paid.Cmp(big.NewInt(210_000_000_001)) != 0 {
				t.Fatalf("sender paid %v, want 210000000001", paid)
			}
		})
	}
}

// TestBuildTransactionsLists_EtnaParent pins the selection on top of an Etna
// parent: golden-touch transactions are ordinary, and zk gas exhaustion stops
// the whole selection instead of skipping the transaction. Before Etna the
// golden touch stays filtered.
func TestBuildTransactionsLists_EtnaParent(t *testing.T) {
	config := newEtnaTestChainConfig()
	signer := types.LatestSigner(config)
	golden := preselectionTransfer(t, config, goldenTouchTestKey(t), 0, 3)
	exhausting := types.MustSignNewTx(testBankKey, signer, &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     0,
		GasTipCap: big.NewInt(2),
		GasFeeCap: big.NewInt(preselectionTestBaseFee + 2),
		Gas:       5_000_000,
		To:        &etnaTestHasher,
	})
	late := preselectionTransfer(t, config, preselectionTestCallerKey, 0, 1)

	t.Run("golden touch is selected", func(t *testing.T) {
		w := newPreselectionTestWorker(t, newPreselectionTestGenesis(config, nil), golden, late)
		lists, err := w.BuildTransactionsLists(preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 30_000_000, 100_000, nil, 1)
		if err != nil {
			t.Fatalf("BuildTransactionsLists: %v", err)
		}
		got, want := listHashes(lists), [][]common.Hash{{golden.Hash(), late.Hash()}}
		if len(got) != 1 || len(got[0]) != 2 || got[0][0] != want[0][0] || got[0][1] != want[0][1] {
			t.Fatalf("lists = %v, want %v", got, want)
		}
	})
	t.Run("zk gas exhaustion stops the selection", func(t *testing.T) {
		w := newPreselectionTestWorker(t, newPreselectionTestGenesis(config, nil), golden, exhausting, late)
		lists, err := w.BuildTransactionsLists(preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 30_000_000, 100_000, nil, 2)
		if err != nil {
			t.Fatalf("BuildTransactionsLists: %v", err)
		}
		got := listHashes(lists)
		if len(got) != 1 || len(got[0]) != 1 || got[0][0] != golden.Hash() {
			t.Fatalf("lists = %v, want only the golden-touch transfer before the exhausting call", got)
		}
	})
	t.Run("before etna the golden touch is filtered", func(t *testing.T) {
		preEtna := newEtnaTestChainConfig()
		never := uint64(math.MaxUint64)
		preEtna.EtnaTime = &never
		w := newPreselectionTestWorker(t, newPreselectionTestGenesis(preEtna, []byte{0, 0, 0, 0, 0, 0, 0}),
			preselectionTransfer(t, preEtna, goldenTouchTestKey(t), 0, 3), preselectionTransfer(t, preEtna, preselectionTestCallerKey, 0, 1))
		lists, err := w.BuildTransactionsLists(preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 30_000_000, 100_000, nil, 1)
		if err != nil {
			t.Fatalf("BuildTransactionsLists: %v", err)
		}
		if got := listHashes(lists); len(got) != 1 || len(got[0]) != 1 {
			t.Fatalf("lists = %v, want only the ordinary transfer", got)
		}
		if sender, _ := types.Sender(signer, lists[0].TxList[0]); sender == taiko.GoldenTouchAccount {
			t.Fatal("the golden-touch transfer was selected before Etna")
		}
	})
}

// TestBuildTransactionsLists_EtnaParentParamErrors pins the invalid-params
// cases of an Etna parent, which precede every other check.
func TestBuildTransactionsLists_EtnaParentParamErrors(t *testing.T) {
	w := newPreselectionTestWorker(t, newPreselectionTestGenesis(newEtnaTestChainConfig(), nil))
	for _, tt := range []struct {
		name     string
		gasLimit uint64
		lists    uint64
	}{
		{"zero lists", 30_000_000, 0},
		{"gas limit product overflows", math.MaxUint64, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := w.BuildTransactionsLists(preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), tt.gasLimit, 100_000, nil, tt.lists)
			if !errors.Is(err, ErrInvalidPreselectionParams) {
				t.Fatalf("error = %v, want ErrInvalidPreselectionParams", err)
			}
		})
	}
}

// TestBuildTransactionsLists_PreEtnaParentAfterEtnaWallClock pins that a
// pre-Etna parent's preselection, which simulates at the wall-clock time, is
// never subject to the Etna parent beacon root rule.
func TestBuildTransactionsLists_PreEtnaParentAfterEtnaWallClock(t *testing.T) {
	config := newEtnaTestChainConfig()
	gspec := newPreselectionTestGenesis(config, []byte{0, 0, 0, 0, 0, 0, 0})
	etnaTime := gspec.Timestamp + 1 // the next second after the genesis, long past
	config.EtnaTime = &etnaTime
	transfer := preselectionTransfer(t, config, testBankKey, 0, 1)
	w := newPreselectionTestWorker(t, gspec, transfer)
	if config.IsEtna(w.chain.CurrentBlock().Time) {
		t.Fatal("the parent must be pre-Etna")
	}

	lists, err := w.BuildTransactionsLists(preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 30_000_000, 100_000, nil, 1)
	if err != nil {
		t.Fatalf("BuildTransactionsLists: %v", err)
	}
	if got := listHashes(lists); len(got) != 1 || len(got[0]) != 1 || got[0][0] != transfer.Hash() {
		t.Fatalf("lists = %v, want the transfer", got)
	}
}

// taikoGoroutineGrowth runs f n times and returns how many goroutines it left
// running. Each environment that is never discarded leaves its state
// prefetcher's goroutine behind.
func taikoGoroutineGrowth(t *testing.T, n int, f func()) int {
	t.Helper()
	f() // let any lazily started goroutine settle first
	before := runtime.NumGoroutine()
	for range n {
		f()
	}
	return runtime.NumGoroutine() - before
}

// TestBuildTransactionsLists_DiscardsEnvironment pins that preselection stops
// the state prefetcher of the environment it simulates in, on both sides of
// Etna, so repeated calls leave no goroutine behind.
func TestBuildTransactionsLists_DiscardsEnvironment(t *testing.T) {
	for _, fork := range []struct {
		name   string
		config func() *params.ChainConfig
		extra  []byte
	}{
		{"etna parent", newEtnaTestChainConfig, etnaTestExtraData},
		{"pre-etna parent", newPreEtnaTestChainConfig, preEtnaTestExtraData},
	} {
		t.Run(fork.name, func(t *testing.T) {
			config := fork.config()
			w := newPreselectionTestWorker(t, newPreselectionTestGenesis(config, fork.extra), preselectionTransfer(t, config, testBankKey, 0, 1))
			growth := taikoGoroutineGrowth(t, 50, func() {
				lists, err := w.BuildTransactionsLists(preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), 30_000_000, 100_000, nil, 1)
				if err != nil || len(lists) != 1 {
					t.Fatalf("BuildTransactionsLists = %v, %v; want one list", lists, err)
				}
			})
			if growth > 5 {
				t.Fatalf("50 preselections left %d goroutines running", growth)
			}
		})
	}
}

// TestSealBlockWith_DiscardsEnvironment pins that sealing stops the state
// prefetcher of its environment when the build fails, as it does when the
// block is assembled.
func TestSealBlockWith_DiscardsEnvironment(t *testing.T) {
	config := newPreEtnaTestChainConfig()
	w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))
	// A nonce gap fails the anchor, the first transaction, after the
	// environment was prepared.
	attrs := preEtnaTestAttributes(b.chain.CurrentBlock(), encodeTestTxList(t, bankTransfer(t, config, 5)))
	growth := taikoGoroutineGrowth(t, 50, func() {
		if _, err := w.sealBlockWith(b.chain.CurrentBlock(), 0, attrs); err == nil || !strings.Contains(err.Error(), "anchor transaction failed") {
			t.Fatalf("sealBlockWith error = %v, want an anchor failure", err)
		}
	})
	if growth > 5 {
		t.Fatalf("50 failing seals left %d goroutines running", growth)
	}
}

// TestBuildTransactionsLists_EstimatedGasUsed pins that every list reports the
// gas its own transactions use, on both sides of Etna, whether the lists split
// on the gas limit or on the compressed size.
func TestBuildTransactionsLists_EstimatedGasUsed(t *testing.T) {
	for _, fork := range []struct {
		name   string
		config func() *params.ChainConfig
		extra  []byte
	}{
		{"etna parent", newEtnaTestChainConfig, etnaTestExtraData},
		{"pre-etna parent", newPreEtnaTestChainConfig, preEtnaTestExtraData},
	} {
		config := fork.config()
		txs := []*types.Transaction{
			preselectionTransfer(t, config, testBankKey, 0, 1),
			preselectionTransfer(t, config, testBankKey, 1, 1),
			preselectionTransfer(t, config, testBankKey, 2, 1),
		}
		// The compressed size of the first two transfers: the third one no
		// longer fits under it.
		twoTxs, err := encodeAndCompressTxList(txs[:2])
		if err != nil {
			t.Fatalf("encodeAndCompressTxList: %v", err)
		}
		for _, split := range []struct {
			name     string
			gasLimit uint64
			maxBytes uint64
		}{
			{"split on gas", 2 * params.TxGas, 100_000},
			{"split on size", 30_000_000, uint64(len(twoTxs))},
		} {
			t.Run(fork.name+"/"+split.name, func(t *testing.T) {
				w := newPreselectionTestWorker(t, newPreselectionTestGenesis(config, fork.extra), txs...)
				lists, err := w.BuildTransactionsLists(preselectionTestBeneficiary, big.NewInt(preselectionTestBaseFee), split.gasLimit, split.maxBytes, nil, 2)
				if err != nil {
					t.Fatalf("BuildTransactionsLists: %v", err)
				}
				got := listHashes(lists)
				if len(got) != 2 || len(got[0]) != 2 || len(got[1]) != 1 ||
					got[0][0] != txs[0].Hash() || got[0][1] != txs[1].Hash() || got[1][0] != txs[2].Hash() {
					t.Fatalf("lists = %v, want [[tx0 tx1] [tx2]]", got)
				}
				for i, want := range []uint64{2 * params.TxGas, params.TxGas} {
					if lists[i].EstimatedGasUsed != want {
						t.Fatalf("list %d estimatedGasUsed = %d, want %d", i, lists[i].EstimatedGasUsed, want)
					}
				}
			})
		}
	}
}
