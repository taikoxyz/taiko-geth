# Add `TX_INTRINSIC_ZK_GAS` to Unzen zk-gas accounting

**Date:** 2026-05-13
**Status:** Approved (ready for implementation plan)
**Spec source:** [taikoxyz/taiko-mono#21669](https://github.com/taikoxyz/taiko-mono/pull/21669)

## Summary

Add a fixed per-transaction intrinsic zk-gas charge (`243,000`) to the Unzen
zk-gas accounting model. The charge is applied once per block transaction —
including the anchor — before opcode and precompile metering begins, and
covers the proving cost of per-transaction sender recovery.

The charge sits in the in-flight transaction zk-gas total, so it is
committed to the finalized block total only when the transaction commits,
and discarded on revert or failure. If the intrinsic charge alone exceeds
the remaining block zk-gas budget, the transaction is rejected with
`ErrZkGasLimitExceeded`: non-anchor transactions trigger truncation at
that index; the anchor transaction is fatal.

Masaya is pinned at `0` because its Unzen activation predates this spec
change. Header `difficulty` on Unzen blocks equals the finalized block
zk-gas total, so retroactively raising Masaya's charge would break replay
of already-finalized Masaya blocks. Mainnet, Hoodi, Devnet, and Internal
networks all receive the full `243,000`.

## Background

Each Unzen block transaction has a fixed proving cost before opcode and
precompile metering begins, primarily from sender recovery. The original
zk-gas accounting model omitted this charge. The spec PR adds it, and this
design ports the implementation into taiko-geth.

The taiko-geth zk-gas implementation already mirrors the structure of the
Rust reference EVM: a `ZkGasSchedule` carrying per-fork constants, a
`ZkGasMeter` tracking in-flight tx and finalized block totals, opcode and
precompile multipliers consumed via `ChargeOpcode` / `ChargePrecompile`,
and a `commit`/`reset` lifecycle invoked by the block-execution loops. The
intrinsic charge slots into this structure without disturbing the existing
spawn-tracking, sticky-error, or snapshot/revert paths.

## Network gating

| Network                             | Chain ID | `TxIntrinsicZkGas` |
| ----------------------------------- | -------- | ------------------ |
| Taiko Mainnet                       | 167000   | 243,000            |
| Taiko Internal Devnet               | 167001   | 243,000            |
| Taiko Masaya Devnet                 | 167011   | **0** (pinned)     |
| Taiko Hoodi Testnet                 | 167013   | 243,000            |
| Any other chain id under Unzen      | —        | 243,000 (default)  |

The pinning for Masaya is consensus-critical: header `difficulty` already
encodes the finalized block zk-gas total for Unzen blocks
(`core/state_processor.go` validates this on import), so any change to the
charge applied to already-finalized Masaya blocks would invalidate them.

## Code changes

### 1. `core/vm/taiko_zk_gas.go` — schedule field and meter method

Add a `TxIntrinsicZkGas uint64` field to `ZkGasSchedule`:

```go
type ZkGasSchedule struct {
    BlockLimit            uint64
    TxIntrinsicZkGas      uint64 // CHANGE(taiko): fixed per-tx intrinsic charge (taiko-mono#21669)
    OpcodeMultipliers     [256]uint16
    PrecompileMultipliers [256]uint16
    SpawnEstimates        SpawnEstimates
}
```

Add a `ChargeTxIntrinsic` method on `ZkGasMeter` that reuses the existing
checked-arithmetic path by charging the schedule value with a multiplier
of `1`:

```go
// ChargeTxIntrinsic charges the fixed per-tx intrinsic zk gas defined by the
// active schedule into the in-flight tx total. A schedule value of 0 makes
// this a no-op. Returns ErrZkGasLimitExceeded if the charge alone would
// exceed the remaining block budget.
func (m *ZkGasMeter) ChargeTxIntrinsic() error {
    return m.charge(m.schedule.TxIntrinsicZkGas, 1)
}
```

Reusing `m.charge(base, multiplier)` keeps the overflow-check and
block-limit-check semantics identical to opcode/precompile charges. With
`multiplier = 1`, `safeMul` short-circuits and the path reduces to the
same checked-add used by opcode charges.

### 2. `core/vm/taiko_zk_gas_unzen.go` — constants and schedule construction

Add two exported constants alongside the existing block-limit constants:

```go
// CHANGE(taiko): TxIntrinsicZkGas is the fixed per-tx intrinsic zk-gas
// charge applied on Devnet, Internal, Hoodi, and Mainnet during Unzen.
// Sourced from taikoxyz/taiko-mono#21669; covers per-tx sender recovery
// proving cost.
const TxIntrinsicZkGas uint64 = 243_000

// CHANGE(taiko): MasayaTxIntrinsicZkGas is the per-tx intrinsic charge
// applied on the Taiko Masaya network during Unzen. Pinned at 0 because
// Masaya activated Unzen before the spec change landed; the header
// difficulty already encodes the finalized block zk gas, so changing the
// per-tx charge retroactively would break consensus on finalized blocks.
const MasayaTxIntrinsicZkGas uint64 = 0
```

Rename `unzenZkGasScheduleWithBlockLimit(blockLimit uint64) ZkGasSchedule`
to `unzenZkGasScheduleWith(blockLimit, txIntrinsicZkGas uint64)
ZkGasSchedule` and thread the new value into the schedule literal:

```go
var UnzenZkGasSchedule = unzenZkGasScheduleWith(BlockZkGasLimit, TxIntrinsicZkGas)
var MasayaUnzenZkGasSchedule = unzenZkGasScheduleWith(MasayaBlockZkGasLimit, MasayaTxIntrinsicZkGas)

func unzenZkGasScheduleWith(blockLimit, txIntrinsicZkGas uint64) ZkGasSchedule {
    s := ZkGasSchedule{
        BlockLimit:       blockLimit,
        TxIntrinsicZkGas: txIntrinsicZkGas,
        SpawnEstimates:   SpawnEstimates{ /* unchanged */ },
    }
    // …rest unchanged
}
```

The opcode-multiplier and precompile-multiplier tables remain shared
across both networks, as enforced by
`TestMasayaUnzenSchedule_SharesTablesWithDefault`.

### 3. `core/state_processor.go` — charge intrinsic in `ApplyTransactionWithEVM`

Insert the intrinsic charge inside the existing
`evm.Config.ZkGasMeter != nil` block in `ApplyTransactionWithEVM`,
**before** the snapshot. On failure, return the error immediately — no
state has been mutated, so no revert is needed:

```go
if evm.Config.ZkGasMeter != nil {
    // CHANGE(taiko): charge the per-tx intrinsic zk gas before EVM
    // execution begins (taikoxyz/taiko-mono#21669). The charge accumulates
    // into the in-flight tx total and is committed alongside opcode and
    // precompile usage on success; a schedule value of 0 (Masaya) makes
    // this a no-op. If the intrinsic alone exceeds the remaining block
    // budget, return ErrZkGasLimitExceeded so the outer loop truncates
    // (non-anchor) or fails (anchor).
    if err := evm.Config.ZkGasMeter.ChargeTxIntrinsic(); err != nil {
        return nil, err
    }
    zkSnap = statedb.Snapshot()
    zkGp = gp.Snapshot()
}
```

This single insertion covers both block-execution paths:

* **Import / replay** (`core.Process` → `ApplyTransactionWithEVM`).
* **Local sealing** (`miner.taiko_worker.sealBlockWith` →
  `commitTransaction` → `core.ApplyTransactionWithEVM`).

Both outer loops already handle `ErrZkGasLimitExceeded` correctly: for
`i == 0` (the anchor) it is fatal, and for `i > 0` the loop truncates the
remaining transactions. No additional changes are required in those loops.

Pre-execution and post-execution system calls
(`ProcessBeaconBlockRoot`, `ProcessParentBlockHash`,
`ProcessWithdrawalQueue`, `ProcessConsolidationQueue`) bypass
`ApplyTransactionWithEVM` entirely — they call `evm.Call` directly through
`processRequestsSystemCall`, which already resets the meter before and
after. They correctly do not get charged the intrinsic.

### 4. Tests

#### `core/vm/taiko_zk_gas_test.go`

* **`TestZkGasMeter_ChargeTxIntrinsic_AddsToInFlight`** — charging the
  intrinsic on a fresh meter sets `TxZkGasUsed()` to the schedule value
  and leaves `BlockZkGasUsed()` at zero. Committing the transaction
  promotes the intrinsic into the block total.
* **`TestZkGasMeter_ChargeTxIntrinsic_NoopWhenScheduleZero`** — with a
  schedule whose `TxIntrinsicZkGas` is `0`, charging the intrinsic leaves
  `TxZkGasUsed()` at zero and returns no error.
* **`TestZkGasMeter_ChargeTxIntrinsic_ReturnsLimitExceeded`** — prefill
  the block total to `BlockLimit - TxIntrinsicZkGas + 1` (using an opcode
  with multiplier 1, e.g. `CREATE 0xf0`), commit, then charge the
  intrinsic and assert `ErrZkGasLimitExceeded`. Mirrors the equivalent
  Rust meter test.

#### `core/vm/taiko_zk_gas_unzen_test.go`

* **`TestUnzenSchedule_TxIntrinsicZkGas`** — pin
  `UnzenZkGasSchedule.TxIntrinsicZkGas == 243_000`,
  `MasayaUnzenZkGasSchedule.TxIntrinsicZkGas == 0`, and the package-level
  constants `TxIntrinsicZkGas == 243_000`,
  `MasayaTxIntrinsicZkGas == 0`.

The existing `TestMasayaUnzenSchedule_SharesTablesWithDefault` continues
to verify that opcode tables, precompile tables, and spawn estimates do
not drift between the two schedules. The new `TxIntrinsicZkGas` field
is intentionally *not* shared, and the existing test does not check it.

#### `core/taiko_state_processor_unzen_test.go`

* **`TestApplyTransactionWithEVM_Unzen_IncludesTxIntrinsicInBlockZkGas`**
  — drive a simple value transfer through `ApplyTransactionWithEVM` with
  the default `UnzenZkGasSchedule`, commit the meter, and assert
  `meter.BlockZkGasUsed() >= TxIntrinsicZkGas`. Confirms the intrinsic
  reaches the finalized block total on the import path.
* **`TestApplyTransactionWithEVM_Masaya_DoesNotChargeTxIntrinsic`** —
  same shape with `MasayaUnzenZkGasSchedule`; assert the finalized block
  zk gas is strictly less than `TxIntrinsicZkGas` and strictly greater
  than zero (confirming the tx still recorded opcode-driven zk gas).

The existing
`TestApplyTransactionWithEVM_ZkGasExhausted_RevertsAndReturnsError` and
`TestApplyTransactionWithEVM_UnzenCommitThenTruncate` are unchanged by
this design but will be re-run as regression coverage; both use bespoke
test schedules whose `TxIntrinsicZkGas` defaults to `0`, so they remain
green without modification.

## Non-changes

* **No changes to `consensus/taiko/consensus.go` or `Finalize`**: header
  `difficulty` validation already compares against
  `meter.BlockZkGasUsed()`, into which the intrinsic flows naturally on
  commit.
* **No changes to the miner loop** beyond the shared
  `ApplyTransactionWithEVM` insertion: the existing reset / commit /
  truncate pattern in `miner/taiko_worker.go` keeps working as-is.
* **No changes to the spawn-opcode tracker, sticky-error path, or
  inspector wiring**: the intrinsic is charged before any opcode dispatch
  and never interacts with mid-execution state.
* **No changes to genesis files or `params/taiko_config.go`**: the spec
  change is part of Unzen, which is already gated through `IsUnzen` at
  the call sites that install the meter.

## Risks and rollout

* **Consensus parity**: Devnet, Internal, Hoodi, and Mainnet must all
  produce and accept blocks whose header `difficulty` reflects the
  intrinsic charge. The intrinsic flows through the existing commit path,
  so this is automatic — the integration tests in
  `core/taiko_state_processor_unzen_test.go` and the meter pin tests
  cover both the schedule-value and the flow-through behavior.
* **Masaya replay**: must continue to accept already-finalized Masaya
  blocks unchanged. The `MasayaTxIntrinsicZkGas = 0` pin and the
  `TestUnzenSchedule_TxIntrinsicZkGas` regression cover this.
* **Anchor handling**: the anchor is charged the intrinsic. If a future
  schedule increase pushed Masaya's intrinsic high enough that the anchor
  alone could bust the block budget, that would be a fatal block
  production error — but Masaya is pinned at `0` here, and other
  networks' `243,000` is well under the `100,000,000` block limit.
* **No new validator-visible RPC surface**: the change is internal to the
  EVM and block-execution loops.

## Out of scope

* Tuning the constant: this design ports `243,000` exactly as specified.
* Generalizing the intrinsic mechanism to non-Unzen forks: the intrinsic
  is only consulted when an Unzen `ZkGasMeter` is installed.
* Refactoring `m.charge(base, multiplier)` into a separate
  `chargeAmount(amount)` helper: kept as a single function for minimum
  diff; the Rust split is not load-bearing for the Go side.

## PR layout

Single PR onto the `taiko` branch, matching the conventions of recent
zk-gas PRs (#553–#558). PR description references the spec PR
(`taikoxyz/taiko-mono#21669`) and the Rust reference port; code comments
reference only the spec PR.
