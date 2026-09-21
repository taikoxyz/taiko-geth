package miner

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
)

const (
	// testCode is the testing contract binary code which will initialises some
	// variables in constructor
	testCode = "0x60806040527fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0060005534801561003457600080fd5b5060fc806100436000396000f3fe6080604052348015600f57600080fd5b506004361060325760003560e01c80630c4dae8814603757806398a213cf146053575b600080fd5b603d607e565b6040518082815260200191505060405180910390f35b607c60048036036020811015606757600080fd5b81019080803590602001909291905050506084565b005b60005481565b806000819055507fe9e44f9f7da8c559de847a3232b57364adc0354f15a2cd8dc636d54396f9587a6000546040518082815260200191505060405180910390a15056fea265627a7a723058208ae31d9424f2d0bc2a3da1a5dd659db2d71ec322a17db8f87e19e209e3a1ff4a64736f6c634300050a0032"

	// testGas is the gas required for contract deployment.
	testGas = 144109
)

func newRandomTx(txPool *txpool.TxPool, creation bool) *types.Transaction {
	var tx *types.Transaction
	gasPrice := big.NewInt(10 * params.InitialBaseFee)
	if creation {
		tx, _ = types.SignTx(types.NewContractCreation(txPool.Nonce(testBankAddress), big.NewInt(0), testGas, gasPrice, common.FromHex(testCode)), types.HomesteadSigner{}, testBankKey)
	} else {
		tx, _ = types.SignTx(types.NewTransaction(txPool.Nonce(testBankAddress), testUserAddress, big.NewInt(1000), params.TxGas, gasPrice, nil), types.HomesteadSigner{}, testBankKey)
	}
	return tx
}

func testGenerateWorker(t *testing.T, txCount int) *Miner {
	t.Parallel()
	var (
		db     = rawdb.NewMemoryDatabase()
		config = *params.AllCliqueProtocolChanges
	)
	config.Taiko = true
	config.Clique = &params.CliqueConfig{Period: 1, Epoch: 30000}
	engine := clique.New(config.Clique, db)

	w, b := newTestWorker(t, &config, engine, db, 0)

	for i := 0; i < txCount; i++ {
		b.txPool.Add([]*types.Transaction{newRandomTx(b.txPool, true)}, true)
		b.txPool.Add([]*types.Transaction{newRandomTx(b.txPool, false)}, true)
	}

	return w
}

func TestBuildTransactionsLists(t *testing.T) {
	w := testGenerateWorker(t, 2000)

	maxBytesPerTxList := (params.BlobTxBytesPerFieldElement - 1) * params.BlobTxFieldElementsPerBlob
	txList, err := w.BuildTransactionsLists(
		testBankAddress,
		nil,
		240_000_000,
		uint64(maxBytesPerTxList)/10,
		nil,
		1,
	)
	assert.NoError(t, err)
	assert.LessOrEqual(t, 1, len(txList))
	assert.LessOrEqual(t, txList[0].BytesLength, uint64(maxBytesPerTxList))
}

func TestRemoveGoldenTouchPendingTxs(t *testing.T) {
	pending := map[common.Address][]*txpool.LazyTransaction{
		taiko.GoldenTouchAccount: {},
		testUserAddress:          {},
	}

	filtered := removeGoldenTouchPendingTxs(pending)

	assert.Len(t, filtered, 1)
	_, exists := filtered[taiko.GoldenTouchAccount]
	assert.False(t, exists)
	_, exists = filtered[testUserAddress]
	assert.True(t, exists)
}

