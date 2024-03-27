package eth

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// TaikoAPIBackend handles l2 node related RPC calls.
type TaikoAPIBackend struct {
	eth *Ethereum
}

// NewTaikoAPIBackend creates a new TaikoAPIBackend instance.
func NewTaikoAPIBackend(eth *Ethereum) *TaikoAPIBackend {
	return &TaikoAPIBackend{
		eth: eth,
	}
}

// HeadL1Origin returns the latest L2 block's corresponding L1 origin.
func (s *TaikoAPIBackend) HeadL1Origin() (*rawdb.L1Origin, error) {
	return s.eth.blockchain.HeadL1Origin()
}

// L1OriginByID returns the L2 block's corresponding L1 origin.
func (s *TaikoAPIBackend) L1OriginByID(blockID *math.HexOrDecimal256) (*rawdb.L1Origin, error) {
	return s.eth.blockchain.L1OriginByID((*big.Int)(blockID))
}

// TxPoolContent retrieves the transaction pool content with the given upper limits.
func (s *TaikoAPIBackend) TxPoolContent(
	beneficiary common.Address,
	baseFee *big.Int,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	locals []string,
	maxTransactionsLists uint64,
) ([]types.Transactions, error) {
	log.Info(
		"Fetching L2 pending transactions finished",
		"blockMaxGasLimit", blockMaxGasLimit,
		"maxBytesPerTxList", maxBytesPerTxList,
		"maxTransactions", maxTransactionsLists,
		"locals", locals,
	)

	return s.eth.Miner().BuildTransactionsLists(
		beneficiary,
		baseFee,
		blockMaxGasLimit,
		maxBytesPerTxList,
		locals,
		maxTransactionsLists,
	)
}

// Get L2ParentBlocks retrieves the block and 255 parent blocks given a block number.
func (s *TaikoAPIBackend) GetL2ParentHeaders(blockID uint64) ([]*types.Header, error) {
	headers := make([]*types.Header, 0, 256)
	start := 0
	if blockID > 255 {
		start = int(blockID - 255)
	}

	for start <= int(blockID) {
		headers = append(headers, s.eth.blockchain.GetHeaderByNumber(uint64(start)))
		start++
	}

	return headers, nil
}
