package miner

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/log"
	"math/big"
)

// PreconfState holds all information about any blocks that have been pre-confirmed
// and any pending changes that are going to be pre-confirmed.
type PreconfState struct {
	// Ordered list of all applied preconf blocks that are ahead of the canonical chain head.
	sealedPreconfBlocks []*environment
	// Current pending preconf block that is yet to be sealed, nil if we aren't building a pending block.
	pendingPreconfBlock *environment
	// Current, canonical, chain head
	chain *core.BlockChain
}

// NewPreconfState creates a PreconfState with empty sealed and pending blocks.
func NewPreconfState(chain *core.BlockChain) *PreconfState {
	return &PreconfState{chain: chain}
}

// SealPendingPreconfBlock moves the current pending preconf block into the `sealedPreconfBlocks` list
// and clears the `pendingPreconfBlock`.
func (state *PreconfState) SealPendingPreconfBlock() error {
	if state.pendingPreconfBlock == nil {
		return errors.New("no pending preconf block to seal")
	}

	state.sealedPreconfBlocks = append(state.sealedPreconfBlocks, state.pendingPreconfBlock)
	state.pendingPreconfBlock = nil

	return nil
}

// OnNewChainHeadEvent clears all sealedPreconfBlocks that have been added to the canonical chain.
// This function will return an error if the new block hash mismatches with a sealed preconf block.
func (state *PreconfState) OnNewChainHeadEvent(event *core.ChainHeadEvent) error {
	eventBlockNumber := event.Block.NumberU64()

	if len(state.sealedPreconfBlocks) == 0 {
		// Sanity check that we are not building a preconf block that matches or was before the event number
		if state.pendingPreconfBlock != nil && state.pendingPreconfBlock.header.Number.Uint64() <= eventBlockNumber {
			return errors.New(fmt.Sprintf(
				"building a pending preconf block that is now stale. Pending preconf number: %s, new chain head: %d",
				state.pendingPreconfBlock.header.Number.String(),
				eventBlockNumber,
			))
		}

		return nil
	}

	// We have sealed preconf blocks so clear any that have been added to the chain + verify our hashes match
	for index, preconfBlock := range state.sealedPreconfBlocks {
		if preconfBlock.header.Number.Uint64() > eventBlockNumber {
			break
		}

		_ = index

	}

	log.Info("OnNewChainHeadEvent")

	return nil
}

// CurrentBlockNumber fetches the latest known, sealed block. It first checks `sealedPreconfBlocks`,
// if these are empty it will use the chain head.
func (state *PreconfState) CurrentBlockNumber() *big.Int {
	numSealedPreconfBlocks := len(state.sealedPreconfBlocks)
	if numSealedPreconfBlocks > 0 {
		return state.sealedPreconfBlocks[numSealedPreconfBlocks-1].header.Number
	}
	return state.chain.CurrentBlock().Number
}
