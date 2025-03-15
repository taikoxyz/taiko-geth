package miner

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
)

type StateId uint64

const (
	LatestSealedId     StateId = 0
	StateIdToStartFrom         = uint64(1) // 0 is reserved for latest sealed state
)

// PreconfState holds all information about any blocks that have been pre-confirmed
// and any pending changes that are going to be pre-confirmed.
type PreconfState struct {
	// sealedPreconfBlocks holds an ordered list of pre-confirmed blocks that are ahead of the canonical chain head.
	// The blocks are guaranteed to be ordered by block number in ascending order.
	sealedPreconfBlocks []*environment
	// stateIdMap maps all state IDs to their corresponding environments.
	stateIdMap map[uint64]*environment
	// chain represents the current canonical blockchain.
	chain *core.BlockChain
	// sealedBlockMutex is held by any fn that modifies the sealedPreconfBlocks array.
	// TODO: we should make this a RwLock and read lock when fetching sealedPreconfBlocks state
	sealedBlockMutex sync.Mutex
	stateIdMutex     sync.RWMutex // Mutex for stateIdMap
	// receiptsCache stores the derived receipts in order to not compute them twice
	receiptsCache *lru.Cache[common.Hash, []*types.Receipt]
	// currentStateId is the next stateId to be used
	currentStateId uint64
}

// NewPreconfState initializes a new PreconfState with empty sealed and pending preconf blocks.
// It requires a reference to the canonical blockchain.
func NewPreconfState(chain *core.BlockChain) *PreconfState {
	return &PreconfState{
		chain:               chain,
		stateIdMap:          make(map[uint64]*environment),
		sealedPreconfBlocks: make([]*environment, 0),
		// create an lru cache limited to 32 elements
		receiptsCache:  lru.NewCache[common.Hash, []*types.Receipt](32),
		currentStateId: StateIdToStartFrom,
	}
}

// CurrentBlock returns the most recent block header from either the sealed pre-confirmed blocks
// or the current blockchain head, whichever has the higher block number.
// If no pre-confirmed blocks exist, it returns nil.
//
// Returns:
//   - *types.Header: The header of the latest sealed pre-confirmed block, or nil if none exist.
func (state *PreconfState) CurrentBlock() *types.Header {
	headChain := state.chain.CurrentBlock()
	if len(state.sealedPreconfBlocks) > 0 {
		latestSelaedBlock := state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1].header
		if latestSelaedBlock.Number.Uint64() > headChain.Number.Uint64() {
			return latestSelaedBlock
		}
		return headChain
	}
	return nil
}

func (state *PreconfState) GetLatestSealedBlock() *types.Header {
	if len(state.sealedPreconfBlocks) > 0 {
		return state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1].header
	}
	return nil
}

// GetHeaderByNumber returns the header of a sealed pre-confirmed block by its block number.
// If no such block exists, it returns nil.
func (state *PreconfState) GetHeaderByNumber(number uint64) *types.Header {
	for _, env := range state.sealedPreconfBlocks {
		if env.header.Number.Uint64() == number {
			return env.header
		}
	}
	return nil
}

// GetHeaderByHash returns the header of a sealed pre-confirmed block by its hash.
// If no such block exists, it returns nil.
func (state *PreconfState) GetHeaderByHash(hash common.Hash) *types.Header {
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.Hash() == hash {
			return env.header
		}
	}
	return nil
}

// BlockNumber returns the latest block number in the pre-confirmed state.
// It compares the chain's current block number with the last sealed pre-confirmed block
// and returns the higher of the two.
func (state *PreconfState) BlockNumber() uint64 {
	chainHead := state.chain.CurrentHeader().Number.Uint64()
	if len(state.sealedPreconfBlocks) > 0 {
		lastSealedBlock := state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1]
		if lastSealedBlock.header.Number.Uint64() > chainHead {
			chainHead = lastSealedBlock.header.Number.Uint64()
		}
	}
	return chainHead
}

// StateAndHeaderByNumber returns the state database and header of a block specified by number.
// If a sealed pre-confirmed block with the specified number exists, it returns its state and header.
// Otherwise, it returns an error.
func (state *PreconfState) StateAndHeaderByNumber(number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	no := uint64(number)
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.NumberU64() == no {
			return env.state, env.header, nil
		}
	}
	return nil, nil, errors.New("no sealed preconf block found")
}

// StateAndHeaderByhash returns the state database and header of a block specified by hash.
// If a sealed pre-confirmed block with the specified hash exists, it returns its state and header.
// Otherwise, it returns an error.
func (state *PreconfState) StateAndHeaderByhash(hash common.Hash) (*state.StateDB, *types.Header, error) {
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.Hash() == hash {
			return env.state, env.header, nil
		}
	}
	return nil, nil, errors.New("no sealed preconf block found")
}

