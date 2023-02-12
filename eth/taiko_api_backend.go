package eth

import (
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core"
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
	blockID, err := rawdb.ReadHeadL1Origin(s.eth.ChainDb())
	if err != nil {
		return nil, err
	}

	if blockID == nil {
		return nil, ethereum.NotFound
	}

	l1Origin, err := rawdb.ReadL1Origin(s.eth.ChainDb(), blockID)
	if err != nil {
		return nil, err
	}

	if l1Origin == nil {
		return nil, ethereum.NotFound
	}

	return l1Origin, nil
}

// L1OriginByID returns the L2 block's corresponding L1 origin.
func (s *TaikoAPIBackend) L1OriginByID(blockID *math.HexOrDecimal256) (*rawdb.L1Origin, error) {
	l1Origin, err := rawdb.ReadL1Origin(s.eth.ChainDb(), (*big.Int)(blockID))
	if err != nil {
		return nil, err
	}

	if l1Origin == nil {
		return nil, ethereum.NotFound
	}

	return l1Origin, nil
}

// GetThrowawayTransactionReceipts returns the throwaway block's receipts
// without checking whether the block is in the canonical chain.
func (s *TaikoAPIBackend) GetThrowawayTransactionReceipts(hash common.Hash) (types.Receipts, error) {
	receipts := s.eth.blockchain.GetReceiptsByHash(hash)
	if receipts == nil {
		return nil, ethereum.NotFound
	}

	return receipts, nil
}

func (s *TaikoAPIBackend) TxPoolContent(
	maxTransactionsPerBlock uint64,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	minTxGasLimit uint64,
	locals string,
) (types.Transactions, error) {
	pending := s.eth.TxPool().Pending(false)

	log.Debug("Fetching L2 pending transactions finished", "length", core.PoolContent(pending).Len())

	contentSplitter, err := core.NewPoolContentSplitter(
		s.eth.BlockChain().Config().ChainID,
		maxTransactionsPerBlock,
		blockMaxGasLimit,
		maxBytesPerTxList,
		minTxGasLimit,
		locals,
	)
	if err != nil {
		return nil, err
	}

	txLists := contentSplitter.Split(pending)

	if len(txLists) == 0 {
		return types.Transactions{}, nil
	}

	return txLists[0], nil
}
