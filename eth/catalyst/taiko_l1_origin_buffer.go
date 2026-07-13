package catalyst

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

// pendingL1OriginsCapacity bounds hash-keyed L1 origins awaiting promotion or retained for
// recent reorg-back. Locally built blocks are normally promoted within one insert sequence;
// build-only previews and recent canonical history share the bounded cache.
const pendingL1OriginsCapacity = 1024

// headL1OriginReconcileLookback bounds pointer repair after a rewind.
const headL1OriginReconcileLookback = 1024

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
	dirty     bool
}

// canonicalL1OriginBlock identifies a block in the newly canonical segment.
type canonicalL1OriginBlock struct {
	number uint64
	hash   common.Hash
}

// pendingL1Origins caches L1 origins of locally built payloads by sealed block hash.
//
// Dirty entries are persisted only once a forkchoice update promotes their exact block.
// Clean entries remain available to restore number-keyed views after a recent reorg-back.
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
	p.entries = append(kept, pendingL1OriginEntry{blockHash: blockHash, pending: pending, dirty: true})
	for len(p.entries) > pendingL1OriginsCapacity {
		evicted := p.entries[0]
		if evicted.dirty {
			log.Warn(
				"Evicting pending L1 origin that was never canonically promoted; if its block is promoted later its origin rows will be missing",
				"blockID", evicted.pending.l1Origin.BlockID,
				"blockHash", evicted.blockHash,
			)
		} else {
			log.Debug("Evicting retained canonical L1 origin metadata", "blockID", evicted.pending.l1Origin.BlockID, "blockHash", evicted.blockHash)
		}
		p.entries = append(p.entries[:0], p.entries[1:]...)
	}
}

// get returns the buffered entry for the given block hash without removing it.
func (p *pendingL1Origins) get(blockHash common.Hash) (pendingL1Origin, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range p.entries {
		if entry.blockHash == blockHash {
			return entry.pending, true
		}
	}
	return pendingL1Origin{}, false
}

// isDirty reports whether a stash has not yet been persisted on canonical promotion.
func (p *pendingL1Origins) isDirty(blockHash common.Hash) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.entries {
		if entry.blockHash == blockHash {
			return entry.dirty
		}
	}
	return false
}

// markPersisted marks a cached entry clean while retaining it for reorg-back.
func (p *pendingL1Origins) markPersisted(blockHash common.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.entries {
		if p.entries[i].blockHash == blockHash {
			p.entries[i].dirty = false
			return
		}
	}
}

// updateStoredOrigin refreshes a clean cached origin with explicit database updates. Dirty
// entries represent a newer build-time value and must win the next persistence pass.
func (p *pendingL1Origins) updateStoredOrigin(blockHash common.Hash, origin *rawdb.L1Origin) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.entries {
		if p.entries[i].blockHash == blockHash && !p.entries[i].dirty {
			p.entries[i].pending.l1Origin = origin
			return
		}
	}
}

// validateTimestamp enforces the future-block rule using the exact locally built origin.
func (p *pendingL1Origins) validateTimestamp(blockHash common.Hash, timestamp, now uint64) error {
	pending, ok := p.get(blockHash)
	if ok && !pending.l1Origin.IsPreconfBlock() && timestamp > now {
		return consensus.ErrFutureBlock
	}
	return nil
}

// reconcileL1OriginTables atomically replaces all custom L1-origin views in the affected
// canonical interval. Missing hash-keyed metadata leaves the corresponding canonical row
// absent instead of retaining metadata for a displaced block.
func reconcileL1OriginTables(
	db ethdb.Database,
	origins *pendingL1Origins,
	first uint64,
	last uint64,
	canonical []canonicalL1OriginBlock,
) error {
	if first > last {
		return fmt.Errorf("invalid L1 origin reconciliation range: %d > %d", first, last)
	}
	firstID := new(big.Int).SetUint64(first)
	lastID := new(big.Int).SetUint64(last)
	batchIDs := make(map[string]*big.Int)
	needsBatchScan := false
	for number := first; ; number++ {
		origin, err := rawdb.ReadL1Origin(db, new(big.Int).SetUint64(number))
		if err != nil {
			return err
		}
		if origin != nil {
			origins.updateStoredOrigin(origin.L2BlockHash, origin)
			if !origin.IsPreconfBlock() {
				needsBatchScan = true
			}
		}
		if number == last {
			break
		}
	}
	// A full prefix scan is only needed when reorged confirmed metadata is no longer
	// available in the bounded cache. Ordinary forward promotion remains O(1).
	if needsBatchScan {
		fallback, err := rawdb.BatchIDsByLastBlockRange(db, firstID, lastID)
		if err != nil {
			return err
		}
		for _, batchID := range fallback {
			batchIDs[batchID.String()] = batchID
		}
	}
	storedHead, err := rawdb.ReadHeadL1Origin(db)
	if err != nil {
		return err
	}

	batch := db.NewBatch()
	for number := first; ; number++ {
		rawdb.DeleteL1Origin(batch, new(big.Int).SetUint64(number))
		if number == last {
			break
		}
	}
	for _, batchID := range batchIDs {
		rawdb.DeleteBatchToLastBlockID(batch, batchID)
	}

	var (
		confirmedHead *big.Int
		persisted     []common.Hash
	)
	for _, block := range canonical {
		if block.number < first || block.number > last {
			return fmt.Errorf("canonical L1 origin block %d outside reconciliation range [%d,%d]", block.number, first, last)
		}
		if rawdb.ReadCanonicalHash(db, block.number) != block.hash {
			return fmt.Errorf("block %d hash %s is not canonical", block.number, block.hash)
		}
		pending, ok := origins.get(block.hash)
		if !ok {
			log.Warn("Missing pending L1 origin for canonical block", "number", block.number, "hash", block.hash)
			continue
		}
		if pending.l1Origin == nil || pending.l1Origin.BlockID == nil {
			return fmt.Errorf("pending L1 origin for block %s is incomplete", block.hash)
		}
		if pending.l1Origin.BlockID.Uint64() != block.number || pending.l1Origin.L2BlockHash != block.hash {
			return fmt.Errorf("pending L1 origin does not match canonical block %d %s", block.number, block.hash)
		}
		rawdb.WriteL1Origin(batch, pending.l1Origin.BlockID, pending.l1Origin)
		persisted = append(persisted, block.hash)
		if !pending.l1Origin.IsPreconfBlock() {
			confirmedHead = new(big.Int).Set(pending.l1Origin.BlockID)
			if pending.batchID != nil {
				rawdb.WriteBatchToLastBlockID(batch, pending.batchID, pending.l1Origin.BlockID)
			}
		}
	}

	if confirmedHead == nil && storedHead != nil && storedHead.Uint64() < first && canonicalL1Origin(db, storedHead) {
		confirmedHead = new(big.Int).Set(storedHead)
	}
	if confirmedHead == nil && first > 0 {
		for number, scanned := first-1, uint64(0); ; number, scanned = number-1, scanned+1 {
			blockID := new(big.Int).SetUint64(number)
			if canonicalL1Origin(db, blockID) {
				confirmedHead = blockID
				break
			}
			if number == 0 || scanned+1 >= headL1OriginReconcileLookback {
				break
			}
		}
	}
	if confirmedHead == nil {
		rawdb.DeleteHeadL1Origin(batch)
	} else {
		rawdb.WriteHeadL1Origin(batch, confirmedHead)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("commit L1 origin reconciliation: %w", err)
	}
	for _, hash := range persisted {
		origins.markPersisted(hash)
	}
	return nil
}

