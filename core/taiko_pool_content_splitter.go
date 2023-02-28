package core

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// PoolContent represents a response body of a `txpool_content` RPC call.
type PoolContent map[common.Address]types.Transactions

// Len returns the number of transactions in the PoolContent.
func (pc PoolContent) Len() int {
	len := 0
	for _, pendingTxs := range pc {
		len += pendingTxs.Len()
	}

	return len
}

// ToTxsByPriceAndNonce creates a transaction set that can retrieve price sorted transactions in a nonce-honouring way.
func (pc PoolContent) ToTxsByPriceAndNonce(
	chainID *big.Int,
	localAddresses []common.Address,
) (
	locals *types.TransactionsByPriceAndNonce,
	remotes *types.TransactionsByPriceAndNonce,
) {
	var (
		localTxs  = map[common.Address]types.Transactions{}
		remoteTxs = map[common.Address]types.Transactions{}
	)

	for address, txsWithNonce := range pc {
	out:
		for _, tx := range txsWithNonce {
			for _, localAddress := range localAddresses {
				if address == localAddress {
					localTxs[address] = append(localTxs[address], tx)
					continue out
				}
			}
			remoteTxs[address] = append(remoteTxs[address], tx)
		}
	}

	return types.NewTransactionsByPriceAndNonce(types.LatestSignerForChainID(chainID), localTxs, nil),
		types.NewTransactionsByPriceAndNonce(types.LatestSignerForChainID(chainID), remoteTxs, nil)
}

// PoolContentSplitter is responsible for splitting the pool content
// which fetched from a `txpool_content` RPC call response into several smaller transactions lists
// and make sure each splitted list satisfies the limits defined in Taiko protocol.
type PoolContentSplitter struct {
	chainID                 *big.Int
	maxTransactionsPerBlock uint64
	blockMaxGasLimit        uint64
	maxBytesPerTxList       uint64
	minTxGasLimit           uint64
	locals                  []common.Address
}

// NewPoolContentSplitter creates a new PoolContentSplitter instance.
func NewPoolContentSplitter(
	chainID *big.Int,
	maxTransactionsPerBlock uint64,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	minTxGasLimit uint64,
	locals []string,
) (*PoolContentSplitter, error) {
	var localsAddresses []common.Address
	for _, account := range locals {
		if trimmed := strings.TrimSpace(account); !common.IsHexAddress(trimmed) {
			return nil, fmt.Errorf("invalid account: %s", trimmed)
		} else {
			localsAddresses = append(localsAddresses, common.HexToAddress(account))
		}
	}

	return &PoolContentSplitter{
		chainID:                 chainID,
		maxTransactionsPerBlock: maxTransactionsPerBlock,
		blockMaxGasLimit:        blockMaxGasLimit,
		maxBytesPerTxList:       maxBytesPerTxList,
		minTxGasLimit:           minTxGasLimit,
		locals:                  localsAddresses,
	}, nil
}

// Split splits the given transaction pool content to make each splitted
// transactions list satisfies the rules defined in Taiko protocol.
func (p *PoolContentSplitter) Split(poolContent PoolContent) []types.Transactions {
	var (
		localTxs, remoteTxs   = poolContent.ToTxsByPriceAndNonce(p.chainID, p.locals)
		splittedLocalTxLists  = p.splitTxs(localTxs)
		splittedRemoteTxLists = p.splitTxs(remoteTxs)
	)

	splittedTxLists := append(splittedLocalTxLists, splittedRemoteTxLists...)

	return splittedTxLists
}

// validateTx checks whether the given transaction is valid according
// to the rules in Taiko protocol.
func (p *PoolContentSplitter) validateTx(tx *types.Transaction) error {
	if tx.Gas() < p.minTxGasLimit || tx.Gas() > p.blockMaxGasLimit {
		return fmt.Errorf(
			"transaction %s gas limit reaches the limits, got=%v, lowerBound=%v, upperBound=%v",
			tx.Hash(), tx.Gas(), p.minTxGasLimit, p.blockMaxGasLimit,
		)
	}

	b, err := rlp.EncodeToBytes(tx)
	if err != nil {
		return fmt.Errorf(
			"failed to rlp encode the pending transaction %s: %w", tx.Hash(), err,
		)
	}

	if len(b) > int(p.maxBytesPerTxList) {
		return fmt.Errorf(
			"size of transaction %s's rlp encoded bytes is bigger than the limit, got=%v, limit=%v",
			tx.Hash(), len(b), p.maxBytesPerTxList,
		)
	}

	return nil
}

// isTxBufferFull checks whether the given transaction can be appended to the
// current transaction list
// NOTE: this function *MUST* be called after using `validateTx` to check every
// inside transaction is valid.
func (p *PoolContentSplitter) isTxBufferFull(t *types.Transaction, txs []*types.Transaction, gas uint64) bool {
	if len(txs) >= int(p.maxTransactionsPerBlock) {
		return true
	}

	if gas+t.Gas() > p.blockMaxGasLimit {
		return true
	}

	// Transactions list's RLP encoding error has already been checked in
	// `validateTx`, so no need to check the error here.
	if b, _ := rlp.EncodeToBytes(append([]*types.Transaction{t}, txs...)); len(b) > int(p.maxBytesPerTxList) {
		return true
	}

	return false
}

// splitTxs the internal implementation Split, splits the given transactions into small transactions lists
// which satisfy the protocol constraints.
func (p *PoolContentSplitter) splitTxs(txs *types.TransactionsByPriceAndNonce) []types.Transactions {
	var (
		splittedTxLists        = make([]types.Transactions, 0)
		txBuffer               = make([]*types.Transaction, 0, p.maxTransactionsPerBlock)
		gasBuffer       uint64 = 0
	)
	for {
		tx := txs.Peek()
		if tx == nil {
			break
		}

		// If the transaction is invalid, we simply ignore it.
		if err := p.validateTx(tx); err != nil {
			log.Debug("Invalid pending transaction", "hash", tx.Hash(), "error", err)
			txs.Pop() // If this tx is invalid, ignore this sender's other txs in pool.
			continue
		}

		// If the transactions buffer is full, we make all transactions in
		// current buffer a new splitted transaction list, and then reset the
		// buffer.
		if p.isTxBufferFull(tx, txBuffer, gasBuffer) {
			splittedTxLists = append(splittedTxLists, txBuffer)
			txBuffer = make([]*types.Transaction, 0, p.maxTransactionsPerBlock)
			gasBuffer = 0
		}

		txBuffer = append(txBuffer, tx)
		gasBuffer += tx.Gas()

		txs.Shift()
	}

	// Maybe there are some remaining transactions in current buffer,
	// make them a new transactions list too.
	if len(txBuffer) > 0 {
		splittedTxLists = append(splittedTxLists, txBuffer)
	}

	return splittedTxLists
}
