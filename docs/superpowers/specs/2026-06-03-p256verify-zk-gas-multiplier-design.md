# Add p256verify multiplier to the Unzen default zk-gas schedule

**Date:** 2026-06-03
**Upstream:** [taikoxyz/taiko-mono#21748](https://github.com/taikoxyz/taiko-mono/pull/21748)

## Problem

taiko-mono#21748 adds `p256verify` (RIP-7212, precompile at address `0x100`) to the
zk-gas precompile multiplier table in `zk_gas_spec.md` with value **163**. It was
missed in the original Unzen calibration sweep because RIP-7212 was not yet part of
the alethia-reth precompile set when the benchmark ran. The multiplier was derived
from an SP1 marginal-cycle measurement (~100k cycles/call, ~4× ecrecover) extrapolated
via ecrecover's measured µs/cycle ratio formula `max(SP1, risc0/6)`.

taiko-geth is the consensus implementation of this spec. The precompile multiplier
tables live in [`core/vm/taiko_zk_gas_unzen.go`](../../../core/vm/taiko_zk_gas_unzen.go),
so the spec change must be reflected there for the consensus zk-gas accounting to match.

## Background

- p256verify is a live precompile in this fork: it is registered in
  `PrecompiledContractsOsaka` at `common.BytesToAddress([]byte{0x1, 0x00})`
  ([`core/vm/contracts.go`](../../../core/vm/contracts.go)), and the Unzen fork runs the
  Osaka EVM. It is therefore callable on every Unzen network.
- Precompile multipliers are keyed by the precompile's full 20-byte address. A lookup
  miss resolves to `FailsafeMultiplier` (`math.MaxUint16`) via
  `ZkGasSchedule.PrecompileMultiplier`.
- Address `0x100` is the first two-byte precompile address. The canonical precompiles
  (`0x01`..`0x13`) use the `common.Address{19: 0xNN}` key form (single low byte). `0x100`
  must be keyed as `common.Address{18: 0x01}` (byte 18 = `0x01`, byte 19 = `0x00`).
- Two consensus schedules exist:
  - `UnzenZkGasSchedule` (default) — built from `unzenPrecompileMultipliers()`; used by
    Devnet/Internal/Hoodi/Mainnet. None of these have finalized Unzen blocks yet, so the
    table is still safe to change.
  - `MasayaUnzenZkGasSchedule` (frozen) — built from `masayaUnzenPrecompileMultipliers()`;
    used only by Masaya (chain id 167011). Masaya has already activated Unzen and encodes
    the finalized block zk-gas total in each block header's difficulty field.

## Decision: freeze Masaya, change the default table only

The multiplier is added **only** to the default `unzenPrecompileMultipliers()` table.
`masayaUnzenPrecompileMultipliers()` is left untouched, so p256verify continues to resolve
to `FailsafeMultiplier` on Masaya.

Rationale: any already-finalized Masaya block that called p256verify computed its zk-gas
cost using `FailsafeMultiplier`. Adding `163` to Masaya's table would make re-execution
diverge from the committed header difficulty → consensus break. This matches the existing
freeze rationale already documented for `MasayaTxIntrinsicZkGas` and the frozen
opcode/precompile multipliers in the same file.

## Changes

### 1. `unzenPrecompileMultipliers()` — add the entry

Add to the default table:

```go
{18: 0x01}: 163, // p256verify (RIP-7212, address 0x100)
```

Update the function doc comment, which currently asserts "Canonical precompiles all live
at 0x00…00XX, so `common.Address{19: 0xNN}` spells their keys." Note the `0x100` exception
and its `{18: 0x01}` encoding.

### 2. `masayaUnzenPrecompileMultipliers()` — leave frozen

No entry added. Add a one-line doc-comment note recording the deliberate omission and the
freeze rationale, so a future reader does not "fix" it by adding `163`.

### 3. Tests — [`core/vm/taiko_zk_gas_unzen_test.go`](../../../core/vm/taiko_zk_gas_unzen_test.go)

- `TestFullAddressLookupPreservesCanonicalPrecompileMultipliers`:
  - bump the default `len()` assertion from `17` → `18`;
  - assert `UnzenZkGasSchedule.PrecompileMultiplier(common.Address{18: 0x01}) == 163`;
  - keep Masaya `len() == 17`;
  - assert Masaya's `{18: 0x01}` resolves to `FailsafeMultiplier` (frozen/absent) — this
    pins the freeze decision as a regression guard.
- `TestUnzenSchedule_Multipliers`: add a default-schedule assertion for p256verify = 163.

## Edge cases

- **Collision safety:** `0x100`'s low byte is `0x00`, which is not a canonical precompile,
  so full-address keying carries no collision risk. The existing
  `TestHighRangePrecompileCollisionResolvesToFailsafe` already covers the general
  high-range case and needs no change.
- **No local spec doc:** `zk_gas_spec.md` lives only in taiko-mono; there is nothing to
  mirror in this repo.

## Out of scope

No change to opcode multiplier tables, block limits, the per-tx intrinsic charge, spawn
estimates, or the lookup logic in `core/vm/taiko_zk_gas.go`.

## Verification

```bash
go build ./...
go test ./core/vm/ -run 'Unzen|Precompile|ZkGas'
```
