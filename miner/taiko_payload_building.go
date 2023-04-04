package miner

import (
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// SetFullBlock updates the full-block to the given block.
func (payload *Payload) SetFullBlock(block *types.Block, fees *big.Int) {
	payload.lock.Lock()
	defer payload.lock.Unlock()

	payload.full = block
	payload.fullFees = fees

	feesInEther := new(big.Float).Quo(new(big.Float).SetInt(fees), big.NewFloat(params.Ether))
	log.Info("Updated payload", "id", payload.id, "number", block.NumberU64(), "hash", block.Hash(),
		"txs", len(block.Transactions()), "gas", block.GasUsed(), "fees", feesInEther,
		"root", block.Root())

	go func() { payload.stop <- struct{}{} }()
	payload.cond.Broadcast() // fire signal for notifying full block
}
