package rawdb

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// Database key prefix for those throwaway transactions.
	throwawayTxPrefix = []byte("TKO:TAT")
)

// l1OriginKey calculates the L1Origin key.
// l1OriginPrefix + l2HeaderHash -> l1OriginKey
func throwawayTxKey(blockID *big.Int) []byte {
	data, _ := (*math.HexOrDecimal256)(blockID).MarshalText()
	return append(throwawayTxPrefix, data...)
}

// ReadThrowawayTx retrieves the given L2 block's throwaway transaction from database.
func ReadThrowawayTx(db ethdb.KeyValueReader, blockID *big.Int) (*types.Transaction, error) {
	data, _ := db.Get(throwawayTxKey(blockID))
	if len(data) == 0 {
		return nil, nil
	}

	tx := new(types.Transaction)
	if err := rlp.Decode(bytes.NewReader(data), tx); err != nil {
		return nil, fmt.Errorf("invalid transaction RLP bytes: %w", err)
	}

	return tx, nil
}

// WriteThrowawayTx stores the given throwaway transaction to database.
func WriteThrowawayTx(db ethdb.KeyValueWriter, blockID *big.Int, tx *types.Transaction) {
	data, err := rlp.EncodeToBytes(tx)
	if err != nil {
		log.Crit("Failed to encode transaction", "err", err)
	}

	if err := db.Put(throwawayTxKey(blockID), data); err != nil {
		log.Crit("Failed to store transaction", "err", err)
	}
}