// GetReceipts returns the receipts of a sealed pre-confirmed block specified by its hash.
// If the block is found, it returns its receipts.
// If no such block exists, it returns an error.
func (state *PreconfState) GetReceipts(hash common.Hash) (types.Receipts, error) {
	if receipts, ok := state.receiptsCache.Get(hash); ok {
		return receipts, nil
	}
	var receipts types.Receipts
	var err error
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.Hash() == hash {
			receipts, err = deriveReceipts(
				env.receipts,
				state.chain.Config(),
				hash,
				env.header,
				env.txs)
			break
		}
	}
	if len(receipts) > 0 {
		state.receiptsCache.Add(hash, receipts)
		return receipts, err
	}
	return nil, err
}

func deriveReceipts(receipts types.Receipts,
	config *params.ChainConfig,
	blockHash common.Hash,
	header *types.Header,
	txs []*types.Transaction) (types.Receipts, error) {
	var baseFee *big.Int
	if header == nil {
		baseFee = big.NewInt(0)
	} else {
		baseFee = header.BaseFee
	}
	// Compute effective blob gas price.
	var blobGasPrice *big.Int
	if header != nil && header.ExcessBlobGas != nil {
		blobGasPrice = eip4844.CalcBlobFee(config, header)
	}
	err := receipts.DeriveFields(config, blockHash, header.Number.Uint64(), header.Time, baseFee, blobGasPrice, txs)
	return receipts, err
}

// BlockByNumber returns the block specified by number.
// - For `LatestBlockNumber`, it returns the latest block among the sealed pre-confirmed blocks and the chain's latest block.
// - For a specific block number, it returns the corresponding sealed pre-confirmed block if it exists; otherwise, it fetches the block from the chain.
// If the block is not found, it returns an error.
func (state *PreconfState) BlockByNumber(number rpc.BlockNumber) (*types.Block, error) {
	header := state.chain.CurrentBlock()
	latestChainBlock := state.chain.GetBlock(header.Hash(), header.Number.Uint64())
	if number == rpc.LatestBlockNumber {
		if len(state.sealedPreconfBlocks) > 0 {
			block := state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1].sealedBlock
			if latestChainBlock == nil || block.NumberU64() > latestChainBlock.NumberU64() {
				return block, nil
			}
			return latestChainBlock, nil
		}
	}
	for _, sealedPreconfBlock := range state.sealedPreconfBlocks {
		if sealedPreconfBlock.sealedBlock.NumberU64() == uint64(number) {
			return sealedPreconfBlock.sealedBlock, nil
		}
	}
	return state.chain.GetBlockByNumber(uint64(number)), nil

}

// BlockByHash returns the block specified by hash.
// It first searches the sealed pre-confirmed blocks.
// If not found, it fetches the block from the chain.
// If the block is not found, it returns an error.
func (state *PreconfState) BlockByHash(hash common.Hash) (*types.Block, error) {
	for _, sealedPreconfBlock := range state.sealedPreconfBlocks {
		for _, tx := range sealedPreconfBlock.txs {
			if tx.Hash() == hash {
				return sealedPreconfBlock.sealedBlock, nil
			}
		}
	}
	return state.chain.GetBlockByHash(hash), nil
}

// GetPoolNonce returns the account nonce for a given address in the pre-confirmed state.
// It first checks the pending pre-confirmed block's state, then the last sealed pre-confirmed block's state.
// If neither exists, it returns zero.
func (state *PreconfState) GetPoolNonce(addr common.Address) uint64 {
	if len(state.sealedPreconfBlocks) > 0 {
		return state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1].state.GetNonce(addr)
	}
	return 0
}

func (state *PreconfState) GetTransaction(hash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64, error) {
	for _, sealedPreconfBlock := range state.sealedPreconfBlocks {
		found, tx, blockHash, blockIndex, txIndex, err := state.getTransactionFromEnv(sealedPreconfBlock, hash)
		if found {
			return found, tx, blockHash, blockIndex, txIndex, err
		}
	}
	return false, nil, common.Hash{}, 0, 0, nil
}

func (state *PreconfState) getTransactionFromEnv(env *environment, hash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64, error) {
	for idx, tx := range env.txs {
		if tx.Hash() == hash {
			hash := common.Hash{}
			if env.sealedBlock == nil {
				hash = env.header.Hash()
			} else {
				hash = env.sealedBlock.Hash()
			}
			return true, tx, hash, env.header.Number.Uint64(), uint64(idx), nil
		}
	}
	return false, nil, common.Hash{}, 0, 0, nil
}

// getLatestPreconfBlock return the last available preconf block.
// pendingPreconfBlock is preferred over the sealed blocks
func (state *PreconfState) getLatestPreconfBlock() *environment {
	if len(state.sealedPreconfBlocks) > 0 {
		return state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1]
	}
	return nil
}

