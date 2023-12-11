package eth

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
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

// TxPoolContent retrieves the transaction pool content with the given upper limits.
func (s *TaikoAPIBackend) TxPoolContent(
	beneficiary common.Address,
	baseFee *big.Int,
	maxTransactionsPerBlock uint64,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	locals []string,
	maxTransactionsLists uint64,
) ([]types.Transactions, error) {
	log.Info(
		"Fetching L2 pending transactions finished",
		"maxTransactionsPerBlock", maxTransactionsPerBlock,
		"blockMaxGasLimit", blockMaxGasLimit,
		"maxBytesPerTxList", maxBytesPerTxList,
		"maxTransactions", maxTransactionsLists,
		"locals", locals,
	)

	return s.eth.Miner().BuildTransactionsLists(
		beneficiary,
		baseFee,
		maxTransactionsPerBlock,
		blockMaxGasLimit,
		maxBytesPerTxList,
		locals,
		maxTransactionsLists,
	)
}

// Get L2ParentHashes retrieves the preceding 256 parent hashes given a block number.
func (s *TaikoAPIBackend) GetL2ParentHashes(blockID uint64) ([]common.Hash, error) {
	var hashes []common.Hash

	for i := blockID; i != 0 && (blockID-i) < 256; i-- {
		hashes = append(hashes, s.eth.blockchain.GetHeaderByNumber(blockID-i).Hash())
	}
	return hashes, nil
}

// Get L2ParentBlocks retrieves the preceding 256 parent blocks given a block number.
func (s *TaikoAPIBackend) GetL2ParentBlocks(blockID uint64) ([]map[string]interface{}, error) {
	var parents []map[string]interface{}
	b := ethapi.NewBlockChainAPI(s.eth.APIBackend)

	for i := blockID; i != 0 && (blockID-i) < 256; i-- {
		block, err := b.GetBlockByNumber(context.Background(), rpc.BlockNumber(blockID-i), false)
		if err != nil {
			return nil, err
		}
		parents = append(parents, block)
	}

	return parents, nil
}
