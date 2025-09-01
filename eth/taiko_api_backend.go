package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

const witnessCacheSize = 256

// TaikoAPIBackend handles L2 node related RPC calls.
type TaikoAPIBackend struct {
	mu           sync.RWMutex
	eth          *Ethereum
	witnessCache *lru.Cache[rpc.BlockNumber, hexutil.Bytes]
}

// NewTaikoAPIBackend creates a new TaikoAPIBackend instance.
func NewTaikoAPIBackend(eth *Ethereum) *TaikoAPIBackend {
	return &TaikoAPIBackend{
		eth:          eth,
		witnessCache: lru.NewCache[rpc.BlockNumber, hexutil.Bytes](witnessCacheSize),
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

// TaikoAuthAPIBackend handles L2 node related authorized RPC calls.
type TaikoAuthAPIBackend struct {
	eth *Ethereum
}

// NewTaikoAuthAPIBackend creates a new TaikoAuthAPIBackend instance.
func NewTaikoAuthAPIBackend(eth *Ethereum) *TaikoAuthAPIBackend {
	return &TaikoAuthAPIBackend{eth}
}

// SetHeadL1Origin sets the latest L2 block's corresponding L1 origin.
func (a *TaikoAuthAPIBackend) SetHeadL1Origin(blockID *math.HexOrDecimal256) *big.Int {
	rawdb.WriteHeadL1Origin(a.eth.ChainDb(), (*big.Int)(blockID))
	return (*big.Int)(blockID)
}

// UpdateL1Origin updates the L2 block's corresponding L1 origin.
func (a *TaikoAuthAPIBackend) UpdateL1Origin(l1Origin *rawdb.L1Origin) *rawdb.L1Origin {
	rawdb.WriteL1Origin(a.eth.ChainDb(), l1Origin.BlockID, l1Origin)
	return l1Origin
}

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

// blockByNumber is the wrapper of the chain access function offered by the backend.
// It will return an error if the block is not found.
func (s *TaikoAPIBackend) blockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	block, err := s.eth.APIBackend.BlockByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("block #%d not found", number)
	}
	return block, nil
}

// GetWitness retrieves the witness for a given block number.
func (s *TaikoAPIBackend) GetWitness(ctx context.Context, number rpc.BlockNumber) (hexutil.Bytes, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res, ok := s.witnessCache.Get(number)
	if ok {
		return res, nil
	}
	res, err := s.getWitness(ctx, number)
	if err != nil {
		return nil, err
	}
	s.witnessCache.Add(number, res)
	return res, nil
}

func (s *TaikoAPIBackend) getWitness(ctx context.Context, number rpc.BlockNumber) (hexutil.Bytes, error) {
	if number < 1 {
		return nil, errors.New("genesis is not traceable")
	}
	block, err := s.blockByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	parentNumber := number - 1
	parentBlock, err := s.blockByNumber(ctx, parentNumber)
	if err != nil {
		return nil, err
	}
	statedb, release, err := s.eth.APIBackend.StateAtBlock(ctx, parentBlock, 128, nil, true, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get state at block %s: %w", parentNumber, err)
	}
	defer release()

	witness, err := stateless.NewWitness(block.Header(), s.eth.BlockChain().HeaderChain())
	if err != nil {
		return nil, err
	}
	statedb.StartPrefetcher("witness", witness)
	defer statedb.StopPrefetcher()
	res, err := s.eth.BlockChain().Processor().Process(block, statedb, vm.Config{})
	if err != nil {
		return nil, err
	}
	if err = s.eth.BlockChain().Validator().ValidateState(block, statedb, res, false); err != nil {
		return nil, err
	}
	// type extWitness struct {
	// 	Headers []*types.Header
	// 	Codes   [][]byte
	// 	State   [][]byte
	// }
	b, err := rlp.EncodeToBytes(witness)
	if err != nil {
		return nil, err
	}
	return b, nil
}
