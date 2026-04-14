# Uzen ZK Gas Parity With Canonical alethia-reth

## Summary

This spec defines the design for making `taiko-geth` compute Uzen zk gas exactly the same way as canonical `alethia-reth`.

The immediate trigger is block `2910` on the internal devnet, where:

- canonical `alethia-reth` reports `difficulty = 0x1bb8701`
- `taiko-geth` reports `difficulty = 0x206b70d`
- both nodes return the same transaction list and the same `gasUsed`

Because Uzen reuses `block.difficulty` as finalized block zk gas, this is a consensus-relevant mismatch.

## Goal

Make `taiko-geth` match `alethia-reth`'s zk gas calculation semantics exactly for all Uzen execution paths.

The acceptance criterion is not "fix block 2910". The acceptance criterion is:

- every zk gas calculation path in `taiko-geth` must behave the same way as canonical `alethia-reth`
- block `2910` must match as a consequence of that parity

## Non-Goals

- Minimize code churn at the expense of semantic accuracy
- Introduce heuristics that only fix the observed block
- Refactor unrelated EVM or consensus code
- Change the Uzen schedule constants unless parity work proves they differ from canonical `alethia-reth`

## Current Mismatch

### Observed chain data

For block `2910` (`0xb5e`):

- `taiko-geth` returns `difficulty = 0x206b70d`
- canonical `alethia-reth` returns `difficulty = 0x1bb8701`
- delta is `0x4b300c`
- transaction list length is the same on both nodes
- `gasUsed` is the same on both nodes
- block hash, receipts root, state root, and mix hash differ

This indicates the divergence is not transaction selection. It is caused by execution semantics and/or header assembly inputs derived from execution.

### Likely root cause

`taiko-geth` currently meters zk gas in a way that is not semantically identical to `alethia-reth`:

- In `core/vm/gas_table.go`, CALL-family dynamic gas returns `intrinsic + callGasTemp`
- In `core/vm/interpreter.go`, the zk gas raw input falls back to `cost`
- For CALL-family opcodes in geth, `cost` can include forwarded gas
- Canonical `alethia-reth` meters CALL/CREATE-family opcodes using net per-step gas delta unless the opcode actually spawned child work
- `taiko-geth` uses a single global `childSpawned` flag, while canonical `alethia-reth` tracks pending/deferred spawn state per frame depth

For implementation planning, this mismatch is treated as the primary defect to remove: `taiko-geth` must stop deriving zk gas from geth gross CALL-family opcode cost semantics and must instead reproduce canonical `alethia-reth` per-step and per-spawn behavior exactly.

## Canonical Semantics To Match

The source of truth is canonical `alethia-reth`, not the current geth implementation.

For Uzen zk gas:

1. Before an opcode executes, capture the frame depth, opcode byte, and gas remaining.
2. After the opcode executes, compute `stepGas = gasBefore - gasAfter`.
3. For non-spawn opcodes, charge zk gas using `ChargeOpcode(opcode, stepGas)`.
4. For `CALL`, `CALLCODE`, `DELEGATECALL`, `STATICCALL`, `CREATE`, and `CREATE2`, do not charge immediately.
5. Defer charging until the runtime can determine whether the opcode actually spawned child work.
6. If the opcode spawned child work, charge the fixed Uzen spawn estimate for that opcode.
7. If the opcode did not spawn child work, charge the measured `stepGas`.
8. If the opcode dispatched a precompile, charge precompile zk gas separately using actual precompile gas used.
9. Spawn tracking must be scoped to the relevant frame depth and pending opcode state.

These rules must hold even for nested frames and mixed call/precompile paths.

## Design

### 1. Replace global spawn bookkeeping with per-frame pending step state

`taiko-geth` should replace the current single `evm.childSpawned` boolean with a per-frame zk metering state model.

The geth implementation does not need to copy Rust structure names, but it must preserve the same semantics as `alethia-reth`:

- track the current pending opcode step by frame depth
- defer CALL/CREATE-family metering until spawn outcome is known
- allow spawn marking to apply to the correct pending parent opcode even when callbacks occur after child dispatch resolution

This removes ambiguity caused by nested frames overwriting one global spawn flag.

