# getLastBlockByBatchId Optimization Design

Date: 2026-02-04
Status: Approved

## Context
`getLastBlockByBatchId` scans backward from the head and currently reads `rawdb.ReadL1Origin` for every scanned block to filter preconfirmation blocks. This is expensive and unnecessary because most blocks will not match the target batch ID.

## Goals
- Reduce `rawdb` reads during backward scans.
- Preserve existing semantics, including lookback limits and error behavior.
- Continue filtering preconfirmation blocks for matching batches.

## Non-goals
- Changing consensus rules or batch encoding.
- Reworking `BatchToLastBlockID` storage or head selection behavior.
- Removing preconfirmation filtering.

## Proposed Change
Reorder the scan to decode `proposalID` from `header.Extra` first, and only read `L1Origin` if the decoded `proposalID` matches the requested `batchID`. For non-matching blocks, skip `rawdb` access entirely. For matching blocks, read `L1Origin` once to check `IsPreconfBlock()`; if preconf, continue scanning, otherwise return the block. Keep the current uncertainty rule when the head block itself matches without a stored mapping.

## Data Flow
1. Determine `headNumber` from `ReadHeadL1Origin` if present; otherwise use current header number.
2. Walk backward from `headNumber`, enforcing the anchor selector and `maxBatchLookupBlocks` cap.
3. Decode `proposalID` from `header.Extra`.
4. If `proposalID != batchID`, move to the previous block with no `rawdb` access.
5. If `proposalID == batchID`, read `L1Origin` and skip if preconf; otherwise return the block unless it is the head, in which case return `ErrProposalLastBlockUncertain`.

## Error Handling
- Preserve existing errors: `ErrProposalLastBlockUncertain`, `ErrProposalLastBlockLookbackExceeded`, `ethereum.NotFound`.
- Propagate decode and `ReadL1Origin` errors as today.

## Testing
- Keep existing tests unchanged.
- Add a test where a matching block is marked preconf and ensure it is skipped in favor of the next matching block (or `NotFound` if none).