// CHANGE(taiko): TestApplyTransaction_SealerZkGasExhaustionSurface pins the
// exported wrapper that the sealer consumes. The inner ApplyTransactionWithEVM
// is already covered by core/taiko_state_processor_unzen_test.go; this test
// exists because miner.applyTransaction (miner/worker.go:416) calls
// core.ApplyTransaction — so a future rename or arg reshuffle of the wrapper
// would fail here at the sealer's actual consumption surface, not just at the
// inner function. The sealer's snapshot/revert at miner/worker.go:413-419 and
// the break-guard at miner/taiko_worker.go:299 both rely on the wrapper
// surfacing vm.ErrZkGasLimitExceeded as a Go error.
func TestApplyTransaction_SealerZkGasExhaustionSurface(t *testing.T) {
	zero := uint64(0)
	chainConfig := *params.MergedTestChainConfig
	chainConfig.Taiko = true
	chainConfig.ChainID = big.NewInt(167000)
	chainConfig.UnzenTime = &zero
	chainConfig.OsakaTime = &zero
	signer := types.LatestSigner(&chainConfig)

	senderKey, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	sender := crypto.PubkeyToAddress(senderKey.PublicKey)
	callee := common.HexToAddress("0x000000000000000000000000000000000000c0de")

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.SetBalance(sender, uint256.NewInt(1_000_000_000_000_000_000), 0)
	statedb.SetCode(callee, []byte{0x60, 0x01, 0x60, 0x01, 0x01, 0x00}, tracing.CodeChangeGenesis) // PUSH1 1 PUSH1 1 ADD STOP
	statedb.Finalise(true)

	schedule := &vm.ZkGasSchedule{BlockLimit: 0}
	for i := range schedule.OpcodeMultipliers {
		schedule.OpcodeMultipliers[i] = math.MaxUint16
	}

	blockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.Address{},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Difficulty:  big.NewInt(0),
		BaseFee:     big.NewInt(1_000_000_000),
		GasLimit:    30_000_000,
		Random:      &common.Hash{},
		BlobBaseFee: big.NewInt(1),
	}
	evm := vm.NewEVM(blockCtx, statedb, &chainConfig, vm.Config{ZkGasMeter: vm.NewZkGasMeter(schedule)})

	tx, err := types.SignTx(
		types.NewTransaction(0, callee, big.NewInt(0), 100_000, big.NewInt(1_000_000_000), nil),
		signer,
		senderKey,
	)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	gp := core.NewGasPool(30_000_000)
	header := &types.Header{Number: big.NewInt(1), Time: 1, BaseFee: big.NewInt(1_000_000_000), GasLimit: 30_000_000}
	receipt, err := core.ApplyTransaction(evm, gp, statedb, header, tx)

	if !errors.Is(err, vm.ErrZkGasLimitExceeded) {
		t.Fatalf("expected ErrZkGasLimitExceeded, got err=%v receipt=%+v", err, receipt)
	}
	if receipt != nil {
		t.Fatalf("expected nil receipt, got %+v", receipt)
	}
}

// CHANGE(taiko): beaconRootsStubCode stands in for the EIP-4788 contract in the
// Unzen sealing tests. It records TIMESTAMP in slot 0 and the calldata word (the
// parent beacon root) in slot 1; with the zero root only slot 0 changes, which is
// enough to make the system call observable in the state root.
//
//	TIMESTAMP PUSH1 0 SSTORE PUSH1 0 CALLDATALOAD PUSH1 1 SSTORE STOP
var beaconRootsStubCode = []byte{0x42, 0x60, 0x00, 0x55, 0x60, 0x00, 0x35, 0x60, 0x01, 0x55, 0x00}

// goldenTouchTestKey is the publicly known golden-touch key that signs anchor
// transactions; consensus/taiko uses the same key in its tests.
var goldenTouchTestKey, _ = crypto.HexToECDSA("92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38")

// taikoL2AddressFor mirrors the TaikoL2 address derivation in consensus/taiko.New.
func taikoL2AddressFor(chainID *big.Int) common.Address {
	prefix := strings.TrimPrefix(chainID.String(), "0")
	return common.HexToAddress(
		"0x" + prefix +
			strings.Repeat("0", common.AddressLength*2-len(prefix)-len(taiko.TaikoL2AddressSuffix)) +
			taiko.TaikoL2AddressSuffix,
	)
}

// newUnzenTestChainConfig returns a Taiko chain config with every Ethereum fork up
// to Osaka and the Unzen fork active from genesis.
func newUnzenTestChainConfig() *params.ChainConfig {
	config := *params.MergedTestChainConfig
	config.ChainID = big.NewInt(167000)
	config.Taiko = true
	zero := uint64(0)
	config.UnzenTime = &zero
	return &config
}

