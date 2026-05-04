# Sticky zk-gas-limit error propagation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a sticky zk-gas-limit error slot on `*vm.EVM` so `vm.ErrZkGasLimitExceeded` cannot be swallowed by `op*Call`'s `ok=false` semantics. Aligns taiko-geth with alethia-reth's `set_custom_error` ↔ `context.error()` mechanism for both the precompile-charge and inner-frame `FinishAndCharge` paths.

**Architecture:** A single `error` field on the `EVM` struct, written by every site that today returns `ErrZkGasLimitExceeded` from inside an EVM call frame, checked at the top of the interpreter loop. The check sits *outside* the four `op*Call` swallow sites, so the swallow can no longer drop the error. Cleared per-tx by the block executor.

**Tech Stack:** Go 1.23, taiko-geth (fork of go-ethereum v1.17.2), package `core/vm`, `core/`.

**Spec:** `docs/superpowers/specs/2026-05-04-sticky-zk-gas-limit-error-design.md`

---

## File Structure

**Modified:**
- `core/vm/evm.go` — add `zkGasErr error` field on the `EVM` struct; add slot setter at the four precompile-charge sites (lines 339, 416, 473, 539).
- `core/vm/interpreter.go` — add slot check at top of `Run` loop; set slot at existing `FinishAndCharge` exit site (line 265-268).
- `core/state_processor.go` — clear slot per-tx alongside existing `ResetTransaction` call (line 134-137).

**Created:**
- `core/vm/taiko_zk_gas_sticky.go` — `setZkGasLimitErr()` and `ResetZkGasErr()` helpers, project convention is `taiko_*.go` for new Taiko-specific files.
- `core/vm/taiko_zk_gas_sticky_test.go` — unit tests for the helpers.
- `core/taiko_zk_gas_sticky_executor_test.go` — executor-level integration test for Test 4.

**Modified tests:**
- `core/vm/taiko_zk_gas_runtime_test.go` — add Tests 1–3 (interpreter-level over-limit cases).

---

## Task 1: Add `zkGasErr` slot and helpers on `*EVM`

**Files:**
- Modify: `core/vm/evm.go` (add field on `EVM` struct, around line 131)
- Create: `core/vm/taiko_zk_gas_sticky.go`
- Create: `core/vm/taiko_zk_gas_sticky_test.go`

- [ ] **Step 1: Write the failing helper unit tests**

Create `core/vm/taiko_zk_gas_sticky_test.go`:

```go
package vm

import "testing"

func TestEVMSetZkGasLimitErr_IdempotentWhenSlotEmpty(t *testing.T) {
	evm := &EVM{}
	evm.setZkGasLimitErr()
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
}

func TestEVMSetZkGasLimitErr_DoesNotClobberExistingError(t *testing.T) {
	evm := &EVM{zkGasErr: ErrZkGasLimitExceeded}
	evm.setZkGasLimitErr()
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded preserved", evm.zkGasErr)
	}
}

func TestEVMResetZkGasErr_ClearsSlot(t *testing.T) {
	evm := &EVM{zkGasErr: ErrZkGasLimitExceeded}
	evm.ResetZkGasErr()
	if evm.zkGasErr != nil {
		t.Fatalf("zkGasErr = %v, want nil after reset", evm.zkGasErr)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/vm/ -run TestEVMSetZkGasLimitErr -v`
Expected: FAIL — `evm.zkGasErr undefined`, `evm.setZkGasLimitErr undefined`, `evm.ResetZkGasErr undefined`.

- [ ] **Step 3: Add the field on the `EVM` struct**

In `core/vm/evm.go`, modify the `EVM` struct (around line 131). Find:

```go
	zkGasTracker *ZkGasStepTracker // CHANGE(taiko): exact per-depth Unzen zk gas tracking
}
```

Replace with:

