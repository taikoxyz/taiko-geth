# p256verify zk-gas Multiplier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port [taikoxyz/taiko-mono#21748](https://github.com/taikoxyz/taiko-mono/pull/21748) into taiko-geth by adding the `p256verify` (RIP-7212, address `0x100`) precompile multiplier `163` to the Unzen default zk-gas schedule, leaving the frozen Masaya schedule unchanged.

**Architecture:** The consensus zk-gas precompile multipliers live in two Go maps in `core/vm/taiko_zk_gas_unzen.go`. We add one entry (`{18: 0x01}: 163`) to the default table `unzenPrecompileMultipliers()` only. The Masaya table stays frozen so already-finalized Masaya blocks (which committed their zk-gas total to header difficulty) keep re-executing identically. Tests in `core/vm/taiko_zk_gas_unzen_test.go` pin the table contents and entry counts, so they are updated first (TDD).

**Tech Stack:** Go 1.23+, go-ethereum test conventions (`go test`).

---

### Task 1: Add p256verify=163 to the default Unzen precompile table

**Files:**
- Modify: `core/vm/taiko_zk_gas_unzen.go` (function `unzenPrecompileMultipliers()` ~lines 433-457; doc comment on `masayaUnzenPrecompileMultipliers()` ~lines 246-252)
- Test: `core/vm/taiko_zk_gas_unzen_test.go` (functions `TestFullAddressLookupPreservesCanonicalPrecompileMultipliers` ~lines 176-215 and `TestUnzenSchedule_Multipliers` ~lines 77-111)

Background facts the implementer needs:
- Address `0x100` = `common.BytesToAddress([]byte{0x1, 0x00})`. As a `common.Address` struct literal this is `{18: 0x01}` (byte index 18 = `0x01`, byte index 19 = `0x00`). This differs from the canonical precompiles, which use `{19: 0xNN}`.
- `ZkGasSchedule.PrecompileMultiplier(addr)` returns `FailsafeMultiplier` (`math.MaxUint16`) for any address absent from the map.
- The default table currently has 17 entries; Masaya's frozen table also has 17 entries and must STAY at 17.

- [ ] **Step 1: Update the failing tests**

In `core/vm/taiko_zk_gas_unzen_test.go`, edit `TestFullAddressLookupPreservesCanonicalPrecompileMultipliers`.

Change the default `len()` assertion from 17 to 18 and add a p256verify lookup check. Replace this block:

```go
	if got := len(UnzenZkGasSchedule.PrecompileMultipliers); got != 17 {
		t.Errorf("default precompile table has %d entries, want 17", got)
	}
```

with:

```go
	// p256verify (RIP-7212) lives at the two-byte address 0x100, keyed as
	// {18: 0x01}; added to the default table in taiko-mono#21748.
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{18: 0x01}); got != 163 {
		t.Errorf("default p256verify (0x100) = %d, want 163", got)
	}
	if got := len(UnzenZkGasSchedule.PrecompileMultipliers); got != 18 {
		t.Errorf("default precompile table has %d entries, want 18", got)
	}
```

Then, in the same test, replace the Masaya `len()` assertion block:

```go
	if got := len(MasayaUnzenZkGasSchedule.PrecompileMultipliers); got != 17 {
		t.Errorf("Masaya precompile table has %d entries, want 17", got)
	}
```

with one that also pins the freeze decision (p256verify absent on Masaya → failsafe):

```go
	// Masaya stays frozen: p256verify was never in its finalized schedule, so it
	// must resolve to the failsafe, not 163. Adding it would break consensus on
	// already-finalized Masaya Unzen blocks.
	if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(common.Address{18: 0x01}); got != math.MaxUint16 {
		t.Errorf("Masaya p256verify (0x100) = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}
	if got := len(MasayaUnzenZkGasSchedule.PrecompileMultipliers); got != 17 {
		t.Errorf("Masaya precompile table has %d entries, want 17", got)
	}
```

Next, in `TestUnzenSchedule_Multipliers`, add a default-schedule assertion for p256verify. Insert it immediately after the existing identity (`0x04`) assertion block:

```go
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x04}); got != 6 {
		t.Fatalf("default identity (0x04) = %d, want 6", got)
	}
```

Add directly below it:

```go
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{18: 0x01}); got != 163 {
		t.Fatalf("default p256verify (0x100) = %d, want 163", got)
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./core/vm/ -run 'TestFullAddressLookupPreservesCanonicalPrecompileMultipliers|TestUnzenSchedule_Multipliers' -v`

Expected: FAIL. The new p256verify assertions report `65535, want 163` (it currently resolves to the failsafe because the entry is absent), and the default `len()` check reports `17 entries, want 18`.

- [ ] **Step 3: Add the entry to the default table**

In `core/vm/taiko_zk_gas_unzen.go`, in `unzenPrecompileMultipliers()`, add the p256verify line after the last entry (`bls12_map_fp2_to_g2`). Replace:

