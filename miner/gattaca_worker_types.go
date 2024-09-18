package miner

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/log"
)

type StateId uint32

const (
	LatestSealedId StateId = iota
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
	stateIdMap map[uint32]*environment
	// chain represents the current canonical blockchain.
	chain *core.BlockChain
	// commitMutex ensures that operations modifying the pendingPreconfBlock are thread-safe.
	commitMutex sync.Mutex
}

// NewPreconfState initializes a new PreconfState with empty sealed and pending preconf blocks.
// It requires a reference to the canonical blockchain.
func NewPreconfState(chain *core.BlockChain) *PreconfState {
	return &PreconfState{
		chain: chain,
	}
}

// sealPendingPreconfBlock moves the current pending preconf block to the sealedPreconfBlocks list.
// It clears the pendingPreconfBlock after sealing.
//
// sealPendingPreconfBlock will also clear the stateIdMap. TBC if we want to keep this logic.
//
// Returns an error if there is no pending preconf block to seal.
func (state *PreconfState) sealPendingPreconfBlock() error {
	if state.pendingPreconfBlock == nil {
		return errors.New("no pending preconf block to seal")
	}
	log.Info("Sealing pending preconf block", "blockNumber", state.pendingPreconfBlock.header.Number.Uint64())

	state.sealedPreconfBlocks = append(state.sealedPreconfBlocks, state.pendingPreconfBlock)
	log.Info("Pending preconf block sealed", "totalSealedBlocks", len(state.sealedPreconfBlocks))

	state.pendingPreconfBlock = nil
	state.stateIdMap = make(map[uint32]*environment)
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
		preconfBlockNumber := preconfBlock.header.Number.Uint64()

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
			if chainBlock.Hash() != preconfBlock.header.Hash() {
				return fmt.Errorf(
					"sealed preconf block hash mismatch: expected %s, got %s for block number %d",
					preconfBlock.header.Hash(),
					chainBlock.Hash(),
					preconfBlockNumber,
				)
			}

		case preconfBlockNumber == eventBlockNumber:
			// Verify that the event block's hash matches the preconf block's hash.
			if event.Block.Hash() != preconfBlock.header.Hash() {
				return fmt.Errorf(
					"sealed preconf block hash mismatch: expected %s, got %s for block number %d",
					preconfBlock.header.Hash(),
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
func (state *PreconfState) commitStateIDToPendingBlock(stateId uint32) (uint64, string, error) {
	state.commitMutex.Lock()
	defer state.commitMutex.Unlock()

	if state.pendingPreconfBlock == nil {
		return 0, "", fmt.Errorf("attempted to commit state ID to a non-existent pending preconf block. stateId: %d", stateId)
	}

	envToCommit, exists := state.stateIdMap[stateId]
	if !exists {
		return 0, "", fmt.Errorf("state for id %d does not exist", stateId)
	}

	log.Info("Committing state ID to pending preconf block", "stateId", stateId, "blockNumber", envToCommit.header.Number.Uint64())

	// Add the environment corresponding to the state ID to the pending preconf block.
	state.pendingPreconfBlock = envToCommit
	log.Info("Pending preconf block updated with committed state", "newPendingBlockNumber", envToCommit.header.Number.Uint64())

	total := uint64(0)
	for _, receipt := range state.pendingPreconfBlock.receipts {
		total += receipt.GasUsed
	}

	formattedBuilderPayment := fmt.Sprintf("0x%x", state.pendingPreconfBlock.cumulativeBuilderPayment)
	return total, formattedBuilderPayment, nil
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

func (state *PreconfState) envAtId(stateId uint32) *environment {
	return state.stateIdMap[stateId]
}