```go
	zkGasTracker *ZkGasStepTracker // CHANGE(taiko): exact per-depth Unzen zk gas tracking

	// CHANGE(taiko): zkGasErr is the sticky zk-gas-limit error slot. Mirrors
	// alethia-reth's ContextError::Custom(ZK_GAS_LIMIT_ERR) on context.error().
	// Set by precompile-charge and FinishAndCharge sites that would otherwise be
	// swallowed by op*Call's ok=false semantics; consumed at the top of the
	// interpreter Run loop. Cleared per-tx by the block executor.
	zkGasErr error
}
```

- [ ] **Step 4: Create the helpers file**

Create `core/vm/taiko_zk_gas_sticky.go`:

```go
package vm

// CHANGE(taiko): setZkGasLimitErr stores the sticky zk-gas-limit error on the
// EVM so it cannot be swallowed by op*Call returning ok=false. Mirrors
// alethia-reth's set_custom_error guard at adapter.rs:399-403, which only
// writes when the slot is currently Ok.
func (evm *EVM) setZkGasLimitErr() {
	if evm.zkGasErr == nil {
		evm.zkGasErr = ErrZkGasLimitExceeded
	}
}

// CHANGE(taiko): ResetZkGasErr clears the sticky zk-gas-limit slot. Called by
// the block executor between transactions so a previously-truncated tx's slot
// does not poison subsequent txs in the same block-execution context.
func (evm *EVM) ResetZkGasErr() {
	evm.zkGasErr = nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./core/vm/ -run TestEVMSetZkGasLimitErr -v && go test ./core/vm/ -run TestEVMResetZkGasErr -v`
Expected: PASS for all three tests.

- [ ] **Step 6: Run full vm package tests to ensure no regressions**

Run: `go test ./core/vm/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add core/vm/evm.go core/vm/taiko_zk_gas_sticky.go core/vm/taiko_zk_gas_sticky_test.go
git commit -m "$(cat <<'EOF'
feat(core): add sticky zk-gas-limit error slot on EVM

Adds zkGasErr field plus setZkGasLimitErr/ResetZkGasErr helpers that
mirror alethia-reth's set_custom_error/context.error() pattern. Not yet
wired into precompile-charge or interpreter sites; that's the next step.
EOF
)"
```

---

## Task 2: Wire slot setter into precompile-charge sites

**Files:**
- Modify: `core/vm/evm.go` (four precompile branches at lines ~339, ~416, ~473, ~539)
- Modify: `core/vm/taiko_zk_gas_sticky_test.go` (add focused test)

- [ ] **Step 1: Write the failing test**

Append to `core/vm/taiko_zk_gas_sticky_test.go`:

```go
import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// stickySchedule returns a tiny zk-gas schedule sized so a single failed
// precompile call will exceed BlockLimit, exercising the over-limit path.
func stickySchedule() *ZkGasSchedule {
	s := &ZkGasSchedule{
		BlockLimit: 1_000,
		SpawnEstimates: SpawnEstimates{
			Call: 1, CallCode: 1, DelegateCall: 1, StaticCall: 1,
			Create: 1, Create2: 1,
		},
	}
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = 0
	}
	for i := range s.PrecompileMultipliers {
		s.PrecompileMultipliers[i] = 1
	}
	// Force point_evaluation (0x0a) precompile multiplier large enough that a
	// 100k-gas call definitely overshoots BlockLimit=1000.
	s.PrecompileMultipliers[0x0a] = 1
	return s
}

func TestEVMCall_PrecompileOverLimit_SetsStickyError(t *testing.T) {
	// STATICCALL to point_evaluation (0x0a) with bad input → precompile fails →
	// full call gas (100k) is charged, which overflows BlockLimit=1000.
	code := common.Hex2Bytes("6000600060c06000600a620186a0fa00")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(stickySchedule())
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, _ = evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))

	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded set after over-limit precompile", evm.zkGasErr)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/vm/ -run TestEVMCall_PrecompileOverLimit_SetsStickyError -v`
Expected: FAIL — `zkGasErr = <nil>, want ErrZkGasLimitExceeded`. The current code returns the error from `evm.Call` but never sets the slot, and `opStaticCall` swallows the return.

- [ ] **Step 3: Wire the setter into the four precompile-charge branches**

In `core/vm/evm.go`, find the `Call` precompile branch (around line 338-342):