// newUnzenTestGenesis funds the test bank, installs the observable beacon-roots
// stub at the EIP-4788 address and a no-op anchor contract at the TaikoL2 address.
func newUnzenTestGenesis(config *params.ChainConfig) *core.Genesis {
	return &core.Genesis{
		Config: config,
		Alloc: types.GenesisAlloc{
			testBankAddress:                   {Balance: testBankFunds},
			params.BeaconRootsAddress:         {Code: beaconRootsStubCode, Balance: common.Big0},
			taikoL2AddressFor(config.ChainID): {Code: []byte{0x00}, Balance: common.Big0}, // STOP
		},
		Timestamp:  1000,
		Difficulty: common.Big0,
		BaseFee:    big.NewInt(params.InitialBaseFee),
	}
}

// newUnzenTestWorker builds a miner on top of the Taiko consensus engine, which is
// the engine sealBlockWith runs against in production.
func newUnzenTestWorker(t *testing.T, config *params.ChainConfig, gspec *core.Genesis) (*Miner, *testWorkerBackend) {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	engine := taiko.New(config, db)
	chain, err := core.NewBlockChain(db, gspec, engine, &core.BlockChainConfig{ArchiveMode: true})
	if err != nil {
		t.Fatalf("core.NewBlockChain failed: %v", err)
	}
	pool := legacypool.New(testTxPoolConfig, chain)
	txPool, _ := txpool.New(testTxPoolConfig.PriceLimit, chain, []txpool.SubPool{pool})
	backend := &testWorkerBackend{db: db, chain: chain, txPool: txPool, genesis: gspec}
	return New(backend, testConfig, engine), backend
}

// TestPrepareWork_UnzenAppliesBeaconRootSystemCall pins the build-side half of the
// EIP-4788 parity invariant: a Taiko Unzen block must be prepared with the canonical
// zero parent beacon root already in the header, so the beacon-roots system call runs
// while sealing exactly as core.StateProcessor runs it when the block is imported.
func TestPrepareWork_UnzenAppliesBeaconRootSystemCall(t *testing.T) {
	config := newUnzenTestChainConfig()
	w, b := newUnzenTestWorker(t, config, newUnzenTestGenesis(config))
	defer b.chain.Stop()

	parent := b.chain.CurrentBlock()
	timestamp := parent.Time + 1
	env, err := w.prepareWork(context.Background(), &generateParams{
		timestamp:     timestamp,
		forceTime:     true,
		parentHash:    parent.Hash(),
		coinbase:      testUserAddress,
		baseFeePerGas: big.NewInt(params.InitialBaseFee),
	}, false)
	if err != nil {
		t.Fatalf("prepareWork: %v", err)
	}
	defer env.discard()

	if env.header.ParentBeaconRoot == nil || *env.header.ParentBeaconRoot != (common.Hash{}) {
		t.Fatalf("Unzen header must carry the zero parent beacon root before execution, got %v", env.header.ParentBeaconRoot)
	}
	want := common.BigToHash(new(big.Int).SetUint64(timestamp))
	if got := env.state.GetState(params.BeaconRootsAddress, common.Hash{}); got != want {
		t.Fatalf("beacon-roots system call did not run while preparing the block: slot 0 = %v, want timestamp %v", got, want)
	}
}

