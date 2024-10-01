package miner

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/assert"
)

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

	insert := make([]*types.Transaction, txCount)
	for nonce := range insert {
		tx := types.NewTransaction(uint64(nonce), common.Address{}, big.NewInt(0), 100000, big.NewInt(0), make([]byte, 10240))
		tx, _ = types.SignTx(tx, types.HomesteadSigner{}, testBankKey)
		insert[nonce] = tx
	}
	b.txPool.Add(insert, true, false)

	return w
}

func TestBuildTransactionsLists(t *testing.T) {
	w := testGenerateWorker(t, 1000)

	maxBytesPerTxList := (params.BlobTxBytesPerFieldElement - 1) * params.BlobTxFieldElementsPerBlob
	txList, err := w.BuildTransactionsLists(
		testBankAddress,
		nil,
		240_000_000,
		uint64(maxBytesPerTxList),
		nil,
		1,
	)
	assert.NoError(t, err)
	assert.LessOrEqual(t, 1, len(txList))
	assert.LessOrEqual(t, txList[0].BytesLength, uint64(maxBytesPerTxList))
}
