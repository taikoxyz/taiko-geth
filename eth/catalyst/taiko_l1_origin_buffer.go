package catalyst

import (
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

// pendingL1OriginsCapacity bounds the number of buffered L1 origins awaiting canonical
// promotion. Locally built blocks are normally promoted within the same insert sequence, so
// the buffer only accumulates when payloads are built without being imported; the cap bounds
// that growth while leaving plenty of slack for in-flight blocks.
const pendingL1OriginsCapacity = 64

// pendingL1Origin is a locally built payload's L1 origin awaiting canonical promotion of its
// block.
type pendingL1Origin struct {
	l1Origin *rawdb.L1Origin // L1 origin with L2BlockHash set to the sealed block hash
	batchID  *big.Int        // batch ID from the payload attributes, nil when absent
}

// pendingL1OriginEntry pairs a buffered origin with the sealed block hash that keys it.
type pendingL1OriginEntry struct {
	blockHash common.Hash
	pending   pendingL1Origin
}

// pendingL1Origins buffers L1 origins of locally built payloads until their block becomes
// the canonical head.
//
// Entries are persisted to the database only once a forkchoice update promotes the built
// block to canonical head; entries whose block is never promoted (e.g. build-only previews)
// are evicted once the buffer exceeds capacity and never reach the database.
type pendingL1Origins struct {
	mu      sync.Mutex
	entries []pendingL1OriginEntry // insertion order, bounded by pendingL1OriginsCapacity
}

// stash buffers a built payload's origin, replacing any entry for the same block hash and
// evicting the oldest entry beyond capacity.
func (p *pendingL1Origins) stash(blockHash common.Hash, pending pendingL1Origin) {
	p.mu.Lock()
	defer p.mu.Unlock()

	kept := p.entries[:0]
	for _, entry := range p.entries {
		if entry.blockHash != blockHash {
			kept = append(kept, entry)
		}
	}
	p.entries = append(kept, pendingL1OriginEntry{blockHash: blockHash, pending: pending})
	if len(p.entries) > pendingL1OriginsCapacity {
		p.entries = append(p.entries[:0], p.entries[len(p.entries)-pendingL1OriginsCapacity:]...)
	}
}

// take removes and returns the buffered entry for the given block hash, if any.
func (p *pendingL1Origins) take(blockHash common.Hash) (pendingL1Origin, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, entry := range p.entries {
		if entry.blockHash == blockHash {
			p.entries = append(p.entries[:i], p.entries[i+1:]...)
			return entry.pending, true
		}
	}
	return pendingL1Origin{}, false
}

// flushPendingL1Origin persists the buffered L1 origin for the newly promoted canonical
// head, if that block was built locally.
//
// L1 origins are buffered at payload-build time and must only reach the database once the
// built block is canonical; persisting earlier would record rows for blocks that may never
// exist on the canonical chain.
func (api *ConsensusAPI) flushPendingL1Origin(head common.Hash) {
	pending, ok := api.pendingL1Origins.take(head)
	if !ok {
		return
	}

	rawdb.WriteL1Origin(api.eth.ChainDb(), pending.l1Origin.BlockID, pending.l1Origin)

	// Only advance the head pointer and the batch mapping for non-preconfirmation blocks.
	if !pending.l1Origin.IsPreconfBlock() {
		rawdb.WriteHeadL1Origin(api.eth.ChainDb(), pending.l1Origin.BlockID)
		if pending.batchID != nil {
			rawdb.WriteBatchToLastBlockID(api.eth.ChainDb(), pending.batchID, pending.l1Origin.BlockID)
		}
	}
}