### 2. Meter CALL/CREATE-family opcodes from net step gas, not geth gross opcode cost

For non-spawning CALL/CREATE-family opcodes, the raw zk gas input must be net per-step gas delta:

- `stepGas = gasBefore - gasAfter`

It must not use the existing geth opcode `cost` fallback, because geth `cost` includes forwarded gas through `evm.callGasTemp`, which is not the canonical `alethia-reth` semantics.

### 3. Keep spawn estimates and precompile charging aligned with canonical behavior

If a CALL/CREATE-family opcode actually opens child work, use the fixed Uzen spawn estimate from the consensus schedule.

If the child work is a precompile:

- the opcode itself still uses spawn semantics
- precompile zk gas is charged separately from actual precompile gas consumed

This must match canonical `alethia-reth` behavior exactly.

### 4. Preserve existing finalized block zk gas lifecycle

The current transaction-level lifecycle in `core/state_processor.go` remains conceptually correct and should be preserved:

- reset in-flight tx zk gas before each transaction
- discard in-flight tx zk gas when a non-anchor tx exceeds the zk gas limit
- commit tx zk gas after successful execution
- verify imported Uzen header difficulty against recomputed finalized block zk gas

The parity patch should change how per-op charges are accumulated, not how per-tx and per-block finalized totals are committed and validated.

## Implementation Areas

### `core/vm/interpreter.go`

Introduce the per-step capture and finalize flow for Uzen zk gas metering:

- snapshot pre-step gas and opcode
- compute post-step net gas delta
- immediately charge ordinary opcodes
- defer CALL/CREATE-family charging until spawn outcome is resolved

### `core/vm/evm.go`

Expose the exact spawn outcome signals needed by the metering layer:

- actual child frame opened
- precompile dispatch occurred
- no child work occurred

The implementation must be depth-safe and work for nested calls and contract creation.

### `core/vm/taiko_zk_gas.go`

Extend the zk gas support types only as needed to support:

- per-frame pending step state
- deferred spawn-op charging
- parity-safe charging helpers

The meter arithmetic itself should remain checked and consensus-owned.

### Tests

Add focused regression coverage near the zk gas implementation:

- non-spawn CALL-family opcode uses net step gas
- spawn CALL-family opcode uses fixed spawn estimate
- precompile path uses spawn estimate plus separate precompile charge
- nested frames do not corrupt pending state
- recomputed finalized block zk gas matches imported header difficulty for parity fixtures

Add a regression fixture derived from the block `2910` execution shape when the fixture can be expressed in a stable test form without introducing external runtime dependencies.

## Error Handling

The parity patch must preserve current Uzen block handling rules:

- zk gas overflow or limit exceed returns the dedicated zk gas limit error
- non-anchor tx zk gas overflow stops further tx execution for the block
- anchor tx is not silently discarded
- imported Uzen blocks still fail when the body extends past the committed receipt count
- imported Uzen blocks still fail when header difficulty does not equal recomputed finalized zk gas

## Validation Plan

Validation is successful only when all of the following are true:

- `taiko-geth` reproduces canonical block `2910` difficulty exactly
- CALL/CREATE/precompile metering tests match canonical `alethia-reth` semantics
- no parity regression is observed for nested call structures in added regression tests

Manual parity validation should compare the same block on both nodes after the patch:

- `difficulty`
- block hash
- receipts root
- state root

Matching `difficulty` alone is not sufficient if other execution-derived header fields still differ.

## Risks

### Hidden semantic drift in nested call handling

The biggest risk is reproducing the surface behavior for block `2910` while still diverging on other nested frame shapes. This is why the design prefers exact semantic parity over a smaller local fix.

### Overfitting to geth internals

A patch that still depends on geth `cost` semantics or a global spawn flag could appear to work on one block while remaining non-canonical. The design explicitly rejects that.

### Incomplete precompile parity

Precompile handling is easy to under-specify. The patch must preserve the canonical split between CALL-family spawn handling and separate precompile gas charging.

## Decision

Implement exact semantic parity with canonical `alethia-reth` for all Uzen zk gas metering paths, even if that requires restructuring the current geth-side metering hooks.