```go
		if evm.Config.ZkGasMeter != nil {
			if zkErr := evm.Config.ZkGasMeter.ChargePrecompile(addr[19], precompileZkGasUsed(gasBeforePrecompile, gas, err)); zkErr != nil {
				return nil, 0, zkErr
			}
		}
```

Replace with:

```go
		if evm.Config.ZkGasMeter != nil {
			if zkErr := evm.Config.ZkGasMeter.ChargePrecompile(addr[19], precompileZkGasUsed(gasBeforePrecompile, gas, err)); zkErr != nil {
				evm.setZkGasLimitErr()
				return nil, 0, zkErr
			}
		}
```

Apply the **identical** transformation to the same block in `CallCode` (around line 415-419), `DelegateCall` (around line 472-476), and `StaticCall` (around line 538-542). Use `replace_all` is not safe here because the four blocks are textually identical; do them one at a time and verify each with `git diff`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/vm/ -run TestEVMCall_PrecompileOverLimit_SetsStickyError -v`
Expected: PASS.

- [ ] **Step 5: Run full vm package tests**

Run: `go test ./core/vm/...`
Expected: PASS. The existing `TestUnzenZkGasParity_*` tests should still pass because they use a non-over-limit schedule.

- [ ] **Step 6: Commit**

```bash
git add core/vm/evm.go core/vm/taiko_zk_gas_sticky_test.go
git commit -m "$(cat <<'EOF'
feat(core): set sticky zk-gas-limit slot on precompile over-limit

