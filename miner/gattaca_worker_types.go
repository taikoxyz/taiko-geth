package miner

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core"
)

// PreconfState holds all information about any blocks that have been pre-confirmed
// and any pending changes that are going to be pre-confirmed.
type PreconfState struct {
	// sealedPreconfBlocks holds an ordered list of pre-confirmed blocks that are ahead of the canonical chain head.
	sealedPreconfBlocks []*environment
	// pendingPreconfBlock is the current pre-confirmed block that is being built but not yet sealed.
	// It is nil if there is no pending pre-conf block.
	pendingPreconfBlock *environment
	// chain represents the current canonical blockchain.
	chain *core.BlockChain
}

// NewPreconfState initializes a new PreconfState with empty sealed and pending preconf blocks.
// It requires a reference to the canonical blockchain.
func NewPreconfState(chain *core.BlockChain) *PreconfState {
	return &PreconfState{
		chain: chain,
	}
}

// SealPendingPreconfBlock moves the current pending preconf block to the sealedPreconfBlocks list.
// It clears the pendingPreconfBlock after sealing.
//
// Returns an error if there is no pending preconf block to seal.
func (state *PreconfState) SealPendingPreconfBlock() error {
	if state.pendingPreconfBlock == nil {
		return errors.New("no pending preconf block to seal")
	}

	state.sealedPreconfBlocks = append(state.sealedPreconfBlocks, state.pendingPreconfBlock)
	state.pendingPreconfBlock = nil

	return nil
}

// OnNewChainHeadEvent processes a new chain head event by clearing any sealed preconf blocks
// that have been incorporated into the canonical chain. It verifies that the hashes of
// the sealed preconf blocks match those in the canonical chain.
//
// Returns an error if there is a hash mismatch or if a pending preconf block becomes stale.
func (state *PreconfState) OnNewChainHeadEvent(event *core.ChainHeadEvent) error {
	eventBlockNumber := event.Block.NumberU64()

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
			break
		}
	}

	return nil
}

// CurrentBlockNumber retrieves the latest known block number. It prioritises the last sealed preconf block
// if available, otherwise, it falls back to the canonical chain head.
func (state *PreconfState) CurrentBlockNumber() *big.Int {
	numSealedPreconfBlocks := len(state.sealedPreconfBlocks)
	if numSealedPreconfBlocks > 0 {
		return state.sealedPreconfBlocks[numSealedPreconfBlocks-1].header.Number
	}
	return state.chain.CurrentBlock().Number
}