func (state *PreconfState) getSealedPreconfBlock() []*environment {
	return state.sealedPreconfBlocks
}

// addSimulatedPreconfEnv add an environment to the internal stateIdMap and return the stateId
// in order to retrieve it later on.

func (state *PreconfState) addSimulatedPreconfEnv(env *environment) uint64 {
	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()

	newStateId := rand.Uint64()
	state.stateIdMap[newStateId] = env.copy(state.chain, state.chain.Config())
	return newStateId
}

// sealPendingPreconfBlock moves the environment at stateId to the sealedPreconfBlocks list.
// It clears the stateIdMap after sealing.
//
// Returns an error if the stateId doesn't exist.
func (state *PreconfState) sealPreconfBlock(stateId uint64, sealedBlockHash common.Hash) error {
	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()

	envToSeal, exists := state.stateIdMap[stateId]
	if !exists {
		return fmt.Errorf("no environment found for stateId %d", stateId)
	}
	log.Info("Simulator-WORKER: Sealing environment to preconf block", "blockNumber", envToSeal.header.Number.Uint64())

	// Add the environment to our sealed blocks.
	err := state.addSealedBlock(envToSeal, stateId)
	if err != nil {
		return err
	}

	// Update the block hash in all receipts. We don't know the hash until sealing so can
	// only do this now.
	for _, receipt := range envToSeal.receipts {
		receipt.BlockHash = sealedBlockHash
	}
	for _, receipt := range envToSeal.hashReceipts {
		receipt.BlockHash = sealedBlockHash
	}

	log.Info("Simulator-WORKER: Environment sealed", "totalSealedBlocks", len(state.sealedPreconfBlocks))

	// Clear the stateIdMap
	state.stateIdMap = make(map[uint64]*environment)
	return nil
}

// handleReorg processes potential chain reorganizations by comparing sealed preconf blocks
// with the canonical chain and cleaning up any divergent blocks.
func (state *PreconfState) handleReorg(event *core.ChainHeadEvent) {
	state.sealedBlockMutex.Lock()
	defer state.sealedBlockMutex.Unlock()

	if event == nil {
		log.Warn("Simulator-WORKER: Received nil event")
		return
	}

	// Early return if no sealed blocks
	if len(state.sealedPreconfBlocks) == 0 {
		return
	}

	// Find the first divergence point by comparing with canonical chain
	divergenceIdx := -1
	divergenceCanonicalHash := common.Hash{}
	divergencePreconfHash := common.Hash{}
	divergenceNumber := uint64(0)
	numPreconfBlocksBeforeDivergence := len(state.sealedPreconfBlocks)
	for i, preconfBlock := range state.sealedPreconfBlocks {
		preconfBlockNum := preconfBlock.header.Number.Uint64()

		// Skip if this block number is beyond current canonical chain
		if preconfBlockNum > event.Header.Number.Uint64() {
			continue
		}

		// Get canonical hash either from the new block or chain state
		var canonicalHash common.Hash
		if preconfBlockNum == event.Header.Number.Uint64() {
			canonicalHash = event.Header.Hash()
		} else {
			canonicalHash = state.chain.GetCanonicalHash(preconfBlockNum)
		}

		if canonicalHash != preconfBlock.sealedBlock.Hash() {
			divergenceIdx = i
			divergenceCanonicalHash = canonicalHash
			divergencePreconfHash = preconfBlock.sealedBlock.Hash()
			divergenceNumber = preconfBlockNum
			log.Warn("Simulator-WORKER: Found chain divergence",
				"blockNumber", preconfBlockNum,
				"preconfHash", preconfBlock.sealedBlock.Hash().String(),
				"canonicalHash", canonicalHash.String())
			break
		}
	}

	// If we found a divergence point, clean up from there
	if divergenceIdx >= 0 {
		// Clear all blocks from divergence point onwards
		state.sealedPreconfBlocks = state.sealedPreconfBlocks[:divergenceIdx]

		// Reset state map since it might contain invalid states
		state.resetState()

		// Clear receipts cache for reorged blocks
		state.receiptsCache = lru.NewCache[common.Hash, []*types.Receipt](32)

		log.Warn("Simulator-WORKER: Detected divergence between canonical chain and sealed preconf blocks. Cleaned up divergent blocks",
			"fromIndex", divergenceIdx,
			"remainingBlocks", len(state.sealedPreconfBlocks),
			"canonicalHash", divergenceCanonicalHash.String(),
			"preconfHash", divergencePreconfHash.String(),
			"canonicalNumber", divergenceNumber,
			"numPreconfBlocksBeforeDivergence", numPreconfBlocksBeforeDivergence,
			"numPreconfBlocksAfterDivergence", numPreconfBlocksBeforeDivergence-len(state.sealedPreconfBlocks))
	}
}

