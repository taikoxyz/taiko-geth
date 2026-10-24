package catalyst

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
)

// TestTaikoForkchoiceRewindIgnoresReorgDepthLimit pins that on a Taiko chain a
// forkchoice update to an older canonical block rewinds the head to it, however
// deep the rewind is. A non-Taiko node ignores such an update as a beacon
// client resync; a Taiko driver relies on it to reorg the L2 chain.
//
// The node runs with the production default reorg depth limit, which the test
// harness otherwise leaves at 0 (no limit), so the rewind below is deeper than
// the limit a non-Taiko node would enforce.
func TestTaikoForkchoiceRewindIgnoresReorgDepthLimit(t *testing.T) {
	genesis, blocks := generateMergeChain(40, true)
	n, ethservice := startEthService(t, genesis, blocks, func(c *ethconfig.Config) {
		c.EngineMaxReorgDepth = ethconfig.Defaults.EngineMaxReorgDepth
	})
	defer n.Close()

	ethservice.BlockChain().Config().Taiko = true
	api := newConsensusAPIWithoutHeartbeat(ethservice)

	if head := ethservice.BlockChain().CurrentBlock().Number.Uint64(); head != 40 {
		t.Fatalf("head = %d before the rewind, want 40", head)
	}
	target := blocks[4]
	if target.NumberU64() != 5 {
		t.Fatalf("target block number = %d, want 5", target.NumberU64())
	}
	// Without a limit below the rewind depth this test passes on any node, Taiko
	// or not, and no longer pins the Taiko branch.
	if api.maxReorgDepth != 32 {
		t.Fatalf("reorg depth limit = %d, want the default 32, below the depth-35 rewind", api.maxReorgDepth)
	}

	resp, err := api.ForkchoiceUpdatedV1(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: target.Hash()}, nil)
	if err != nil {
		t.Fatalf("ForkchoiceUpdatedV1: %v", err)
	}
	if resp.PayloadStatus.Status != engine.VALID {
		t.Fatalf("forkchoice status = %s, want %s", resp.PayloadStatus.Status, engine.VALID)
	}
	if head := ethservice.BlockChain().CurrentBlock().Number.Uint64(); head != 5 {
		t.Fatalf("head = %d after a depth-35 rewind, want 5", head)
	}
}
