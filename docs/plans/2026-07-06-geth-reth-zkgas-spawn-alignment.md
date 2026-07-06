# Align geth and reth zk-gas spawn-boundary behavior

Status: Implemented by PR #586.

This note tracks the next parity fix after the defensive gaiko2 manifest-filter
handling for `StateDB` witness errors plus `ErrZkGasLimitExceeded`. The
defensive handling is acceptable as a short-term guard, but the long-term fix
should make the geth-backed SGX lane stop at the same zk-gas boundary as the
reth-backed lane.

## Confirmed Gap

The observed divergence is in CALL/CREATE-family spawn accounting:

- alethia-reth/revm records a CALL/CREATE-family opcode step, waits until frame
  dispatch proves that child work was spawned, then charges the fixed spawn
  estimate before the child frame can keep progressing. Target account/code
  lookup may already have happened as part of dispatch; the important boundary
  is that child opcodes, CREATE initcode, and precompile-specific charging do
  not progress before the parent spawn charge is resolved.
- taiko-geth currently marks CALL/CREATE-family opcodes as spawned at dispatch
  entry, but the final `FinishAndCharge` still happens after `operation.execute`
  returns to the interpreter loop. That leaves a window where the child frame
  can perform witness-backed state reads before the parent spawn charge is
  rejected.

The problematic shape is:

1. A non-canonical tx in the manifest filter enters a nested CALL-family opcode.
2. The parent opcode's fixed spawn zk-gas estimate is already over the block
   limit.
3. reth/revm rejects at the spawn boundary and truncates the tx list.
4. geth enters the child frame first, may touch witness state that the reth
   execution never needed, and then reports zk-gas overflow after the child
   work returns.

This explains why the same tx-list witness can be complete for the canonical
reth boundary while the geth-backed filter sees a missing trie node before the
same logical zk-gas truncation point.

## Desired Invariant

For every CALL/CREATE-family opcode that actually dispatches child work, the EVM
must charge the fixed spawn estimate before the child frame can require any
additional witness read. If that charge exceeds the remaining block zk-gas, the
result must be `ErrZkGasLimitExceeded` with no extra child-frame witness
dependency.

This invariant should apply to:

- `CALL`
- `CALLCODE`
- `DELEGATECALL`
- `STATICCALL`
- `CREATE`
- `CREATE2`
- precompile dispatch, while preserving the separate precompile zk-gas charge

Ordinary non-spawn opcode metering should keep using the existing measured
per-step gas rule.

## Where To Fix

The primary fix belongs in taiko-geth, under `core/vm`, not in gaiko2:

- `core/vm/interpreter.go` currently calls `FinishAndCharge` after
  `operation.execute`.
- `core/vm/evm.go` marks pending CALL/CREATE spawns at dispatch entry.
- `core/vm/taiko_zk_gas_runtime.go` owns the per-depth pending step tracker and
  spawn-estimate charging.

gaiko2 should keep the defensive manifest-filter behavior until it consumes a
taiko-geth version that enforces this boundary itself. After the upstream fix
lands, update gaiko2's taiko-geth dependency to the reviewed commit, then bump
raiko2's gaiko2 image/vendor reference as needed.

## Implemented Fix

The fix uses the preferred approach:

1. Add a taiko-geth helper that charges the pending spawn step before entering
   real child work.
2. Call that helper from CALL/CREATE dispatch paths after dispatch has resolved
   the target/precompile and before child code execution, CREATE initcode, or
   precompile-specific charging can run.
3. Preserve the existing post-`operation.execute` `FinishAndCharge` path for
   non-spawn opcodes and for spawn opcodes that did not dispatch child work.
4. Clear the pending step after a successful early charge so the parent opcode is
   not charged twice.
5. Keep precompile behavior aligned with reth: charge the CALL-family spawn
   estimate for dispatch and charge precompile-specific zk-gas separately.

The fix intentionally does not move the spawn charge before target account/code
lookup, because alethia-reth/revm obtains the target bytecode while preparing the
call input. Empty-code calls still use the existing post-op path and retain the
short-circuit spawn semantics.

The alternative considered was:

- Implement a deferred-spawn queue in taiko-geth that mirrors reth's inspector
  model and flushes deferred spawn charges before the first child opcode. This
  is closer to the reference model but more invasive in geth's interpreter.

Avoid:

- Adding more gaiko2-only witness suppression rules as the permanent fix.
- Making witness availability decide zk-gas boundary semantics.

## Tests Added

The taiko-geth tests cover:

- A parent `CALL`, `CALLCODE`, `DELEGATECALL`, or `STATICCALL` whose fixed spawn
  estimate exceeds remaining zk-gas, where the child would `SLOAD` if entered.
  Expected result: `ErrZkGasLimitExceeded`, and no child opcode zk-gas charge.
- A parent `CREATE` or `CREATE2` whose initcode would `SLOAD` if entered.
  Expected result: `ErrZkGasLimitExceeded`, and no initcode opcode zk-gas
  charge.
- Precompile dispatch where the CALL-family spawn charge exceeds the remaining
  limit. Expected result: `ErrZkGasLimitExceeded`, and no precompile-specific
  zk-gas charge.
- Tracker idempotence proving the early charge clears the pending step and the
  post-op `FinishAndCharge` path does not double-charge.

Remaining integration regression:

- A fixture matching the manifest-filter shape seen in production should be run
  after consumers bump to this taiko-geth commit: the geth-backed filter and
  reth-backed execution should choose the same tx-list truncation prefix.

gaiko2/raiko2 regression:

- Keep the gaiko2 manifest-filter tests that prove canonical committed
  transactions cannot hide witness errors.
- Re-run the previously failing proposal after the taiko-geth bump.
- Confirm the defensive gaiko2 path is no longer exercised for that proposal,
  or keep it as a fallback with logs proving the upstream boundary now matches.

## Acceptance Criteria

- reth-backed SGX and geth-backed SGX truncate manifest tx lists at the same
  zk-gas boundary.
- A child-frame witness read cannot occur after the parent spawn charge is
  already known to exceed the zk-gas limit.
- Canonical committed transactions still fail proving on witness errors.
- The existing proposal regression passes without requiring a gaiko2-specific
  exception for missing trie nodes at the zk-gas boundary.

## Scope Notes

This fix is not a promise that every missing witness node will coincide with
`ErrZkGasLimitExceeded`. Canonical committed transactions must still fail on
missing witness data, and target account/code lookup can still be required
before the spawn charge, matching reth's dispatch path. The fixed class is the
case where the parent spawn charge is already over the block zk-gas limit but
geth would previously enter child execution first and discover an extra child
witness dependency before surfacing the zk-gas overflow.