Wires the sticky slot into the four ChargePrecompile call sites in
EVM.Call/CallCode/DelegateCall/StaticCall. The slot is not yet consumed —
the next commit adds the top-of-loop check that surfaces it past the
op*Call swallow.
EOF
)"
```

---

## Task 3: Set slot at `FinishAndCharge` exit in interpreter loop

**Files:**
- Modify: `core/vm/interpreter.go` (line 265-268)
- Modify: `core/vm/taiko_zk_gas_sticky_test.go` (add focused test)

- [ ] **Step 1: Write the failing test**

Append to `core/vm/taiko_zk_gas_sticky_test.go`:

```go
func TestRun_FinishAndChargeOverLimit_SetsStickyError(t *testing.T) {
	// ADD opcode (0x01) with multiplier=math.MaxUint16 → first ADD at depth 0
	// will trip BlockLimit=1000 immediately. Use schedule with non-trivial ADD
	// multiplier to force overflow on the first arithmetic step.
	schedule := stickySchedule()
	schedule.OpcodeMultipliers[0x01] = 1024 // ADD: cost = ADD_gas(3) * 1024 = 3072 > 1000

	// PUSH1 1 PUSH1 2 ADD STOP — overflows on the ADD step.
	code := common.Hex2Bytes("60016002010100")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(schedule)
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded set after FinishAndCharge over-limit", evm.zkGasErr)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/vm/ -run TestRun_FinishAndChargeOverLimit_SetsStickyError -v`
Expected: FAIL — `zkGasErr = <nil>`. The interpreter currently returns `ErrZkGasLimitExceeded` from `Run` but never sets the slot.

- [ ] **Step 3: Set the slot at the FinishAndCharge exit**

In `core/vm/interpreter.go`, find (around lines 261-268):

```go
		// CHANGE(taiko): charge zk gas after opcode execution using exact
		// alethia-reth semantics: net per-step gas unless the opcode actually
		// spawned child work, in which case the fixed spawn estimate is used.
		if evm.zkGasTracker != nil {
			if zkErr := evm.zkGasTracker.FinishAndCharge(evm.depth, contract.Gas); zkErr != nil {
				return nil, ErrZkGasLimitExceeded
			}
		}
```

Replace with:

```go
		// CHANGE(taiko): charge zk gas after opcode execution using exact
		// alethia-reth semantics: net per-step gas unless the opcode actually
		// spawned child work, in which case the fixed spawn estimate is used.
		if evm.zkGasTracker != nil {
			if zkErr := evm.zkGasTracker.FinishAndCharge(evm.depth, contract.Gas); zkErr != nil {
				evm.setZkGasLimitErr()
				return nil, ErrZkGasLimitExceeded
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./core/vm/ -run TestRun_FinishAndChargeOverLimit_SetsStickyError -v`
Expected: PASS.

- [ ] **Step 5: Run full vm package tests**

Run: `go test ./core/vm/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add core/vm/interpreter.go core/vm/taiko_zk_gas_sticky_test.go
git commit -m "$(cat <<'EOF'
feat(core): set sticky zk-gas-limit slot on FinishAndCharge over-limit

Adds the slot write at the existing interpreter exit. The slot is still
not consumed yet; consumption (top-of-loop check) is the next commit.
EOF
)"
```

---

## Task 4: Add top-of-loop sticky-error check (consumer)

**Files:**
- Modify: `core/vm/interpreter.go` (top of `for { ... }` loop, around line 168)
- Modify: `core/vm/taiko_zk_gas_runtime_test.go` (add Test 1 from spec)

- [ ] **Step 1: Write the failing Test 1 from the spec**

Append to `core/vm/taiko_zk_gas_runtime_test.go`:

```go
func TestUnzenZkGas_FailedPrecompileExceedingBlockLimit_StickyError(t *testing.T) {
	// PUSH1 0 PUSH1 0 PUSH1 0xc0 PUSH1 0 PUSH1 0x0a PUSH3 0x0186a0 STATICCALL
	// STOP STOP — second STOP marks "did the loop continue past the failing
	// STATICCALL?". With the sticky check in place, only the first STOP could
	// run, but actually neither STOP runs because the loop exits at top-of-loop.
	code := common.Hex2Bytes("6000600060c06000600a620186a0fa0000")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(stickySchedule())
	var observedOps []byte
	tracer := &tracing.Hooks{
		OnOpcode: func(_ uint64, op byte, _ uint64, _ uint64, _ tracing.OpContext, _ []byte, _ int, _ error) {
			observedOps = append(observedOps, op)
		},
	}
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter, Tracer: tracer})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
	// The STATICCALL itself ran (it's the opcode that triggered the over-limit
	// precompile charge), but the post-STATICCALL STOP must NOT have run — the
	// top-of-loop sticky check exits the frame before the next dispatch.
	for _, op := range observedOps {
		if op == 0x00 { // STOP
			t.Fatalf("STOP after over-limit STATICCALL was dispatched; sticky check failed to short-circuit. ops=%v", observedOps)
		}
	}
}
```

Note: the `stickySchedule` helper is already defined in `taiko_zk_gas_sticky_test.go` (Task 2). Both files are in package `vm`, so `stickySchedule` is shared.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/vm/ -run TestUnzenZkGas_FailedPrecompileExceedingBlockLimit_StickyError -v`
Expected: FAIL — `STOP after over-limit STATICCALL was dispatched`. Even though the slot is now set (Tasks 2 and 3), nothing consumes it; `opStaticCall` returns nil, the loop continues, the post-STATICCALL STOP runs.

- [ ] **Step 3: Add the top-of-loop sticky check**

In `core/vm/interpreter.go`, find the start of the main loop (around line 167-170):

```go
	_ = jumpTable[0] // nil-check the jumpTable out of the loop
	for {
		gasBefore := contract.Gas
		if debug {
```

Replace with:

```go
	_ = jumpTable[0] // nil-check the jumpTable out of the loop
	for {
		// CHANGE(taiko): consume the sticky zk-gas-limit slot before dispatching
		// the next opcode. Mirrors alethia-reth's step-callback flush check at
		// adapter.rs:91-98. Required because op*Call swallows the Go error from
		// EVM.Call when ChargePrecompile/FinishAndCharge fails inside a child
		// frame, but the slot persists and exits the outer frame here.
		if evm.zkGasErr != nil {
			return nil, evm.zkGasErr
		}
		gasBefore := contract.Gas
		if debug {
```

- [ ] **Step 4: Run Test 1 to verify it passes**

Run: `go test ./core/vm/ -run TestUnzenZkGas_FailedPrecompileExceedingBlockLimit_StickyError -v`
Expected: PASS.

- [ ] **Step 5: Run full vm package tests**

Run: `go test ./core/vm/...`
Expected: PASS. All existing parity tests still pass (the slot is `nil` in those cases, so the new check is a no-op fast path).

- [ ] **Step 6: Commit**

```bash
git add core/vm/interpreter.go core/vm/taiko_zk_gas_runtime_test.go
git commit -m "$(cat <<'EOF'
feat(core): consume sticky zk-gas-limit slot at top of interpreter loop

Closes the op*Call swallow path: when ChargePrecompile or FinishAndCharge
fails inside a child frame, the Go error is swallowed by op*Call's
ok=false semantics, but the sticky slot survives and aborts the outer
frame at the next opcode dispatch. Mirrors alethia-reth adapter.rs:91-98.
EOF
)"
```

---

## Task 5: Reset slot per-tx in state processor

**Files:**
- Modify: `core/state_processor.go` (around line 134-137)
- Create: `core/taiko_zk_gas_sticky_executor_test.go` (placeholder; full test in Task 8)

- [ ] **Step 1: Write the failing test**

Append to `core/vm/taiko_zk_gas_sticky_test.go`:

```go
func TestEVMResetZkGasErr_CalledPerTxClearsSlot(t *testing.T) {
	// Simulate the state_processor's per-tx pattern: tx 1 sets slot, executor
	// calls ResetZkGasErr, tx 2 sees a clean slot.
	evm := &EVM{}
	evm.setZkGasLimitErr()
	if evm.zkGasErr == nil {
		t.Fatalf("setZkGasLimitErr did not set slot")
	}
	evm.ResetZkGasErr()
	if evm.zkGasErr != nil {
		t.Fatalf("ResetZkGasErr did not clear slot; zkGasErr = %v", evm.zkGasErr)
	}
}
```

This already passes after Task 1, but adding it now makes the per-tx contract explicit. Run it:

Run: `go test ./core/vm/ -run TestEVMResetZkGasErr_CalledPerTxClearsSlot -v`
Expected: PASS.

- [ ] **Step 2: Wire `ResetZkGasErr` into the state processor**

In `core/state_processor.go`, find (around line 134-137):

```go
		// CHANGE(taiko): reset in-flight zk gas before each transaction.
		if cfg.ZkGasMeter != nil {
			cfg.ZkGasMeter.ResetTransaction()
		}
```

Replace with:

```go
		// CHANGE(taiko): reset in-flight zk gas before each transaction.
		if cfg.ZkGasMeter != nil {
			cfg.ZkGasMeter.ResetTransaction()
			evm.ResetZkGasErr()
		}
```

- [ ] **Step 3: Verify compilation and existing tests**

Run: `go build ./... && go test ./core/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add core/state_processor.go core/vm/taiko_zk_gas_sticky_test.go
git commit -m "$(cat <<'EOF'
feat(core): clear sticky zk-gas-limit slot between transactions

Calls EVM.ResetZkGasErr alongside the existing meter ResetTransaction so
a previously-truncated tx's slot does not poison subsequent txs in the
same block-execution context.
EOF
)"
```

---

## Task 6: Test 2 — successful precompile over-limit

**Files:**
- Modify: `core/vm/taiko_zk_gas_runtime_test.go`

- [ ] **Step 1: Write the test**

Append to `core/vm/taiko_zk_gas_runtime_test.go`:

```go
func TestUnzenZkGas_SuccessfulPrecompileExceedingBlockLimit_StickyError(t *testing.T) {
	// STATICCALL to identity (0x04) with non-empty input. Identity always
	// succeeds, so this exercises the success path of ChargePrecompile.
	// PUSH1 0 PUSH1 32 PUSH1 0 PUSH1 0 PUSH1 0x04 PUSH3 0x0186a0 STATICCALL STOP
	code := common.Hex2Bytes("6000602060006000600462018680fa00")

	schedule := stickySchedule()
	// Identity precompile multiplier sized so even minimal gas use overshoots BlockLimit=1000.
	schedule.PrecompileMultipliers[0x04] = 10_000

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(schedule)
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./core/vm/ -run TestUnzenZkGas_SuccessfulPrecompileExceedingBlockLimit_StickyError -v`
Expected: PASS. Tasks 2 and 4 already cover this code path; this test confirms the success path is symmetric with the failure path.

- [ ] **Step 3: Commit**

```bash
git add core/vm/taiko_zk_gas_runtime_test.go
git commit -m "test(core): cover successful precompile over-limit sticky error path"
```

---

## Task 7: Test 3 — inner-frame opcode over-limit

**Files:**
- Modify: `core/vm/taiko_zk_gas_runtime_test.go`

- [ ] **Step 1: Write the test**

Append to `core/vm/taiko_zk_gas_runtime_test.go`:

```go
func TestUnzenZkGas_InnerFrameOpcodeExceedingBlockLimit_StickyError(t *testing.T) {
	// Outer contract CALLs into innerAddr. Inner code does ADD which trips
	// FinishAndCharge inside the inner frame. The outer opCall swallows the
	// Go error (ok=false on stack), but the sticky slot survives and aborts
	// the outer frame at the next top-of-loop check.
	innerAddr := common.HexToAddress("0x2000000000000000000000000000000000000000")

	// Outer: PUSH1 0 PUSH1 0 PUSH1 0 PUSH1 0 PUSH1 0 PUSH20 inner PUSH3 0x0186a0 CALL STOP
	outerCode := common.Hex2Bytes("60006000600060006000732000000000000000000000000000000000000000620186a0f100")
	// Inner: PUSH1 1 PUSH1 2 ADD STOP — ADD trips the over-limit FinishAndCharge.
	innerCode := common.Hex2Bytes("60016002010100")

	schedule := stickySchedule()
	schedule.OpcodeMultipliers[0x01] = 1024 // ADD overshoots BlockLimit=1000.

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, outerCode, tracing.CodeChangeUnspecified)
	statedb.CreateAccount(innerAddr)
	statedb.SetCode(innerAddr, innerCode, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(schedule)
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./core/vm/ -run TestUnzenZkGas_InnerFrameOpcodeExceedingBlockLimit_StickyError -v`
Expected: PASS. Tasks 3 and 4 already cover this code path; this test confirms the inner-frame swallow is closed.

- [ ] **Step 3: Commit**

```bash
git add core/vm/taiko_zk_gas_runtime_test.go
git commit -m "test(core): cover inner-frame opcode over-limit sticky error path"
```

---

## Task 8: Test 4 — executor-level integration test

**Files:**
- Create: `core/taiko_zk_gas_sticky_executor_test.go`

- [ ] **Step 1: Locate existing executor test patterns**

Run: `grep -rn "ApplyTransactionWithEVM\|state_processor_test" core/ --include="*.go" | head -10`

Use the existing `core/state_processor_test.go` (if present) or `core/taiko_*_test.go` files as a pattern reference. The goal is to drive `ApplyTransactionWithEVM` end-to-end with a tx whose precompile charge exceeds the limit and assert: (a) `result.Err == ErrZkGasLimitExceeded`, (b) statedb reverted to pre-tx snapshot, (c) gas pool reset.

- [ ] **Step 2: Write the test**

Create `core/taiko_zk_gas_sticky_executor_test.go`:

```go
package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// stickyExecutorSchedule mirrors the small-budget schedule from
// core/vm/taiko_zk_gas_sticky_test.go so a single failed precompile call
// overshoots the block limit.
func stickyExecutorSchedule() *vm.ZkGasSchedule {
	s := &vm.ZkGasSchedule{
		BlockLimit: 1_000,
		SpawnEstimates: vm.SpawnEstimates{
			Call: 1, CallCode: 1, DelegateCall: 1, StaticCall: 1,
			Create: 1, Create2: 1,
		},
	}
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = 0
	}
	for i := range s.PrecompileMultipliers {
		s.PrecompileMultipliers[i] = 1
	}
	s.PrecompileMultipliers[0x0a] = 1
	return s
}

func TestUnzenZkGas_StickyError_ApplyTransactionRevertsAndPropagates(t *testing.T) {
	// STATICCALL to point_evaluation (0x0a) with bad input: failed precompile,
	// full call gas charged, overshoots BlockLimit=1000.
	contractCode := common.Hex2Bytes("6000600060c06000600a620186a0fa00")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	senderAddr := common.HexToAddress("0xaaaa000000000000000000000000000000000000")

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode, tracing.CodeChangeUnspecified)
	statedb.CreateAccount(senderAddr)
	statedb.AddBalance(senderAddr, uint256.NewInt(1_000_000_000_000_000_000), tracing.BalanceChangeUnspecified)
	statedb.Finalise(true)
	preStateRoot := statedb.IntermediateRoot(true)

	meter := vm.NewZkGasMeter(stickyExecutorSchedule())
	cfg := vm.Config{ZkGasMeter: meter}

	blockCtx := vm.BlockContext{
		CanTransfer: func(_ vm.StateDB, _ common.Address, _ *uint256.Int) bool { return true },
		Transfer:    func(_ vm.StateDB, _ common.Address, _ common.Address, _ *uint256.Int, _ *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}
	evm := vm.NewEVM(blockCtx, statedb, params.MergedTestChainConfig, cfg)

	tx := types.NewTransaction(0, contractAddr, new(big.Int), 200_000, big.NewInt(1), nil)
	signer := types.HomesteadSigner{}
	msg, err := TransactionToMessage(tx, signer, nil)
	if err != nil {
		// Use a synthetic Message to avoid signing complications.
		msg = &Message{
			From:      senderAddr,
			To:        &contractAddr,
			Nonce:     0,
			Value:     new(big.Int),
			GasLimit:  200_000,
			GasPrice:  big.NewInt(1),
			GasFeeCap: big.NewInt(1),
			GasTipCap: big.NewInt(1),
			Data:      nil,
		}
	}

	gp := new(GasPool).AddGas(10_000_000)
	_, err = ApplyTransactionWithEVM(msg, gp, statedb, big.NewInt(1), common.Hash{}, 1, tx, evm)
	if err != vm.ErrZkGasLimitExceeded {
		t.Fatalf("ApplyTransactionWithEVM err = %v, want ErrZkGasLimitExceeded", err)
	}

	// Statedb must have been reverted: intermediate root matches the pre-tx root.
	postStateRoot := statedb.IntermediateRoot(true)
	if preStateRoot != postStateRoot {
		t.Fatalf("statedb not reverted: pre=%x post=%x", preStateRoot, postStateRoot)
	}
}
```

**Note on placement:** if `TransactionToMessage` requires a signed tx and signing the synthetic tx is awkward, use the `Message` literal fallback shown above. The point of the test is the executor behavior on an over-limit tx, not the tx-decoding path.

- [ ] **Step 3: Run test**

Run: `go test ./core/ -run TestUnzenZkGas_StickyError_ApplyTransactionRevertsAndPropagates -v`
Expected: PASS. If the test panics or fails to compile due to unused imports/parameters from the `Message` construction, fix the imports and the synthetic message fields to match the current `core.Message` shape — re-read `core/state_transition.go` for the exact field set.

- [ ] **Step 4: Commit**

```bash
git add core/taiko_zk_gas_sticky_executor_test.go
git commit -m "$(cat <<'EOF'
test(core): cover sticky zk-gas-limit propagation through ApplyTransactionWithEVM

Drives an over-limit precompile tx end-to-end and asserts the executor
returns ErrZkGasLimitExceeded and the statedb is reverted to the pre-tx
intermediate root.
EOF
)"
```

---

## Task 9: Final verification

**Files:** none.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: no warnings.

- [ ] **Step 3: Run gofmt check**

Run: `gofmt -l core/vm/evm.go core/vm/interpreter.go core/vm/taiko_zk_gas_sticky.go core/vm/taiko_zk_gas_sticky_test.go core/state_processor.go core/taiko_zk_gas_sticky_executor_test.go`
Expected: empty output (no formatting issues).

If anything outputs, run `gofmt -s -w <file>` for each.

- [ ] **Step 4: Run lint per project Makefile**

Run: `make lint`
Expected: PASS. If lint introduces warnings on the new files, address them inline — they are likely about CHANGE(taiko) comment placement or unused imports.

- [ ] **Step 5: Verify the diff is scoped to the planned files only**

Run: `git diff --name-only taiko..HEAD`
Expected: this list, nothing more:
```
core/state_processor.go
core/taiko_zk_gas_sticky_executor_test.go
core/vm/evm.go
core/vm/interpreter.go
core/vm/taiko_zk_gas_runtime_test.go
core/vm/taiko_zk_gas_sticky.go
core/vm/taiko_zk_gas_sticky_test.go
docs/superpowers/specs/2026-05-04-sticky-zk-gas-limit-error-design.md
docs/superpowers/plans/2026-05-04-sticky-zk-gas-limit-error.md
```
Plus any pre-existing modified files from the original `d1b80cf65` commit:
```
core/vm/taiko_zk_gas_runtime_test.go (already modified by d1b80cf65)
```

If extra files appear, investigate before declaring complete.

- [ ] **Step 6: Sanity-check the swallow behavior is unchanged for non-zk-gas errors**

Run: `go test ./core/vm/ -run TestUnzenZkGasParity -v`
Expected: PASS for all existing parity tests. The sticky-slot mechanism is a no-op when the slot is `nil`, so the existing `op*Call` swallow semantics for ordinary errors (revert, OOG, depth, etc.) are unchanged.

---

## Self-Review

Spec coverage check:

- **Architecture (sticky slot on `*EVM`):** Task 1 adds field + helpers. ✓
- **Components: `evm.go` slot field and four precompile sites:** Task 1 (field), Task 2 (sites). ✓
- **Components: `interpreter.go` top-of-loop check + FinishAndCharge site:** Task 4 (check), Task 3 (site). ✓
- **Components: `state_processor.go` per-tx clear:** Task 5. ✓
- **Components: `taiko_zk_gas_sticky.go` helpers file:** Task 1. ✓
- **Components: `ZkGasMeter` unchanged:** confirmed — no task touches it. ✓
- **Data flow: failed precompile path:** Tests in Tasks 2, 4, 6 exercise it end-to-end. ✓
- **Data flow: inner-frame opcode path:** Tests in Tasks 3, 7 exercise it. ✓
- **Data flow: tx boundary clear:** Task 5. ✓
- **Edge case: idempotent set:** Task 1 test `TestEVMSetZkGasLimitErr_DoesNotClobberExistingError`. ✓
- **Edge case: read-only EVM (nil meter):** existing `TestUnzenZkGasParity_*` tests run with the meter and confirm no regression; Task 9 step 6 explicitly re-verifies. The nil-meter case is implicitly covered by `go test ./...` running existing eth_call/estimateGas tests. ✓
- **Edge case: anchor tx protection:** out of scope for this branch — no task changes the `i > 0` check, and `state_processor.go:143-152` is unmodified. ✓
- **Edge case: tracer compatibility:** Task 4 test uses a tracer to assert opcode dispatch order, confirming tracer hooks still fire. ✓
- **Test 1 (failed precompile over-limit):** Task 4. ✓
- **Test 2 (successful precompile over-limit):** Task 6. ✓
- **Test 3 (inner-frame opcode over-limit):** Task 7. ✓
- **Test 4 (executor-level truncation):** Task 8. ✓

Placeholder scan: none. All steps have concrete code, exact commands, expected outputs.

Type consistency: `setZkGasLimitErr` (Task 1) is the same name everywhere it's referenced (Tasks 2, 3). `ResetZkGasErr` (Task 1) is consistently named in Task 5. `zkGasErr` field is consistently spelled.

Naming caveat: the small-budget schedule helper is called `stickySchedule()` in `core/vm/taiko_zk_gas_sticky_test.go` (Task 2) and `stickyExecutorSchedule()` in `core/taiko_zk_gas_sticky_executor_test.go` (Task 8). Different names because they live in different packages and can't share — intentional, but flagged here for the implementer.
