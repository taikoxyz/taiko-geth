# Limit backward batch lookup in getLastBlockByBatchId

## Context
`getLastBlockByBatchId` walks backward from the head L1 origin to find the last Shasta block for a given batch ID. Today it can traverse arbitrarily far, which is unnecessary for Shasta and can be expensive at the chain tip when the BatchToLastBlockID mapping is missing. We want a deterministic cap based on protocol constraints, and we will keep the behavior the same for all existing error cases.

## Requirements
- Cap the backward traversal to a fixed maximum lookback of `192 * 12` blocks.
- Keep all existing error semantics, including `ErrProposalLastBlockUncertain` when the head block matches without a mapping.
- When the cap is reached without a match, return `ethereum.NotFound` (same as a normal miss).
- Scope the change to `eth/taiko_api_backend.go` using a local constant and a `CHANGE(taiko)` comment.

## Approach
Introduce a local constant (e.g., `maxBatchLookupBlocks = 192 * 12`) near `getLastBlockByBatchId` with a `CHANGE(taiko)` note. Track the number of blocks inspected during the backward scan. Each iteration that inspects a block increments a counter; once the counter reaches the maximum, stop scanning and return `ethereum.NotFound`. The scanning logic for anchor checks, proposal ID decoding, and head-match uncertainty remains unchanged.

## Error handling
- Preserve existing decoding errors, `ErrProposalLastBlockUncertain`, and `ethereum.NotFound` behavior.
- The new cap returns `ethereum.NotFound` to avoid introducing a new public error type.

## Testing
Add a unit test that constructs a chain longer than the max lookback, sets a matching proposal ID on a block older than the allowed window, and asserts `getLastBlockByBatchId` returns `ethereum.NotFound` with a nil block ID. Ensure the head L1 origin is set and the recent blocks have a different proposal ID so the uncertainty-at-head case is not triggered.
