// Copyright 2026 The Taiko Authors
// This file is part of the go-ethereum library.
//
// Licensed under the GNU Lesser General Public License v3 or later.

package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/params"
)

// TestTaikoNeverUsesParallelExecution pins that a Taiko block never takes the
// BAL-driven parallel processor. That path carries none of the Taiko hooks in
// StateProcessor.Process (anchor marking, basefee sharing, zk gas metering and
// truncation, the Unzen post-checks), so a Taiko block on it would be executed
// with different semantics. The dispatch is Amsterdam-gated and no Taiko network
// schedules Amsterdam; if one ever does, this test fails and the dispatch needs a
// Taiko guard.
func TestTaikoNeverUsesParallelExecution(t *testing.T) {
	const blockTime = uint64(2_000_000_000) // past Unzen on every Taiko network

	// The access list is the other half of the dispatch condition, so the block
	// carries one: only the fork gate can keep it off the parallel path.
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       blockTime,
		Difficulty: big.NewInt(1), // Unzen stores the block zk gas here
	}
	block := types.NewBlockWithHeader(header).WithAccessListUnsafe(&bal.BlockAccessList{})
	if block.AccessList() == nil {
		t.Fatal("test block lost its access list")
	}

	configs := map[string]*params.ChainConfig{
		"unzen-test": unzenTestChainConfig(t),
		"mainnet":    TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64()).Config,
		"hoodi":      TaikoGenesisBlock(params.TaikoHoodiNetworkID.Uint64()).Config,
		"internal":   TaikoGenesisBlock(params.TaikoInternalNetworkID.Uint64()).Config,
	}
	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			if !config.Taiko {
				t.Fatal("config is not a Taiko config")
			}
			if !config.IsUnzen(blockTime) {
				t.Fatal("block time is not past Unzen")
			}
			if supportsParallelExecution(block, config, false, false, false) {
				t.Fatal("Taiko block took the BAL-parallel path")
			}
		})
	}

	// Control: the same block on an Amsterdam config does take the parallel
	// path, so the checks above are not passing for a reason other than the
	// fork gate.
	amsterdam := *unzenTestChainConfig(t)
	amsterdam.AmsterdamTime = new(uint64)
	if !supportsParallelExecution(block, &amsterdam, false, false, false) {
		t.Fatal("control: Amsterdam block with an access list did not select the parallel path")
	}
}
