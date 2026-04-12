package miner

import (
	"context"
	"crypto/sha256"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
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

func TestCommitL2TransactionsSkipsBlobTransactionsAfterUzen(t *testing.T) {
	var (
		db         = rawdb.NewMemoryDatabase()
		cancunTime = uint64(0)
		uzenTime   = uint64(1)
		config     = *params.AllCliqueProtocolChanges
	)
	config.Taiko = true
	config.ShanghaiTime = &cancunTime
	config.CancunTime = &cancunTime
	config.OsakaTime = nil
	config.UzenTime = &uzenTime
	config.BlobScheduleConfig = params.DefaultBlobSchedule
	config.Clique = &params.CliqueConfig{Period: 1, Epoch: 30000}

	engine := clique.New(config.Clique, db)
	w, b := newTestWorker(t, &config, engine, db, 0)

	currentHead := b.chain.CurrentBlock()
	env, err := w.prepareWork(context.Background(), &generateParams{
		timestamp:     uzenTime,
		forceTime:     true,
		parentHash:    currentHead.Hash(),
		coinbase:      testBankAddress,
		random:        currentHead.MixDigest,
		noTxs:         false,
		baseFeePerGas: big.NewInt(params.InitialBaseFee),
	}, false)
	if err != nil {
		t.Fatalf("prepareWork() error = %v", err)
	}
	env.header.GasLimit = 1_000_000
	env.gasPool = core.NewGasPool(env.header.GasLimit)

	legacyTx := newRandomTx(b.txPool, false)
	blobTx := makeTestBlobTransaction(t, &config, 1)

	queued := map[common.Address][]*txpool.LazyTransaction{
		testBankAddress: {
			newLazyTransaction(legacyTx),
			newLazyTransaction(blobTx),
		},
	}
	result, err := w.commitL2Transactions(
		env,
		nil,
		nil,
		newTransactionsByPriceAndNonce(env.signer, queued, env.header.BaseFee),
		newTransactionsByPriceAndNonce(env.signer, nil, env.header.BaseFee),
		1_000_000,
		0,
	)
	if err != nil {
		t.Fatalf("commitL2Transactions() error = %v", err)
	}

	assert.Len(t, result.TxsRemaining, 1)
	assert.EqualValues(t, types.LegacyTxType, result.TxsRemaining[0].Type())
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

func makeTestBlobTransaction(t *testing.T, config *params.ChainConfig, nonce uint64) *types.Transaction {
	t.Helper()

	blob := kzg4844.Blob{}
	commitment, err := kzg4844.BlobToCommitment(&blob)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := kzg4844.ComputeBlobProof(&blob, commitment)
	if err != nil {
		t.Fatal(err)
	}
	blobHash := kzg4844.CalcBlobHashV1(sha256.New(), &commitment)

	return types.MustSignNewTx(testBankKey, types.LatestSigner(config), &types.BlobTx{
		ChainID:    uint256.MustFromBig(config.ChainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(params.InitialBaseFee),
		GasFeeCap:  uint256.NewInt(2 * params.InitialBaseFee),
		Gas:        params.TxGas,
		To:         testUserAddress,
		BlobHashes: []common.Hash{blobHash},
		BlobFeeCap: uint256.NewInt(params.BlobTxMinBlobGasprice),
		Value:      uint256.NewInt(1),
		Sidecar: types.NewBlobTxSidecar(
			types.BlobSidecarVersion0,
			[]kzg4844.Blob{blob},
			[]kzg4844.Commitment{commitment},
			[]kzg4844.Proof{proof},
		),
	})
}

func newLazyTransaction(tx *types.Transaction) *txpool.LazyTransaction {
	return &txpool.LazyTransaction{
		Hash:      tx.Hash(),
		Tx:        tx,
		Time:      time.Now(),
		GasFeeCap: uint256.MustFromBig(tx.GasFeeCap()),
		GasTipCap: uint256.MustFromBig(tx.GasTipCap()),
		Gas:       tx.Gas(),
		BlobGas:   tx.BlobGas(),
	}
}
