# Sticky zk-gas-limit error propagation across CALL-family swallowing

**Status:** Design
**Date:** 2026-05-04
**Branch:** `fix/unzen-precompile-zk-gas-accounting`
**Related:** commit `d1b80cf65` (charge failed precompile zk gas)

## Problem

The `op*Call` family in `core/vm/instructions.go` swallows any error returned by `evm.Call*`, converting it into the standard EVM `ok=false` semantics:

```go
ret, returnGas, err := evm.Call(...)
if err != nil { temp.Clear() } else { temp.SetOne() }
stack.push(&temp)
...
return ret, nil    // err is dropped
```

This is correct for ordinary EVM errors (revert, out-of-gas, depth, etc.) but wrong for the Taiko-specific `vm.ErrZkGasLimitExceeded`, which is a consensus-level violation of the block zk-gas budget and must abort and truncate the tx. Two sites raise this error from inside an EVM call frame today, and both are silently swallowed:

1. **Precompile charging** — `evm.go:339, 416, 473, 539`. When `ZkGasMeter.ChargePrecompile` fails (the projected `blockZkGasUsed + cost` exceeds `schedule.BlockLimit`), the error returns from `evm.Call*` and gets dropped by the outer `op*Call`. The precompile's charge is also rejected (`ZkGasMeter.charge` only writes `txZkGasUsed = nextTx` after every check passes), so `txZkGasUsed` is unchanged. The tx commits as if the over-limit precompile never happened.
2. **Inner-frame `FinishAndCharge`** — `interpreter.go:266`. An inner-frame opcode whose zk-gas charge exceeds the block limit returns from inner `Run` → inner `evm.Call*` → outer `op*Call` (swallowed).