func (state *PreconfState) resetState() {
	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()

	state.currentStateId = StateIdToStartFrom
	state.stateIdMap = make(map[uint64]*environment)
}

// onNewChainHeadEvent processes new chain head events and handles potential chain reorganizations.
// It ignores events from preconfirmed blocks to avoid circular processing.
// The function ensures that sealed preconfirmed blocks remain consistent with the canonical chain
// by cleaning up any divergent blocks through handleReorg.
//
// Parameters:
//   - event: The chain head event containing information about the new block
//
// Returns:
//   - error: Returns nil as handleReorg handles all cleanup internally
func (state *PreconfState) onNewChainHeadEvent(event *core.ChainHeadEvent) {
	// info log block hash and txs
	log.Info("Simulator-WORKER: New chain head event",
		"blockHash", event.Header.Hash().String(),
		"txs-hash", len(event.Header.TxHash))

	// Handle any potential reorgs
	state.handleReorg(event)

	log.Info("Simulator-WORKER: Finished processing sealed preconf blocks against new chain head")
}

// calculateStateMetrics returns the cumulative gas used and builder payment for a given state ID.
func (state *PreconfState) calculateStateMetrics(stateId uint64) (uint64, *uint256.Int, error) {
	state.sealedBlockMutex.Lock()
	defer state.sealedBlockMutex.Unlock()

	env, exists := state.stateIdMap[stateId]
	if !exists {
		return 0, nil, fmt.Errorf("state for id %d does not exist", stateId)
	}

	log.Info("Simulator-WORKER: Calculating metrics for state", "stateId", stateId, "blockNumber", env.header.Number.Uint64(), "num_receipts", len(env.receipts))

	totalGas := uint64(0)
	for _, receipt := range env.receipts {
		totalGas += receipt.GasUsed
	}

	return totalGas, env.cumulativeBuilderPayment, nil
}

// getLatestSealedBlock returns the latest block from sealedPreconfBlocks if there are items in the array.
// Otherwise, it returns nil.
func (state *PreconfState) latestSealedPreconfEnv() *environment {
	state.sealedBlockMutex.Lock()
	defer state.sealedBlockMutex.Unlock()

	numPreconfBlocks := len(state.sealedPreconfBlocks)
	if numPreconfBlocks > 0 {
		return state.sealedPreconfBlocks[numPreconfBlocks-1]
	}
	return nil
}

// CurrentBlockNumber retrieves the latest known block number. It prioritises the last sealed preconf block
// if available, otherwise, it falls back to the canonical chain head.
func (state *PreconfState) currentBlockNumber() *big.Int {
	numSealedPreconfBlocks := len(state.sealedPreconfBlocks)
	if numSealedPreconfBlocks > 0 {
		return state.sealedPreconfBlocks[numSealedPreconfBlocks-1].header.Number
	}
	return state.chain.CurrentBlock().Number
}

func (state *PreconfState) envAtId(stateId uint64) *environment {
	state.stateIdMutex.RLock()
	defer state.stateIdMutex.RUnlock()

	if env, exists := state.stateIdMap[stateId]; exists {
		return env
	}
	return nil
}

// addEnvironment adds a new environment to the stateIdMap in a thread-safe manner and returns the assigned stateId
func (p *PreconfState) addEnvironment(env *environment) uint64 {
	if env == nil {
		return 0
	}

	p.stateIdMutex.Lock()
	defer p.stateIdMutex.Unlock()

	if p.stateIdMap == nil {
		p.stateIdMap = make(map[uint64]*environment)
	}

	// Check for overflow - if we're at max uint64, reset to starting point
	if p.currentStateId == math.MaxUint64 {
		p.currentStateId = StateIdToStartFrom
	}

	stateId := p.currentStateId
	p.currentStateId++
	p.stateIdMap[stateId] = env
	return stateId
}

// Add this helper to ensure blocks are always added in order
func (state *PreconfState) addSealedBlock(env *environment, stateId uint64) error {
	state.sealedBlockMutex.Lock()
	defer state.sealedBlockMutex.Unlock()

	newBlockNum := env.header.Number.Uint64()

	// If we have existing blocks, ensure we're adding in order
	if len(state.sealedPreconfBlocks) > 0 {
		lastBlock := state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1]
		if newBlockNum <= lastBlock.header.Number.Uint64() {
			return fmt.Errorf("Simulator-WORKER: attempting to add block %d out of order, last block was %d, stateId %d",
				newBlockNum, lastBlock.header.Number.Uint64(), stateId)
		}
	}

	state.sealedPreconfBlocks = append(state.sealedPreconfBlocks, env)
	return nil
}
