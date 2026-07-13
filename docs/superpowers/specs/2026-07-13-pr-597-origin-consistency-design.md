# PR 597 L1 Origin Consistency Design

## Goal

Persist Taiko L1-origin metadata only for canonical blocks while preserving the metadata needed for pre-import consensus checks and keeping `L1Origin`, `HeadL1Origin`, and `BatchToLastBlockID` consistent across ordinary promotion, rewind, and sibling reorg paths.

## Constraints

- Build-only previews must not write any L1-origin table rows.
- `newPayload` must enforce the existing future-timestamp rule for locally built non-preconfirmation blocks before importing them.
- A rawdb origin may classify a header only when its `L2BlockHash` matches that header.
- Explicit `taikoAuth` setter RPC behavior remains unchanged.
- `HeadL1Origin` and batch mappings remain gated to non-preconfirmation blocks.
- Reconciliation must run under `forkchoiceLock`, after canonicalization and before a payload requested by the same FCU is stashed.
- Missing metadata must degrade to absent rows and a cleared or clamped confirmed-head pointer, never a row describing a noncanonical block.

## Root Causes

The first PR version removes build-time rawdb writes, but Taiko header verification still reads `L1Origin` by block number during `newPayload`. The candidate's origin is therefore absent, causing non-preconfirmation future blocks to skip `ErrFutureBlock`. A same-height candidate can instead inherit the previous sibling's classification because the table is keyed only by number.

The pending buffer also removes an entry after its first promotion. Taiko permits rewinds and sibling reorgs, so a later return to that block cannot restore its number-keyed origin. Rows and batch mappings from the displaced branch can remain visible even though a different block is canonical.

## Chosen Architecture

### Hash-keyed metadata cache

The bounded origin buffer remains process-local and keyed by sealed block hash, matching the payload cache's lifetime. It gains a non-destructive lookup and retains entries after promotion instead of consuming them. Re-stashing the same hash replaces its metadata. Eviction stays bounded and observable.

The cache serves two roles:

1. Before import, it supplies the exact block's origin for timestamp validation without writing rawdb.
2. After a reorg, it supplies metadata for blocks on the newly canonical segment so the number-keyed tables can be rebuilt.

If metadata was evicted or the process restarted, reconciliation leaves that canonical height without an origin row and logs the omission. This is safer than returning metadata for the wrong block and lets existing driver recovery paths re-derive the missing information.

### Pre-import timestamp validation

`newPayload` looks up pending metadata by `params.BlockHash` before `InsertBlockWithoutSetHead`. When the origin is non-preconfirmation and the payload timestamp is later than the current Unix time, it returns the same invalid-payload result produced for `consensus.ErrFutureBlock`.

The Taiko consensus verifier continues supporting already-persisted origins, but applies the future-timestamp rule only when `l1Origin.L2BlockHash == header.Hash()`. This prevents an existing sibling row from classifying a different candidate at the same height.

### Canonical table reconciliation

Before calling `SetCanonical`, FCU captures the previous canonical head. After successful canonicalization it finds the common ancestor between the previous and requested heads and reconciles the affected height interval in one rawdb batch:

1. Delete number-keyed `L1Origin` rows for every affected height.
2. Delete every batch mapping whose target block ID falls in the affected interval. This removes mappings for displaced siblings even though the mapping table does not store block hashes.
3. Walk the new canonical segment from the common ancestor toward the new head. For each block with cached hash-keyed metadata, write its number-keyed origin and, when non-preconfirmation, its batch mapping.
4. Recompute `HeadL1Origin` as the highest non-preconfirmation origin whose stored hash matches the canonical hash at that height. Prefer the new segment, then retain a still-valid pointer below the affected interval. If no valid row is available within the bounded recovery window, clear the pointer.
5. Commit the custom-table batch atomically.

An idempotent FCU where the requested head is already current still reconciles that head when cached metadata exists. An FCU that promotes N and builds N+1 completes reconciliation for N before stashing N+1.

## Raw Database Operations

`core/rawdb/taiko_l1_origin.go` will add helpers that separate iteration from mutation so all mutations can be applied through an `ethdb.Batch`:

- delete one number-keyed L1 origin;
- delete or clear the head-origin pointer;
- iterate and delete batch mappings targeting an inclusive block-number interval.

Existing write helpers and public read behavior remain compatible. The reconciliation code owns batch creation and commit so the three custom-table views cannot be partially updated relative to one another.

## Error Handling

- Canonicalization errors return before any custom-table changes.
- Batch construction or commit failure aborts the FCU with an internal error; cached metadata remains available for an idempotent retry.
- Missing cached metadata is logged with block number and hash, and no incorrect substitute row is written.
- Rawdb decode errors abort reconciliation rather than silently deleting unknown data.
- Pointer repair clears an unverifiable value instead of fabricating a confirmed head.

## Testing

Tests will be added before production changes and observed failing for the intended reasons.

### Consensus and import validation

- A pending non-preconfirmation payload with a future timestamp is rejected before import.
- A pending preconfirmation payload with the same timestamp is not rejected by this rule.
- A rawdb origin for a different hash at the same height does not classify the candidate.
- A matching rawdb non-preconfirmation origin continues rejecting a future header.

### Buffer behavior

- Non-destructive lookup returns the exact hash entry without removing it.
- A promoted entry remains available for a later reorg-back.
- Replacement and bounded eviction retain their documented behavior.

### Reconciliation behavior

- A build-only preview leaves all three tables unchanged.
- First promotion writes the expected origin, confirmed-head pointer, and batch mapping.
- Preconfirmation promotion writes only the origin.
- A sibling reorg removes the displaced batch mapping and installs the new sibling's metadata.
- Reorging back restores the first sibling's metadata.
- Rewinding to an ancestor removes rows and mappings above the new head and clamps or clears `HeadL1Origin`.
- A multi-block branch switch rebuilds metadata in canonical order.
- Missing cached metadata leaves rows absent instead of retaining displaced data.
- A single FCU that promotes N and builds N+1 reconciles N before N+1 is cached.

## Alternatives Rejected

Writing `L1Origin` before import would preserve timestamp validation but recreate the build-only ghost rows this PR exists to eliminate.

Retaining only the last promoted entry would fix a single sibling switch but not rewinds or multi-block reorgs and would leave stale batch mappings.

Changing the persistent schema to make block hash the primary key would provide durable fork history, but it requires migration and API-index changes disproportionate to this PR. The bounded cache plus conservative clearing preserves the current schema and fails safely after cache loss.