// canonicalL1Origin reports whether blockID has a confirmed origin matching the canonical hash.
func canonicalL1Origin(db ethdb.Database, blockID *big.Int) bool {
	origin, err := rawdb.ReadL1Origin(db, blockID)
	if err != nil || origin == nil || origin.IsPreconfBlock() {
		return false
	}
	return rawdb.ReadCanonicalHash(db, blockID.Uint64()) == origin.L2BlockHash
}

// canonicalL1OriginSegment returns the affected height range and the new canonical branch
// in ascending order. Both branches must already be available through lookup.
func canonicalL1OriginSegment(
	oldHead *types.Header,
	newHead *types.Header,
	lookup func(common.Hash) *types.Header,
) (uint64, uint64, []canonicalL1OriginBlock, error) {
	if oldHead == nil || newHead == nil {
		return 0, 0, nil, errors.New("nil canonical head")
	}
	if oldHead.Hash() == newHead.Hash() {
		number := newHead.Number.Uint64()
		return number, number, []canonicalL1OriginBlock{{number: number, hash: newHead.Hash()}}, nil
	}
	oldCursor := oldHead
	newCursor := newHead
	var reversed []*types.Header
	parent := func(header *types.Header) (*types.Header, error) {
		ancestor := lookup(header.ParentHash)
		if ancestor == nil {
			return nil, fmt.Errorf("missing ancestor %s for block %s", header.ParentHash, header.Hash())
		}
		return ancestor, nil
	}
	for oldCursor.Number.Cmp(newCursor.Number) > 0 {
		var err error
		oldCursor, err = parent(oldCursor)
		if err != nil {
			return 0, 0, nil, err
		}
	}
	for newCursor.Number.Cmp(oldCursor.Number) > 0 {
		reversed = append(reversed, newCursor)
		var err error
		newCursor, err = parent(newCursor)
		if err != nil {
			return 0, 0, nil, err
		}
	}
	for oldCursor.Hash() != newCursor.Hash() {
		reversed = append(reversed, newCursor)
		var err error
		oldCursor, err = parent(oldCursor)
		if err != nil {
			return 0, 0, nil, err
		}
		newCursor, err = parent(newCursor)
		if err != nil {
			return 0, 0, nil, err
		}
	}
	first := oldCursor.Number.Uint64() + 1
	last := max(oldHead.Number.Uint64(), newHead.Number.Uint64())
	var canonical []canonicalL1OriginBlock
	for i := len(reversed) - 1; i >= 0; i-- {
		canonical = append(canonical, canonicalL1OriginBlock{
			number: reversed[i].Number.Uint64(),
			hash:   reversed[i].Hash(),
		})
	}
	return first, last, canonical, nil
}

// reconcilePendingL1Origins derives the affected canonical segment and replaces its
// number-keyed origin views using retained hash-keyed metadata.
func (api *ConsensusAPI) reconcilePendingL1Origins(oldHead, newHead *types.Header) error {
	if oldHead.Hash() == newHead.Hash() {
		if !api.pendingL1Origins.isDirty(newHead.Hash()) {
			return nil
		}
	}
	first, last, canonical, err := canonicalL1OriginSegment(
		oldHead,
		newHead,
		api.eth.BlockChain().GetHeaderByHash,
	)
	if err != nil {
		return err
	}
	return reconcileL1OriginTables(api.eth.ChainDb(), &api.pendingL1Origins, first, last, canonical)
}
