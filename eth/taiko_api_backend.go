package eth

import (
	"bytes"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
)

const lastBlockByBatchCacheSize = 1024 // CHANGE(taiko): cache last-block lookups by batch ID.

// TaikoAPIBackend handles L2 node related RPC calls.
type TaikoAPIBackend struct {
	eth *Ethereum

	lastBlockByBatchCache     *lru.Cache[string, *big.Int]
	lastBlockByBatchCacheHead atomic.Value // common.Hash
	lastBlockByBatchCacheInit sync.Once
}

// NewTaikoAPIBackend creates a new TaikoAPIBackend instance.
func NewTaikoAPIBackend(eth *Ethereum) *TaikoAPIBackend {
	return &TaikoAPIBackend{
		eth:                       eth,
		lastBlockByBatchCache:     lru.NewCache[string, *big.Int](lastBlockByBatchCacheSize),
		lastBlockByBatchCacheInit: sync.Once{},
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

// LastL1OriginByBatchID returns the L1 origin of the last block for the given batch.
func (s *TaikoAPIBackend) LastL1OriginByBatchID(batchID *math.HexOrDecimal256) (*rawdb.L1Origin, error) {
	blockID, err := s.LastBlockIDByBatchID(batchID)
	if err != nil {
		return nil, err
	}
	return s.L1OriginByID((*math.HexOrDecimal256)(blockID))
}

// LastBlockIDByBatchID returns the ID of the last block for the given batch.
func (s *TaikoAPIBackend) LastBlockIDByBatchID(batchID *math.HexOrDecimal256) (*hexutil.Big, error) {
	s.lastBlockByBatchCacheInit.Do(func() {
		if s.lastBlockByBatchCache == nil {
			s.lastBlockByBatchCache = lru.NewCache[string, *big.Int](lastBlockByBatchCacheSize)
		}
	})

	head := s.eth.blockchain.CurrentHeader()
	if head == nil {
		return nil, ethereum.NotFound
	}
	headHash := head.Hash()
	if cachedHead, ok := s.lastBlockByBatchCacheHead.Load().(common.Hash); !ok || cachedHead != headHash {
		s.lastBlockByBatchCache.Purge()
		s.lastBlockByBatchCacheHead.Store(headHash)
	}

	currentBlock := s.eth.BlockChain().GetBlockByNumber(head.Number.Uint64())
	targetBatchID := (*big.Int)(batchID)
	cacheKey := targetBatchID.String()
	if cached, ok := s.lastBlockByBatchCache.Get(cacheKey); ok {
		return (*hexutil.Big)(new(big.Int).Set(cached)), nil
	}

	// If the given batchID is greater than the head proposalID,
	// it means the batch does not exist.
	if isShastaBlock(currentBlock) {
		proposalID, _, err := core.DecodeShastaProposalID(currentBlock.Header().Extra)
		if err != nil {
			return nil, err
		}
		if targetBatchID.Cmp(proposalID) > 0 {
			return nil, ethereum.NotFound
		}
	}

	// Traverse backwards to find the last block of the given batchID.
	for isShastaBlock(currentBlock) {
		if currentBlock.NumberU64() == 0 {
			break
		}
		proposalID, endOfProposal, err := core.DecodeShastaProposalID(currentBlock.Header().Extra)
		if err != nil {
			return nil, err
		}
		if proposalID.Cmp(targetBatchID) == 0 {
			if !endOfProposal {
				return nil, ethereum.NotFound
			}
			result := new(big.Int).Set(currentBlock.Number())
			s.lastBlockByBatchCache.Add(cacheKey, result)
			return (*hexutil.Big)(new(big.Int).Set(result)), nil
		}

		currentBlock = s.eth.BlockChain().GetBlockByNumber(currentBlock.NumberU64() - 1)
	}
	return nil, ethereum.NotFound
}

// isShastaBlock checks if the given block is a Shasta block by inspecting its first transaction's data.
func isShastaBlock(block *types.Block) bool {
	if block == nil {
		return false
	}
	txs := block.Transactions()
	if txs.Len() == 0 {
		return false
	}
	return bytes.HasPrefix(txs[0].Data(), taiko.AnchorV4Selector)
}

// GetSyncMode returns the node sync mode.
func (s *TaikoAPIBackend) GetSyncMode() (string, error) {
	return s.eth.config.SyncMode.String(), nil
}

// TaikoAuthAPIBackend handles L2 node related authorized RPC calls.
type TaikoAuthAPIBackend struct {
	eth *Ethereum
}

// NewTaikoAuthAPIBackend creates a new TaikoAuthAPIBackend instance.
func NewTaikoAuthAPIBackend(eth *Ethereum) *TaikoAuthAPIBackend {
	return &TaikoAuthAPIBackend{eth}
}

// SetHeadL1Origin sets the latest L2 block's corresponding L1 origin.
func (a *TaikoAuthAPIBackend) SetHeadL1Origin(blockID *math.HexOrDecimal256) *hexutil.Big {
	rawdb.WriteHeadL1Origin(a.eth.ChainDb(), (*big.Int)(blockID))
	return (*hexutil.Big)(blockID)
}

// UpdateL1Origin updates the L2 block's corresponding L1 origin.
func (a *TaikoAuthAPIBackend) UpdateL1Origin(l1Origin *rawdb.L1Origin) *rawdb.L1Origin {
	rawdb.WriteL1Origin(a.eth.ChainDb(), l1Origin.BlockID, l1Origin)
	return l1Origin
}

// SetL1OriginSignature sets the L1 origin signature for the given block ID.
func (a *TaikoAuthAPIBackend) SetL1OriginSignature(blockID *big.Int, signature [65]byte) (*rawdb.L1Origin, error) {
	l1Origin, err := rawdb.ReadL1Origin(a.eth.ChainDb(), blockID)
	if err != nil {
		return nil, err
	}

	l1Origin.Signature = signature
	rawdb.WriteL1Origin(a.eth.ChainDb(), blockID, l1Origin)

	return l1Origin, nil
}

// TxPoolContent retrieves the transaction pool content with the given upper limits.
func (a *TaikoAuthAPIBackend) TxPoolContent(
	beneficiary common.Address,
	baseFee *big.Int,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	locals []string,
	maxTransactionsLists uint64,
) ([]*miner.PreBuiltTxList, error) {
	log.Debug(
		"Fetching L2 pending transactions finished",
		"baseFee", baseFee,
		"blockMaxGasLimit", blockMaxGasLimit,
		"maxBytesPerTxList", maxBytesPerTxList,
		"maxTransactions", maxTransactionsLists,
		"locals", locals,
	)

	return a.eth.Miner().BuildTransactionsLists(
		beneficiary,
		baseFee,
		blockMaxGasLimit,
		maxBytesPerTxList,
		locals,
		maxTransactionsLists,
	)
}

// TxPoolContentWithMinTip retrieves the transaction pool content with the given upper limits and minimum tip.
func (a *TaikoAuthAPIBackend) TxPoolContentWithMinTip(
	beneficiary common.Address,
	baseFee *big.Int,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	locals []string,
	maxTransactionsLists uint64,
	minTip uint64,
) ([]*miner.PreBuiltTxList, error) {
	log.Debug(
		"Fetching L2 pending transactions finished",
		"baseFee", baseFee,
		"blockMaxGasLimit", blockMaxGasLimit,
		"maxBytesPerTxList", maxBytesPerTxList,
		"maxTransactions", maxTransactionsLists,
		"locals", locals,
		"minTip", minTip,
	)

	return a.eth.Miner().BuildTransactionsListsWithMinTip(
		beneficiary,
		baseFee,
		blockMaxGasLimit,
		maxBytesPerTxList,
		locals,
		maxTransactionsLists,
		minTip,
	)
}