```go
		{19: 0x12}: 246, // bls12_map_fp_to_g1
		{19: 0x13}: 208, // bls12_map_fp2_to_g2
	}
}
```

with:

```go
		{19: 0x12}: 246, // bls12_map_fp_to_g1
		{19: 0x13}: 208, // bls12_map_fp2_to_g2
		// p256verify (RIP-7212) lives at the two-byte address 0x100, so its key
		// is {18: 0x01}, not the {19: 0xNN} form the canonical precompiles use.
		{18: 0x01}: 163, // p256verify
	}
}
```

- [ ] **Step 4: Update the two doc comments to reflect the 0x100 exception**

Still in `core/vm/taiko_zk_gas_unzen.go`, update the `unzenPrecompileMultipliers()` doc comment. Replace:

```go
// unzenPrecompileMultipliers returns the recalibrated default precompile
// multiplier table, keyed by full precompile address. Canonical precompiles all
// live at 0x00…00XX, so common.Address{19: 0xNN} spells their keys. Absent
// addresses resolve to FailsafeMultiplier.
```

with:

```go
// unzenPrecompileMultipliers returns the recalibrated default precompile
// multiplier table, keyed by full precompile address. Canonical precompiles
// live at 0x00…00XX, so common.Address{19: 0xNN} spells their keys; p256verify
// (RIP-7212) is the exception at the two-byte address 0x100, keyed as
// common.Address{18: 0x01}. Absent addresses resolve to FailsafeMultiplier.
```

Then update the `masayaUnzenPrecompileMultipliers()` doc comment to record the deliberate omission. Replace:

```go
// masayaUnzenPrecompileMultipliers returns the frozen Masaya precompile
// multiplier table, keyed by full precompile address and pinned at the
// pre-recalibration values. Frozen for the same consensus reason as
// masayaUnzenOpcodeMultipliers. Canonical precompiles all live at 0x00…00XX, so
// common.Address{19: 0xNN} spells their keys. Absent addresses resolve to
// FailsafeMultiplier.
```

with:

```go
// masayaUnzenPrecompileMultipliers returns the frozen Masaya precompile
// multiplier table, keyed by full precompile address and pinned at the
// pre-recalibration values. Frozen for the same consensus reason as
// masayaUnzenOpcodeMultipliers. Canonical precompiles all live at 0x00…00XX, so
// common.Address{19: 0xNN} spells their keys. Absent addresses resolve to
// FailsafeMultiplier. p256verify (RIP-7212, 0x100) is deliberately omitted here:
// it was never in Masaya's finalized schedule, so adding it would break
// consensus on already-finalized Masaya Unzen blocks.
```

- [ ] **Step 5: Run the updated tests to verify they pass**

Run: `go test ./core/vm/ -run 'TestFullAddressLookupPreservesCanonicalPrecompileMultipliers|TestUnzenSchedule_Multipliers' -v`

Expected: PASS for both tests.

- [ ] **Step 6: Run the full zk-gas/precompile suite and build**

Run: `go test ./core/vm/ -run 'Unzen|Precompile|ZkGas' && go build ./...`

Expected: `ok  github.com/ethereum/go-ethereum/core/vm` and a clean build with no output.

- [ ] **Step 7: Commit**

```bash
git add core/vm/taiko_zk_gas_unzen.go core/vm/taiko_zk_gas_unzen_test.go
git commit -m "feat(vm): add p256verify (RIP-7212) zk-gas multiplier to Unzen default

Ports taikoxyz/taiko-mono#21748: p256verify at address 0x100 gets
multiplier 163 in the default Unzen precompile table. Masaya stays frozen
(p256verify resolves to the failsafe there) to preserve consensus on
already-finalized Masaya Unzen blocks.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Spec "Change 1" (add `{18: 0x01}: 163` to default table) → Task 1, Step 3. ✓
- Spec "Change 1" doc-comment update → Task 1, Step 4 (first comment). ✓
- Spec "Change 2" (leave Masaya frozen + note) → Task 1, Step 4 (second comment); freeze verified by test in Step 1. ✓
- Spec "Change 3" tests (default len 17→18, p256verify=163 assertion, Masaya len 17 + failsafe assertion, `TestUnzenSchedule_Multipliers` addition) → Task 1, Step 1. ✓
- Spec "Edge cases" (collision safety — existing `TestHighRangePrecompileCollisionResolvesToFailsafe` unchanged): no task needed; called out as out-of-scope. ✓
- Spec "Verification" commands → Task 1, Step 6. ✓

**Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to" placeholders. Every code step shows the exact before/after text. ✓

**Type consistency:** Key encoding is `common.Address{18: 0x01}` everywhere (test and implementation); value `163` and `math.MaxUint16` failsafe used consistently; entry counts 18 (default) / 17 (Masaya) consistent across the test edits and the implementation. `common` and `math` are already imported in both files. ✓
