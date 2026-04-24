package miner

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
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
	for i := range schedule.PrecompileMultipliers {
		schedule.PrecompileMultipliers[i] = math.MaxUint16
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