// TestSealBlockWith_UnzenSealedRootMatchesImport re-executes a locally sealed Unzen
// block through core.StateProcessor, the path every importing node and prover uses,
// and requires the same state root. It fails whenever sealing skips a pre-execution
// system call that import performs.
func TestSealBlockWith_UnzenSealedRootMatchesImport(t *testing.T) {
	config := newUnzenTestChainConfig()
	w, b := newUnzenTestWorker(t, config, newUnzenTestGenesis(config))
	defer b.chain.Stop()

	parent := b.chain.CurrentBlock()
	timestamp := parent.Time + 1
	baseFee := big.NewInt(params.InitialBaseFee)
	taikoL2Address := taikoL2AddressFor(config.ChainID)

	anchorTx := types.MustSignNewTx(goldenTouchTestKey, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     0,
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

	block, err := w.sealBlockWith(parent, timestamp, 0, &engine.BlockMetadata{
		Beneficiary: testUserAddress,
		GasLimit:    parent.GasLimit,
		Timestamp:   timestamp,
		TxList:      txList,
		ExtraData:   []byte{},
	}, baseFee, nil)
	if err != nil {
		t.Fatalf("sealBlockWith: %v", err)
	}

	// The canonical Unzen header fields must come out of sealing exactly as
	// eth/catalyst reconstructs them from a payload.
	if block.BeaconRoot() == nil || *block.BeaconRoot() != (common.Hash{}) {
		t.Fatalf("sealed BeaconRoot = %v, want zero", block.BeaconRoot())
	}
	if block.BlobGasUsed() == nil || *block.BlobGasUsed() != 0 {
		t.Fatalf("sealed BlobGasUsed = %v, want 0", block.BlobGasUsed())
	}
	if block.ExcessBlobGas() == nil || *block.ExcessBlobGas() != 0 {
		t.Fatalf("sealed ExcessBlobGas = %v, want 0", block.ExcessBlobGas())
	}
	if block.RequestsHash() == nil || *block.RequestsHash() != types.EmptyRequestsHash {
		t.Fatalf("sealed RequestsHash = %v, want %v", block.RequestsHash(), types.EmptyRequestsHash)
	}

	statedb, err := b.chain.StateAt(parent.Root)
	if err != nil {
		t.Fatalf("StateAt(parent): %v", err)
	}
	res, err := core.NewStateProcessor(b.chain).Process(context.Background(), block, statedb, vm.Config{})
	if err != nil {
		t.Fatalf("import-side execution failed: %v", err)
	}
	if got := statedb.IntermediateRoot(true); got != block.Root() {
		t.Fatalf("sealed state root %v != imported state root %v: sealing and import disagree on pre-execution system calls", block.Root(), got)
	}
	if res.GasUsed != block.GasUsed() {
		t.Fatalf("sealed gas used %d != imported gas used %d", block.GasUsed(), res.GasUsed)
	}
	if got := types.DeriveSha(res.Receipts, trie.NewStackTrie(nil)); got != block.ReceiptHash() {
		t.Fatalf("sealed receipt root %v != imported receipt root %v", block.ReceiptHash(), got)
	}
}

// TestPrepareWork_PreUnzenSkipsBeaconRootSystemCall pins the other side of the
// gate: a Taiko block before the Unzen fork keeps a nil parent beacon root and must
// not run the beacon-roots system call, so pre-fork blocks stay identical to what
// shipped.
func TestPrepareWork_PreUnzenSkipsBeaconRootSystemCall(t *testing.T) {
	config := newUnzenTestChainConfig()
	config.UnzenTime, config.CancunTime, config.PragueTime, config.OsakaTime = nil, nil, nil, nil
	w, b := newUnzenTestWorker(t, config, newUnzenTestGenesis(config))
	defer b.chain.Stop()

	parent := b.chain.CurrentBlock()
	env, err := w.prepareWork(context.Background(), &generateParams{
		timestamp:     parent.Time + 1,
		forceTime:     true,
		parentHash:    parent.Hash(),
		coinbase:      testUserAddress,
		baseFeePerGas: big.NewInt(params.InitialBaseFee),
	}, false)
	if err != nil {
		t.Fatalf("prepareWork: %v", err)
	}
	defer env.discard()

	if env.header.ParentBeaconRoot != nil {
		t.Fatalf("pre-Unzen header must not carry a parent beacon root, got %v", env.header.ParentBeaconRoot)
	}
	if got := env.state.GetState(params.BeaconRootsAddress, common.Hash{}); got != (common.Hash{}) {
		t.Fatalf("pre-Unzen block ran the beacon-roots system call: slot 0 = %v", got)
	}
}
