package miner

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"math/big"
	"math/rand"
	"sync"
)

type StateId uint64

const (
	LatestSealedId StateId = 1
)

// PreconfState holds all information about any blocks that have been pre-confirmed
// and any pending changes that are going to be pre-confirmed.
type PreconfState struct {
	// sealedPreconfBlocks holds an ordered list of pre-confirmed blocks that are ahead of the canonical chain head.
	sealedPreconfBlocks []*environment
	// pendingPreconfBlock is the current pre-confirmed block that is being built but not yet sealed.
	// It is nil if there is no pending pre-conf block.
	pendingPreconfBlock *environment
	// stateIdMap maps all state IDs to their corresponding environments.
	stateIdMap map[uint64]*environment
	// chain represents the current canonical blockchain.
	chain *core.BlockChain
	// commitMutex ensures that operations modifying the pendingPreconfBlock are thread-safe.
	commitMutex sync.Mutex
}

// NewPreconfState initializes a new PreconfState with empty sealed and pending preconf blocks.
// It requires a reference to the canonical blockchain.
func NewPreconfState(chain *core.BlockChain) *PreconfState {
	return &PreconfState{
		chain:               chain,
		stateIdMap:          make(map[uint64]*environment),
		sealedPreconfBlocks: make([]*environment, 0),
	}
}

// CurrentBlock returns the header of the most recently sealed preconfigured block.
// If there are no such blocks, it returns nil.
//
// Returns:
//   - *types.Header: The header of the latest sealed preconfigured block, or nil if none exist.
func (state *PreconfState) CurrentBlock() *types.Header {
	if len(state.sealedPreconfBlocks) > 0 {
		return state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1].header
	}
	return nil
}

// GetPendingBlock returns the header of the current pending preconfigured block.
// If there is no pending block, it returns nil.
func (state *PreconfState) GetPendingBlock() *types.Header {
	if state.pendingPreconfBlock != nil {
		return state.pendingPreconfBlock.header
	}
	return nil
}

// GetHeaderByNumber returns the header of a sealed preconfigured block by its block number.
// If no such block exists, it returns nil.
func (state *PreconfState) GetHeaderByNumber(number uint64) *types.Header {
	for _, env := range state.sealedPreconfBlocks {
		if env.header.Number.Uint64() == number {
			return env.header
		}
	}
	return nil
}

// GetHeaderByHash returns the header of a sealed preconfigured block by its hash.
// If no such block exists, it returns nil.
func (state *PreconfState) GetHeaderByHash(hash common.Hash) *types.Header {
	if state.pendingPreconfBlock != nil && state.pendingPreconfBlock.header.Hash() == hash {
		return state.pendingPreconfBlock.header
	}
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.Hash() == hash {
			return env.header
		}
	}
	return nil
}

// BlockNumber returns the latest block number in the preconfigured state.
// It compares the chain's current block number with the last sealed preconfigured block
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
// If the number is pending, it returns the pending preconfigured block's state and header.
// If a sealed preconfigured block with the specified number exists, it returns its state and header.
// Otherwise, it returns an error.
func (state *PreconfState) StateAndHeaderByNumber(number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	if number == rpc.PendingBlockNumber {
		if state.pendingPreconfBlock != nil {
			return state.pendingPreconfBlock.state, state.pendingPreconfBlock.header, nil
		} else {
			return nil, nil, errors.New("no pending preconf block found")
		}
	}
	no := uint64(number)
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.NumberU64() == no {
			return env.state, env.header, nil
		}
	}
	return nil, nil, errors.New("no sealed preconf block found")
}

// StateAndHeaderByhash returns the state database and header of a block specified by hash.
// If a sealed preconfigured block with the specified hash exists, it returns its state and header.
// Otherwise, it returns an error.
func (state *PreconfState) StateAndHeaderByhash(hash common.Hash) (*state.StateDB, *types.Header, error) {
	if state.pendingPreconfBlock != nil && state.pendingPreconfBlock.header.Hash() == hash {
		return state.pendingPreconfBlock.state, state.pendingPreconfBlock.header, nil
	}
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.Hash() == hash {
			return env.state, env.header, nil
		}
	}
	return nil, nil, errors.New("no sealed preconf block found")
}

// GetReceipts returns the receipts of a sealed preconfigured block specified by its hash.
// If the block is found, it returns its receipts.
// If no such block exists, it returns an error.
func (state *PreconfState) GetReceipts(hash common.Hash) (types.Receipts, error) {
	if state.pendingPreconfBlock != nil && state.pendingPreconfBlock.header.Hash() == hash {
		return state.pendingPreconfBlock.receipts, nil
	}
	for _, env := range state.sealedPreconfBlocks {
		if env.sealedBlock.Hash() == hash {
			return env.receipts, nil
		}
	}
	return nil, nil
}

