package eth

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
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

// LastL1OriginByBatchID returns the L1 origin of the last block for the given batch.
func (s *TaikoAPIBackend) LastL1OriginByBatchID(batchID *math.HexOrDecimal256) (*rawdb.L1Origin, error) {
	blockID, err := rawdb.ReadBatchToLastBlockID(s.eth.ChainDb(), (*big.Int)(batchID))
	if err != nil {
		return nil, err
	}
	if blockID == nil {
		if blockID, err = s.getLastBlockByBatchId((*big.Int)(batchID)); err != nil {
			return nil, err
		}
		if blockID == nil {
			return nil, ethereum.NotFound
		}
	}
	return s.L1OriginByID((*math.HexOrDecimal256)(blockID))
}

// LastBlockIDByBatchID returns the ID of the last block for the given batch.
func (s *TaikoAPIBackend) LastBlockIDByBatchID(batchID *math.HexOrDecimal256) (*big.Int, error) {
	blockID, err := rawdb.ReadBatchToLastBlockID(s.eth.ChainDb(), (*big.Int)(batchID))
	if err != nil {
		return nil, err
	}
	if blockID != nil {
		return blockID, nil
	}

	return s.getLastBlockByBatchId((*big.Int)(batchID))
}

// GetSyncMode returns the node sync mode.
func (s *TaikoAPIBackend) GetSyncMode() (string, error) {
	return s.eth.config.SyncMode.String(), nil
}

// getLastBlockByBatchId traverses the blockchain backwards to find the last Shasta block of the given Shasta batch ID.
func (s *TaikoAPIBackend) getLastBlockByBatchId(batchID *big.Int) (*big.Int, error) {
	currentBlock := s.eth.BlockChain().GetBlockByNumber(s.eth.blockchain.CurrentHeader().Number.Uint64())

	for currentBlock != nil &&
		currentBlock.Transactions().Len() > 0 &&
		bytes.HasPrefix(currentBlock.Transactions()[0].Data(), taiko.AnchorV4Selector) {
		if currentBlock.NumberU64() == 0 {
			break
		}
		// Decode the AnchorV4 calldata to fetch the proposal ID for comparison.
		proposalID, err := anchorV4ProposalID(currentBlock.Transactions()[0].Data())
		if err != nil {
			return nil, err
		}
		if proposalID.Cmp(batchID) == 0 {
			return currentBlock.Number(), nil
		}

		currentBlock = s.eth.BlockChain().GetBlockByNumber(currentBlock.NumberU64() - 1)
	}
	return nil, ethereum.NotFound
}

// anchorV4ProposalID extracts the proposal ID encoded inside an AnchorV4 transaction's calldata.
func anchorV4ProposalID(txData []byte) (*big.Int, error) {
	if len(txData) < len(taiko.AnchorV4Selector) {
		return nil, fmt.Errorf("anchorV4 tx data too short: %d", len(txData))
	}
	if !bytes.HasPrefix(txData, taiko.AnchorV4Selector) {
		return nil, fmt.Errorf("invalid anchorV4 selector")
	}

	// Calldata layout: 4-byte selector + ABI-encoded arguments. Skip selector so we can
	// reason about the argument area directly.
	args := txData[len(taiko.AnchorV4Selector):]
	if len(args) < 32 {
		return nil, fmt.Errorf("anchorV4 calldata missing proposal params offset")
	}

	// The first 32 bytes hold the offset (relative to args start) where the proposal tuple lives.
	offset := new(big.Int).SetBytes(args[:32])
	if !offset.IsUint64() {
		return nil, fmt.Errorf("anchorV4 proposal params offset too large")
	}
	offsetU64 := offset.Uint64()
	if offsetU64 > uint64(len(args)) || offsetU64+32 > uint64(len(args)) {
		return nil, fmt.Errorf("anchorV4 proposal params offset %d out of bounds (len=%d)", offsetU64, len(args))
	}

	// Slice out the proposalId slot (first field inside the tuple) and convert to big.Int.
	return new(big.Int).SetBytes(args[offsetU64 : offsetU64+32]), nil
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

// SetBatchToLastBlock sets the mapping from batch ID to the last block ID in this batch.
func (a *TaikoAuthAPIBackend) SetBatchToLastBlock(
	batchID *math.HexOrDecimal256,
	blockID *math.HexOrDecimal256,
) *big.Int {
	rawdb.WriteBatchToLastBlockID(a.eth.ChainDb(), (*big.Int)(batchID), (*big.Int)(blockID))
	return (*big.Int)(batchID)
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
