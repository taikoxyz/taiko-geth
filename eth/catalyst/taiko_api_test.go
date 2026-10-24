package catalyst

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
)

// TestTaikoForkchoiceRewindIgnoresReorgDepthLimit pins that on a Taiko chain a
// forkchoice update to an older canonical block rewinds the head to it, however
// deep the rewind is. A non-Taiko node ignores such an update as a beacon
// client resync; a Taiko driver relies on it to reorg the L2 chain.
func TestTaikoForkchoiceRewindIgnoresReorgDepthLimit(t *testing.T) {
	genesis, blocks := generateMergeChain(40, true)
	n, ethservice := startEthService(t, genesis, blocks)
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
