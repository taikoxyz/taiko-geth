package core

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func TestPoolContentSplit(t *testing.T) {
	testAddress := "0x0000777735367b36bC9B61C50022d9D0700dB4Ec"
	testPrivKey := "92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38"

	// Gas limit is smaller than the limit.
	splitter := &PoolContentSplitter{
		chainID:       new(big.Int).SetUint64(1336),
		minTxGasLimit: 21000,
	}

	splitted := splitter.Split(PoolContent{
		common.BytesToAddress(randomBytes(32)): {
			types.NewTx(&types.LegacyTx{}),
		},
	})

	require.Empty(t, splitted)

	// Gas limit is larger than the limit.
	splitter = &PoolContentSplitter{
		chainID:       new(big.Int).SetUint64(1336),
		minTxGasLimit: 21000,
	}

	splitted = splitter.Split(PoolContent{
		common.BytesToAddress(randomBytes(32)): {
			types.NewTx(&types.LegacyTx{Gas: 21001}),
		},
	})

	require.Empty(t, splitted)

	// Transaction's RLP encoded bytes is larger than the limit.
	txBytesTooLarge := types.NewTx(&types.LegacyTx{})

	bytes, err := rlp.EncodeToBytes(txBytesTooLarge)
	require.Nil(t, err)
	require.NotEmpty(t, bytes)

	splitter = &PoolContentSplitter{
		chainID:           new(big.Int).SetUint64(1336),
		maxBytesPerTxList: uint64(len(bytes) - 1),
		minTxGasLimit:     uint64(len(bytes) - 2),
	}

	splitted = splitter.Split(PoolContent{
		common.BytesToAddress(randomBytes(32)): {txBytesTooLarge},
	})

	require.Empty(t, splitted)

	// Transactions that meet the limits
	testKey, err := crypto.HexToECDSA(testPrivKey)
	require.Nil(t, err)

	signer := types.LatestSignerForChainID(new(big.Int).SetUint64(1336))
	tx1 := types.MustSignNewTx(testKey, signer, &types.LegacyTx{Gas: 21001, Nonce: 1})
	tx2 := types.MustSignNewTx(testKey, signer, &types.LegacyTx{Gas: 21001, Nonce: 2})

	bytes, err = rlp.EncodeToBytes(tx1)
	require.Nil(t, err)
	require.NotEmpty(t, bytes)

	splitter = &PoolContentSplitter{
		chainID:                 new(big.Int).SetUint64(1336),
		minTxGasLimit:           21000,
		maxBytesPerTxList:       uint64(len(bytes) + 1000),
		maxTransactionsPerBlock: 1,
		blockMaxGasLimit:        tx1.Gas() + 1000,
	}

	splitted = splitter.Split(PoolContent{
		common.HexToAddress(testAddress): {tx1, tx2},
	})

	require.Equal(t, 2, len(splitted))
}

// RandomBytes generates a random bytes.
func randomBytes(size int) (b []byte) {
	b = make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		log.Crit("Generate random bytes error", "error", err)
	}
	return
}