Commit `d1b80cf65` aligned the *amount* charged for failed precompiles with alethia-reth (full call gas on non-revert errors, matching revm's `gas.spend_all()` on `Halt`), but it did not address the swallow path. Because failed precompiles now charge a much larger amount, this branch makes the swallow path more reachable in practice.

In alethia-reth, `crates/evm/src/zk_gas/adapter.rs` solves this by writing `ContextError::Custom(ZK_GAS_LIMIT_ERR)` into the EVM context's error slot from five sites (`adapter.rs:95, 137, 178, 189, 224`). The slot is sticky — it survives the CALL-family `ok=false` swallowing because the error lives on `context.error()` rather than the call return value. The block executor at `block/src/executor.rs:373` matches the string and converts it into truncation.

## Goal

100% alignment with alethia-reth's behavior for both swallow sites. The geth executor (`core/state_processor.go`) already handles `vm.ErrZkGasLimitExceeded` correctly when it surfaces; the missing piece is making it surface from inside the EVM in the swallow cases.

## Design

### Architecture

Add a sticky error slot on `*vm.EVM` that mirrors alethia-reth's `context.error()` ↔ `ContextError::Custom(ZK_GAS_LIMIT_ERR)`. Sites that raise `ErrZkGasLimitExceeded` from inside an EVM call frame write the slot in addition to returning the Go error. The interpreter loop checks the slot at the top of each iteration, before dispatching the next opcode, and exits the frame with `ErrZkGasLimitExceeded`. The check happens *outside* `op*Call`, so the swallow sites can no longer drop the error.

Both Go return value and slot are present and not mutually exclusive: the return value lets the outermost `Run` exit cleanly without an extra loop iteration when no swallow happens; the slot rescues the case where `op*Call` swallows the return value.

The slot is cleared per-tx in `state_processor.go` so it cannot leak across transactions.

### Components

**`core/vm/evm.go`**
- Add field `zkGasErr error` to the `EVM` struct.
- Add unexported helper `setZkGasLimitErr()` that writes `ErrZkGasLimitExceeded` into the slot only if it is currently `nil`. Mirrors the `if err_slot.is_ok()` guard in alethia-reth's `set_custom_error` (`adapter.rs:401`).
- Add exported `ResetZkGasErr()` for the executor to clear the slot per-tx.
- In each precompile branch (`Call`, `CallCode`, `DelegateCall`, `StaticCall`), when `ChargePrecompile` returns an error, call `evm.setZkGasLimitErr()` before returning. Continue returning the Go error too — outer `op*Call` will still swallow it, but the slot is set.

**`core/vm/interpreter.go`**
- At the top of the `Run` loop (before `operation.execute`), check `evm.zkGasErr`. If set, return `(nil, ErrZkGasLimitExceeded)` immediately. This is the consumer that survives `op*Call` swallowing.
- At the existing `FinishAndCharge` site (`interpreter.go:265-268`), call `evm.setZkGasLimitErr()` in addition to the existing `return nil, ErrZkGasLimitExceeded`. This closes the inner-frame swallow path.

**`core/vm/taiko_zk_gas.go`**
- No state change to `ZkGasMeter`. The slot lives on `*EVM`, not on the meter, keeping the meter a pure accounting struct.

**`core/state_processor.go`**
- Alongside `ResetTransaction` at line 136, add `evm.ResetZkGasErr()` so a previously-truncated tx's slot doesn't poison subsequent txs in the same block-execution context.
- Existing branches at `:143-152` (truncation on `errors.Is(err, vm.ErrZkGasLimitExceeded)` for non-anchor txs) and `:267-271` (revert and propagate from `result.Err`) require no changes.

**`core/vm/taiko_zk_gas_runtime_test.go`**
- Three interpreter-level tests covering both swallow paths (failed precompile, successful precompile, inner-frame opcode).
- One executor-level integration test confirming the full truncation path through `ApplyTransactionWithEVM` and `Process`.

### Data flow

#### Failed precompile that exceeds block limit

1. Outer contract executes `STATICCALL` to `0x0a` with bad input.
2. `opStaticCall` → `evm.StaticCall` → precompile branch runs `RunPrecompiledContract`, which returns `(nil, 0, errPrecompileFailed)`.
3. `precompileZkGasUsed` returns full `gasBeforePrecompile`.
4. `ChargePrecompile` returns `ErrZkGasLimitExceeded` because `blockZkGasUsed + cost > BlockLimit`.
5. **NEW:** `evm.StaticCall` calls `evm.setZkGasLimitErr()`. Slot is set.
6. `evm.StaticCall` returns `(nil, 0, ErrZkGasLimitExceeded)`.
7. `opStaticCall` swallows the error (existing behavior preserved): pushes `0`, returns `(ret, nil)`.
8. Interpreter loop hits next iteration. **NEW:** top-of-loop check observes `evm.zkGasErr != nil`, returns `(nil, ErrZkGasLimitExceeded)`.
9. `Run` returns the error → outer `evm.Call*` returns it → `state_transition.go` puts it in `result.Err`.
10. `state_processor.go:267-271` matches `errors.Is(result.Err, vm.ErrZkGasLimitExceeded)`, reverts statedb snapshot and gas pool, returns `vm.ErrZkGasLimitExceeded`.
11. `state_processor.go:143-152` matches the same error, truncates the block at this tx (anchor-tx-protected), proceeds to body/difficulty validation.

#### Inner-frame opcode that exceeds block limit

1. Outer contract `CALL`s into inner contract.
2. Inner `Run` loop executes some opcode; `FinishAndCharge` returns `ErrZkGasLimitExceeded`.
3. **NEW:** Interpreter calls `evm.setZkGasLimitErr()` before returning.
4. Inner `Run` returns `(nil, ErrZkGasLimitExceeded)` → inner `evm.Call` returns it → outer `opCall` swallows.
5. **NEW:** Outer-frame `Run` loop's next iteration sees `evm.zkGasErr != nil`, returns the error.
6. Same executor path from step 9 above.

#### Tx boundary

`state_processor.go:136` calls `ResetTransaction` per tx. The new `evm.ResetZkGasErr()` is called at the same point so a previously-truncated tx's slot doesn't poison subsequent txs.

### Error handling and edge cases

- **Idempotent set.** `setZkGasLimitErr` only writes if the slot is currently `nil`. Mirrors alethia-reth's `if err_slot.is_ok()` guard at `adapter.rs:399-403`. Prevents a later, less-informative error from clobbering a real zk-gas violation.
- **No leak across nested EVMs.** Geth creates a fresh `*EVM` per block-execution context. The slot lives on that instance, so it cannot leak across blocks. Within a block, `ResetZkGasErr` clears it per-tx.
- **Anchor tx protection preserved.** The existing `i > 0` checks at `state_processor.go:143` and `:160` keep the anchor tx exempt from truncation. Our change only adds new error sources to the existing `ErrZkGasLimitExceeded` channel.
- **Read-only EVM (`eth_call`, gas estimation).** These paths construct `*EVM` with `ZkGasMeter == nil`, so no charging happens, no slot is ever set, and the top-of-loop check is a single nil-comparison fast path.
- **Tracer compatibility.** The slot is set after `RunPrecompiledContract` and after `FinishAndCharge`, i.e. after any tracer hook for that step. Tracer output is unchanged. The existing post-error gas zeroing at `evm.go:360-367` still fires because we keep returning the Go error from `evm.Call*`.
- **Concurrent execution.** `*EVM` is single-threaded by contract; no synchronization needed. Matches alethia-reth's single-mutator inspector model.

### Testing

All tests in `core/vm/taiko_zk_gas_runtime_test.go`. Tests 1–3 stay at interpreter level for speed and focus; Test 4 covers the executor-level path end-to-end.

**Test 1: `TestUnzenZkGas_FailedPrecompileExceedingBlockLimit_StickyError`**
- STATICCALL to `0x0a` (point_evaluation) with zeroed 192-byte input → precompile fails.
- `BlockLimit` set just below `gasBeforePrecompile * PrecompileMultipliers[0x0a]` so the full-gas charge overflows.
- Assert returned error is `ErrZkGasLimitExceeded` and `evm.zkGasErr` is set.
- Bytecode places a `STOP` immediately after the `STATICCALL`. Use a tracer (or an `OnOpcode` callback) to record opcode dispatches; assert the post-`STATICCALL` `STOP` is never observed — confirming the top-of-loop sticky check exited the frame before the next opcode dispatch.
- Assert `txZkGasUsed` does not include the rejected over-limit precompile charge (the `charge` helper rejects atomically, so `txZkGasUsed` reflects only successful prior charges).

**Test 2: `TestUnzenZkGas_SuccessfulPrecompileExceedingBlockLimit_StickyError`**
- Same shape as Test 1 but with valid `point_evaluation` input. Multiplier sized so the success-path `gasUsed * multiplier` overshoots `BlockLimit`.
- Confirms the sticky-error path is symmetric across success and failure.

**Test 3: `TestUnzenZkGas_InnerFrameOpcodeExceedingBlockLimit_StickyError`**
- Outer contract `CALL`s into inner contract whose first opcode (e.g. a `KECCAK256` over a large memory region) trips `FinishAndCharge`.
- Asserts the inner-frame error is not lost when outer `opCall` swallows it.

**Test 4: `TestUnzenZkGas_StickyError_TruncatesBlockAtFailingTx`**
- Drive `ApplyTransactionWithEVM` end-to-end with a tx whose precompile charge exceeds the limit.
- Assert `result.Err` is `ErrZkGasLimitExceeded`, statedb is reverted to the pre-tx snapshot, gas pool is reset.
- Assert that within `Process`, this tx is truncated and subsequent txs are skipped.

## Out of scope

- Changes to executor-level handling of `ErrZkGasLimitExceeded`. Geth's `state_processor.go` already mirrors alethia-reth's truncation/rejection path via `result.Err` matching.
- Changes to `ZkGasMeter` accounting semantics. The amount charged on failed precompiles is already correct after `d1b80cf65`.
- Refactoring `op*Call` to surface different error types. The CALL-family `ok=false` semantics for ordinary errors are preserved unchanged; only the zk-gas-limit case is rescued via the sticky slot.

## Alignment notes

This design maps directly onto alethia-reth's `set_custom_error` mechanism:

| alethia-reth | taiko-geth (this design) |
|---|---|
| `context.error()` slot | `evm.zkGasErr` field |
| `ContextError::Custom(ZK_GAS_LIMIT_ERR)` | `vm.ErrZkGasLimitExceeded` |
| `set_custom_error()` (`adapter.rs:399`) | `evm.setZkGasLimitErr()` |
| `if err_slot.is_ok()` guard | `if evm.zkGasErr == nil` guard |
| `step` callback flush check (`adapter.rs:91-98`) | top-of-`Run`-loop check |
| `call_end` precompile charge (`adapter.rs:181-191`) | `evm.Call*` precompile branch sets slot |
| `step_end` ordinary opcode charge (`adapter.rs:111-140`) | `interpreter.go` `FinishAndCharge` site sets slot |
| Block executor `ZK_GAS_LIMIT_ERR` match (`block/src/executor.rs:373`) | `state_processor.go:143-152` and `:267-271` (already in place) |
