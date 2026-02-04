package eth

import (
	"bytes"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
)

// ErrProposalLastBlockUncertain indicates the last block for the proposal is not yet deterministic.
var ErrProposalLastBlockUncertain = errors.New(
	"proposal last block uncertain: BatchToLastBlockID missing and no newer proposal observed",
)

// ErrProposalLastBlockLookbackExceeded indicates the last block for the proposal is beyond the max lookback window.
var ErrProposalLastBlockLookbackExceeded = errors.New(
	"proposal last block lookback exceeded: BatchToLastBlockID missing and lookback limit reached",
)

// TaikoAPIBackend handles L2 node related RPC calls.
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

// GetSyncMode returns the node sync mode.
func (s *TaikoAPIBackend) GetSyncMode() (string, error) {
	return s.eth.config.SyncMode.String(), nil
}

// maxBatchLookupBlocks defines the maximum number of blocks to look back
// when searching for the last block of a given batch ID.
const maxBatchLookupBlocks = 192 * 21_600

// TaikoAuthAPIBackend handles L2 node related authorized RPC calls.
type TaikoAuthAPIBackend struct {
	eth *Ethereum
}

// NewTaikoAuthAPIBackend creates a new TaikoAuthAPIBackend instance.
func NewTaikoAuthAPIBackend(eth *Ethereum) *TaikoAuthAPIBackend {
	return &TaikoAuthAPIBackend{eth}
}

// LastL1OriginByBatchID returns the L1 origin of the last block for the given batch.
func (a *TaikoAuthAPIBackend) LastL1OriginByBatchID(batchID *math.HexOrDecimal256) (*rawdb.L1Origin, error) {
	blockID, err := rawdb.ReadBatchToLastBlockID(a.eth.ChainDb(), (*big.Int)(batchID))
	if err != nil && !errors.Is(err, ethereum.NotFound) {
		return nil, err
	}
	if blockID == nil {
		if blockID, err = a.getLastBlockByBatchId((*big.Int)(batchID)); err != nil {
			return nil, err
		}
		if blockID == nil {
			return nil, ethereum.NotFound
		}
	}

	return rawdb.ReadL1Origin(a.eth.ChainDb(), (*big.Int)(blockID))
}

// LastBlockIDByBatchID returns the ID of the last block for the given batch.
func (a *TaikoAuthAPIBackend) LastBlockIDByBatchID(batchID *math.HexOrDecimal256) (*hexutil.Big, error) {
	blockID, err := rawdb.ReadBatchToLastBlockID(a.eth.ChainDb(), (*big.Int)(batchID))
	if err != nil && !errors.Is(err, ethereum.NotFound) {
		return nil, err
	}
	if blockID != nil {
		return blockID, nil
	}

	return a.getLastBlockByBatchId((*big.Int)(batchID))
}

// getLastBlockByBatchId traverses the blockchain backwards to find the last Shasta block of the given Shasta batch ID.
func (a *TaikoAuthAPIBackend) getLastBlockByBatchId(batchID *big.Int) (*hexutil.Big, error) {
	// We start from the head L1 origin and traverse backwards until we find
	// the matching batch ID, to ignore all preconfirmation blocks at the chain tip.
	var (
		headNumber   = a.eth.BlockChain().CurrentHeader().Number
		currentBlock = a.eth.BlockChain().GetBlockByNumber(headNumber.Uint64())
		lookedBack   uint64
	)

	for currentBlock != nil &&
		currentBlock.Transactions().Len() > 0 &&
		bytes.HasPrefix(currentBlock.Transactions()[0].Data(), taiko.AnchorV4Selector) {
		if lookedBack >= maxBatchLookupBlocks {
			return nil, ErrProposalLastBlockLookbackExceeded
		}
		lookedBack++
		if currentBlock.NumberU64() == 0 {
			break
		}
		proposalID, err := core.DecodeShastaProposalID(currentBlock.Header().Extra)
		if err != nil {
			return nil, err
		}
		if proposalID.Cmp(batchID) < 0 {
			return nil, ethereum.NotFound
		}
		if proposalID.Cmp(batchID) > 0 {
			currentBlock = a.eth.BlockChain().GetBlockByNumber(currentBlock.NumberU64() - 1)
			continue
		}

		l1Origin, err := rawdb.ReadL1Origin(a.eth.ChainDb(), currentBlock.Number())
		if err != nil && !errors.Is(err, ethereum.NotFound) {
			return nil, err
		}
		// Skip preconfirmation blocks.
		if l1Origin != nil && l1Origin.IsPreconfBlock() {
			currentBlock = a.eth.BlockChain().GetBlockByNumber(currentBlock.NumberU64() - 1)
			continue
		}

		if currentBlock.Number().Cmp(headNumber) == 0 {
			// If we are at the chain tip, ensure the L1 origin is there and not a preconfirmation block.
			if l1Origin == nil || l1Origin.IsPreconfBlock() {
				return nil, ErrProposalLastBlockUncertain
			}
		}
		return (*hexutil.Big)(currentBlock.Number()), nil
	}
	return nil, ethereum.NotFound
}

// SetHeadL1Origin sets the latest L2 block's corresponding L1 origin.
func (a *TaikoAuthAPIBackend) SetHeadL1Origin(blockID *math.HexOrDecimal256) *hexutil.Big {
	rawdb.WriteHeadL1Origin(a.eth.ChainDb(), (*big.Int)(blockID))
	return (*hexutil.Big)(blockID)
}

// SetBatchToLastBlock sets the mapping from batch ID to the last block ID in this batch.
func (a *TaikoAuthAPIBackend) SetBatchToLastBlock(
	batchID *math.HexOrDecimal256,
	blockID *math.HexOrDecimal256,
) *hexutil.Big {
	rawdb.WriteBatchToLastBlockID(a.eth.ChainDb(), (*big.Int)(batchID), (*big.Int)(blockID))
	return (*hexutil.Big)(batchID)
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