// BlockByNumber returns the block specified by number.
// - For `LatestBlockNumber`, it returns the latest block among the sealed preconfigured blocks and the chain's latest block.
// - For a specific block number, it returns the corresponding sealed preconfigured block if it exists; otherwise, it fetches the block from the chain.
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
// It first searches the sealed preconfigured blocks.
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

// GetPoolNonce returns the account nonce for a given address in the preconfigured state.
// It first checks the pending preconfigured block's state, then the last sealed preconfigured block's state.
// If neither exists, it returns zero.
func (state *PreconfState) GetPoolNonce(addr common.Address) uint64 {
	if state.pendingPreconfBlock != nil {
		return state.pendingPreconfBlock.state.GetNonce(addr)
	}
	if len(state.sealedPreconfBlocks) > 0 {
		return state.sealedPreconfBlocks[len(state.sealedPreconfBlocks)-1].state.GetNonce(addr)
	}
	return 0
}

func (state *PreconfState) GetTransaction(hash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64, error) {
	if state.pendingPreconfBlock != nil {
		found, tx, blockHash, blockIndex, txIndex, err := state.getTransactionInEnv(state.pendingPreconfBlock, hash)
		if found {
			return found, tx, blockHash, blockIndex, txIndex, err
		}
	}
	for _, sealedPreconfBlock := range state.sealedPreconfBlocks {
		found, tx, blockHash, blockIndex, txIndex, err := state.getTransactionInEnv(sealedPreconfBlock, hash)
		if found {
			return found, tx, blockHash, blockIndex, txIndex, err
		}
	}
	return false, nil, common.Hash{}, 0, 0, nil
}

func (state *PreconfState) getTransactionInEnv(env *environment, hash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64, error) {
	for idx, tx := range env.txs {
		log.Info("hashes", "toSearch", hash.Hex(), "storedTx", tx.Hash().Hex())
		if tx.Hash().Hex() == hash.Hex() {
			hash := common.Hash{}
			if env.sealedBlock == nil {
				hash = env.header.Hash()
			}
			return true, tx, hash, env.header.Number.Uint64(), uint64(idx), nil
		}
	}
	return false, nil, common.Hash{}, 0, 0, nil
}

func (state *PreconfState) getPendingPreconfBlock() (*environment, error) {
	if state.pendingPreconfBlock == nil {
		return nil, errors.New("no pending preconf block found")
	}
	return state.pendingPreconfBlock, nil
}

// getLatestPreconfBlock return the last available preconf block.
// pendingPreconfBlock is preferred over the sealed blocks
func (state *PreconfState) getLatestPreconfBlock() *environment {
	if state.pendingPreconfBlock != nil {
		return state.pendingPreconfBlock
	}
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
	newStateId := rand.Uint64()
	state.stateIdMap[newStateId] = env.copy()
	return newStateId
}

// setPendingPreconfBlock set the pendingPreconfBlock with the input environment.
// If the pendingPreconfBlock is not nil, a log.Warn is printed and the pendingPreconfBlock is overwritten.
func (state *PreconfState) setPendingPreconfBlock(pendingPreconfBlock *environment) {
	if state.pendingPreconfBlock != nil {
		log.Warn("current block is not sealed, overwrite pending block environment")
	}
	state.pendingPreconfBlock = pendingPreconfBlock
}

// sealPendingPreconfBlock moves the current pending preconf block to the sealedPreconfBlocks list.
// It clears the pendingPreconfBlock after sealing.
//
// sealPendingPreconfBlock will also clear the stateIdMap. TBC if we want to keep this logic.
//
// Returns an error if there is no pending preconf block to seal.
func (state *PreconfState) sealPendingPreconfBlock(sealedBlockHash common.Hash) error {
	if state.pendingPreconfBlock == nil {
		return errors.New("no pending preconf block to seal")
	}
	log.Info("Sealing pending preconf block", "blockNumber", state.pendingPreconfBlock.header.Number.Uint64())

	// Update the block hash in all receipts for the pendingPreconfBlock. We don't know the hash until sealing so can
	// only do this now.
	for _, receipt := range state.pendingPreconfBlock.receipts {
		receipt.BlockHash = sealedBlockHash
	}
	for _, receipt := range state.pendingPreconfBlock.hashReceipts {
		receipt.BlockHash = sealedBlockHash
	}

	// Add the pending preconf block to our sealed blocks.
	state.sealedPreconfBlocks = append(state.sealedPreconfBlocks, state.pendingPreconfBlock)
	log.Info("Pending preconf block sealed", "totalSealedBlocks", len(state.sealedPreconfBlocks))

	state.pendingPreconfBlock = nil
	state.stateIdMap = make(map[uint64]*environment)
	return nil
}

// onNewChainHeadEvent processes a new chain head event by clearing any sealed preconf blocks
// that have been incorporated into the canonical chain. It verifies that the hashes of
// the sealed preconf blocks match those in the canonical chain.
//
// Returns an error if there is a hash mismatch or if a pending preconf block becomes stale.
func (state *PreconfState) onNewChainHeadEvent(event *core.ChainHeadEvent) error {
	eventBlockNumber := event.Block.NumberU64()
	log.Info("Processing new chain head event", "eventBlockNumber", eventBlockNumber)

	// If there are no sealed preconf blocks, perform a sanity check on the pending preconf block.
	if len(state.sealedPreconfBlocks) == 0 {
		if state.pendingPreconfBlock != nil && state.pendingPreconfBlock.header.Number.Uint64() <= eventBlockNumber {
			return fmt.Errorf(
				"building a pending preconf block that is now stale. Pending preconf number: %s, new chain head: %d",
				state.pendingPreconfBlock.header.Number.String(),
				eventBlockNumber,
			)
		}
		return nil
	}

	// Iterate over sealed preconf blocks to verify their inclusion in the canonical chain.
	for index, preconfBlock := range state.sealedPreconfBlocks {
		preconfBlockNumber := preconfBlock.sealedBlock.Number().Uint64()

		switch {
		case preconfBlockNumber < eventBlockNumber:
			// Verify that the preconf block exists in the canonical chain and hashes match.
			chainBlock := state.chain.GetBlockByNumber(preconfBlockNumber)
			if chainBlock == nil {
				return fmt.Errorf(
					"canonical chain missing block number %d referenced by preconf block",
					preconfBlockNumber,
				)
			}
			if chainBlock.Hash() != preconfBlock.sealedBlock.Hash() {
				return fmt.Errorf(
					"sealed preconf block hash mismatch: expected %s, got %s for block number %d",
					preconfBlock.sealedBlock.Hash(),
					chainBlock.Hash(),
					preconfBlockNumber,
				)
			}

		case preconfBlockNumber == eventBlockNumber:
			// Verify that the event block's hash matches the preconf block's hash.
			if event.Block.Hash() != preconfBlock.sealedBlock.Hash() {
				return fmt.Errorf(
					"sealed preconf block hash mismatch: expected %s, got %s for block number %d",
					preconfBlock.sealedBlock.Hash(),
					event.Block.Hash(),
					preconfBlockNumber,
				)
			}

		case preconfBlockNumber > eventBlockNumber:
			// Truncate the sealedPreconfBlocks slice to remove blocks beyond the current event block number.
			state.sealedPreconfBlocks = state.sealedPreconfBlocks[index:]
			log.Info("Sealed preconf blocks truncated", "remainingSealedBlocks", len(state.sealedPreconfBlocks))
			break
		}
	}

	log.Info("Finished processing sealed preconf blocks against new chain head")
	return nil
}

// commitStateIDToPendingBlock takes a state ID from the map and adds the changes from that state ID
// to the pending preconf block. Returns the cumulative gas used and builderPayment of the new pendingPreconfBlock.
func (state *PreconfState) commitStateIDToPendingBlock(stateId uint64) (uint64, *uint256.Int, error) {
	state.commitMutex.Lock()
	defer state.commitMutex.Unlock()

	envToCommit, exists := state.stateIdMap[stateId]
	if !exists {
		return 0, nil, fmt.Errorf("state for id %d does not exist", stateId)
	}

	log.Info("Committing state ID to pending preconf block", "stateId", stateId, "blockNumber", envToCommit.header.Number.Uint64())

	// Add the environment corresponding to the state ID to the pending preconf block.
	state.pendingPreconfBlock = envToCommit
	log.Info("Pending preconf block updated with committed state", "newPendingBlockNumber", envToCommit.header.Number.Uint64())

	totalGas := uint64(0)
	for _, receipt := range state.pendingPreconfBlock.receipts {
		totalGas += receipt.GasUsed
	}

	return totalGas, state.pendingPreconfBlock.cumulativeBuilderPayment, nil
}

// getLatestSealedBlock returns the latest block from sealedPreconfBlocks if there are items in the array.
// Otherwise, it returns nil.
func (state *PreconfState) latestSealedPreconfEnv() *environment {
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
	return state.stateIdMap[stateId]
}
