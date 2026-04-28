# Network-aware Unzen zk-gas block limit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port alethia-reth #170 to taiko-geth so the Unzen zk-gas block limit is 1B on Taiko Masaya (chain id 167011) and stays at 100M on Mainnet/Hoodi/Devnet, with all other zk-gas tables identical across networks.

**Architecture:** Refactor `core/vm/taiko_zk_gas_unzen.go` so a private builder `unzenZkGasScheduleWithBlockLimit(blockLimit)` produces both the existing `UnzenZkGasSchedule` (100M) and a new `MasayaUnzenZkGasSchedule` (1B). Add a `vm.UnzenZkGasScheduleFor(chainID)` selector keyed off `params.MasayaDevnetNetworkID`. Update the two meter-construction call sites (`core/state_processor.go:91`, `miner/taiko_worker.go:265`) to pass the chain id. Add tests pinning the Masaya schedule, asserting Masaya/default tables are byte-identical, covering selector behavior across all four Taiko chain ids, and pinning the four chain-id constants in `params`.

**Tech Stack:** Go 1.23+, taiko-geth (fork of go-ethereum v1.17.2). Standard `testing` package. No new dependencies.

---

## File Structure

**Modified:**
- `core/vm/taiko_zk_gas_unzen.go` — refactor builder, add `MasayaBlockZkGasLimit`, `BlockZkGasLimit`, `MasayaUnzenZkGasSchedule`, and `UnzenZkGasScheduleFor` selector. The file gains an import of `math/big` and `github.com/ethereum/go-ethereum/params`.
- `core/state_processor.go` — call `vm.UnzenZkGasScheduleFor(config.ChainID)` at line 91.
- `miner/taiko_worker.go` — call `vm.UnzenZkGasScheduleFor(w.chainConfig.ChainID)` at line 265.

**New:**
- `core/vm/taiko_zk_gas_unzen_test.go` — Masaya schedule + selector tests.

**Extended:**
- `params/config_test.go` — add `TestTaikoNetworkIDsArePinned`.

---

## Task 1: Add chain-id pin test in `params`

This is the smallest, most isolated change — start here so the chain-id values the selector depends on are explicitly pinned before we use them.

**Files:**
- Modify: `params/config_test.go` (append at end of file, after line 175)

- [ ] **Step 1: Append the pin test**

Append to `params/config_test.go`:

```go
func TestTaikoNetworkIDsArePinned(t *testing.T) {
	// Network IDs are part of consensus and must not silently drift if
	// params/taiko_config.go is edited. Genesis-hash tests do not depend on
	// chain id, so pin them here explicitly.
	cases := []struct {
		name string
		got  *big.Int
		want int64
	}{
		{"taiko-mainnet", TaikoMainnetNetworkID, 167000},
		{"taiko-internal", TaikoInternalNetworkID, 167001},
		{"masaya-devnet", MasayaDevnetNetworkID, 167011},
		{"taiko-hoodi", TaikoHoodiNetworkID, 167013},
	}
	for _, c := range cases {
		if c.got == nil || c.got.Int64() != c.want {
			t.Fatalf("%s network id = %v, want %d", c.name, c.got, c.want)
		}
	}
}
```

(`big` is already imported at `params/config_test.go:22`. No other imports change.)

- [ ] **Step 2: Run the test and verify it passes**

Run: `go test ./params/ -run TestTaikoNetworkIDsArePinned -v`
Expected: `PASS`. If any line fails, the constant in `params/taiko_config.go` was edited — fix the source, not the test.

- [ ] **Step 3: Commit**

```bash
git add params/config_test.go
git commit -m "test(params): pin Taiko network IDs

Mirrors alethia-reth's chain-id pin so silent drift in
params/taiko_config.go fails fast.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Add failing tests for Masaya schedule and selector

TDD: write the new test file before the implementation. The schedule and selector don't exist yet, so the test must reference symbols that the next task introduces.

**Files:**
- Create: `core/vm/taiko_zk_gas_unzen_test.go`

- [ ] **Step 1: Create the test file**

Write `core/vm/taiko_zk_gas_unzen_test.go`:

```go
package vm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// TestUnzenBlockLimits pins the per-network Unzen block budgets:
// Masaya gets a 10× higher budget; everyone else stays at 100M.
func TestUnzenBlockLimits(t *testing.T) {
	if got := UnzenZkGasSchedule.BlockLimit; got != 100_000_000 {
		t.Fatalf("UnzenZkGasSchedule.BlockLimit = %d, want 100_000_000", got)
	}
	if got := MasayaUnzenZkGasSchedule.BlockLimit; got != 1_000_000_000 {
		t.Fatalf("MasayaUnzenZkGasSchedule.BlockLimit = %d, want 1_000_000_000", got)
	}
	if got := BlockZkGasLimit; got != 100_000_000 {
		t.Fatalf("BlockZkGasLimit = %d, want 100_000_000", got)
	}
	if got := MasayaBlockZkGasLimit; got != 1_000_000_000 {
		t.Fatalf("MasayaBlockZkGasLimit = %d, want 1_000_000_000", got)
	}
}

// TestMasayaUnzenSchedule_SharesTablesWithDefault asserts byte-identity of
// every non-block-limit field. This guards against accidental drift between
// the two schedules — only the block budget should differ.
func TestMasayaUnzenSchedule_SharesTablesWithDefault(t *testing.T) {
	if MasayaUnzenZkGasSchedule.OpcodeMultipliers != UnzenZkGasSchedule.OpcodeMultipliers {
		t.Fatal("MasayaUnzenZkGasSchedule.OpcodeMultipliers differs from UnzenZkGasSchedule")
	}
	if MasayaUnzenZkGasSchedule.PrecompileMultipliers != UnzenZkGasSchedule.PrecompileMultipliers {
		t.Fatal("MasayaUnzenZkGasSchedule.PrecompileMultipliers differs from UnzenZkGasSchedule")
	}
	if MasayaUnzenZkGasSchedule.SpawnEstimates != UnzenZkGasSchedule.SpawnEstimates {
		t.Fatal("MasayaUnzenZkGasSchedule.SpawnEstimates differs from UnzenZkGasSchedule")
	}
}

// TestUnzenZkGasScheduleFor verifies that the selector routes Masaya (167011)
// to the 1B schedule and every other Taiko chain id to the default 100M
// schedule. Pointer comparison ensures the selector returns the package
// global, not a copy.
func TestUnzenZkGasScheduleFor(t *testing.T) {
	cases := []struct {
		name    string
		chainID *big.Int
		want    *ZkGasSchedule
	}{
		{"nil", nil, &UnzenZkGasSchedule},
		{"mainnet-167000", params.TaikoMainnetNetworkID, &UnzenZkGasSchedule},
		{"internal-167001", params.TaikoInternalNetworkID, &UnzenZkGasSchedule},
		{"hoodi-167013", params.TaikoHoodiNetworkID, &UnzenZkGasSchedule},
		{"masaya-167011", params.MasayaDevnetNetworkID, &MasayaUnzenZkGasSchedule},
		{"explicit-167011", big.NewInt(167011), &MasayaUnzenZkGasSchedule},
		{"unknown-1", big.NewInt(1), &UnzenZkGasSchedule},
	}
	for _, c := range cases {
		got := UnzenZkGasScheduleFor(c.chainID)
		if got != c.want {
			t.Fatalf("UnzenZkGasScheduleFor(%s) = %p, want %p", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail to compile**

Run: `go test ./core/vm/ -run 'TestUnzenBlockLimits|TestMasayaUnzenSchedule_SharesTablesWithDefault|TestUnzenZkGasScheduleFor' -v`
Expected: build failure with errors mentioning `MasayaUnzenZkGasSchedule`, `BlockZkGasLimit`, `MasayaBlockZkGasLimit`, and `UnzenZkGasScheduleFor` undefined. (Compile failure is the "failing test" for a Go package.)

Do **not** commit yet — failing-to-compile tests should land together with the implementation that makes them pass (Task 3).

---

## Task 3: Refactor schedule builder, add Masaya schedule + selector

This task replaces the body of `core/vm/taiko_zk_gas_unzen.go` so the schedule construction is parameterized by block limit, and adds the new public surface.

**Files:**
- Modify: `core/vm/taiko_zk_gas_unzen.go` (whole-file rewrite)

- [ ] **Step 1: Rewrite `core/vm/taiko_zk_gas_unzen.go`**

Replace the entire file contents with:

```go
package vm

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/params"
)

// CHANGE(taiko): zk-gas block limit on Devnet, Hoodi, and Mainnet during Unzen.
const BlockZkGasLimit uint64 = 100_000_000

// CHANGE(taiko): zk-gas block limit on the Taiko Masaya network during Unzen.
// Masaya runs Unzen with a 10× higher block budget than the other Taiko chains.
const MasayaBlockZkGasLimit uint64 = 1_000_000_000

// CHANGE(taiko): UnzenZkGasSchedule is the consensus zk-gas schedule used by
// Devnet, Hoodi, and Mainnet during the Unzen fork.
var UnzenZkGasSchedule = unzenZkGasScheduleWithBlockLimit(BlockZkGasLimit)

// CHANGE(taiko): MasayaUnzenZkGasSchedule is the consensus zk-gas schedule
// used by the Taiko Masaya network during the Unzen fork. Opcode multipliers,
// precompile multipliers, and spawn estimates are identical to
// UnzenZkGasSchedule; only the per-block budget differs.
var MasayaUnzenZkGasSchedule = unzenZkGasScheduleWithBlockLimit(MasayaBlockZkGasLimit)

// CHANGE(taiko): UnzenZkGasScheduleFor returns the Unzen zk-gas schedule for
// the given chain id. Taiko Masaya (167011) runs the 1B-budget schedule; all
// other chains use the default 100M-budget schedule.
func UnzenZkGasScheduleFor(chainID *big.Int) *ZkGasSchedule {
	if chainID != nil && chainID.Cmp(params.MasayaDevnetNetworkID) == 0 {
		return &MasayaUnzenZkGasSchedule
	}
	return &UnzenZkGasSchedule
}

// unzenZkGasScheduleWithBlockLimit builds an Unzen-shaped schedule with the
// requested block limit. Opcode multipliers, precompile multipliers, and
// spawn estimates are identical across all networks; only the block budget
// differs.
func unzenZkGasScheduleWithBlockLimit(blockLimit uint64) ZkGasSchedule {
	s := ZkGasSchedule{
		BlockLimit: blockLimit,
		SpawnEstimates: SpawnEstimates{
			Call:         12500,
			CallCode:     12500,
			DelegateCall: 3500,
			StaticCall:   3500,
			Create:       37000,
			Create2:      44500,
		},
	}

	// Failsafe default: all 256 opcode slots start at MaxUint16.
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = math.MaxUint16
	}

	// Known opcode multipliers (from the ZK gas spec).
	s.OpcodeMultipliers[0x00] = 0   // STOP
	s.OpcodeMultipliers[0x01] = 12  // ADD
	s.OpcodeMultipliers[0x02] = 21  // MUL
	s.OpcodeMultipliers[0x03] = 13  // SUB
	s.OpcodeMultipliers[0x04] = 110 // DIV
	s.OpcodeMultipliers[0x05] = 93  // SDIV
	s.OpcodeMultipliers[0x06] = 95  // MOD
	s.OpcodeMultipliers[0x07] = 29  // SMOD
	s.OpcodeMultipliers[0x08] = 71  // ADDMOD
	s.OpcodeMultipliers[0x09] = 152 // MULMOD
	s.OpcodeMultipliers[0x0a] = 33  // EXP
	s.OpcodeMultipliers[0x0b] = 21  // SIGNEXTEND
	s.OpcodeMultipliers[0x10] = 11  // LT
	s.OpcodeMultipliers[0x11] = 10  // GT
	s.OpcodeMultipliers[0x12] = 14  // SLT
	s.OpcodeMultipliers[0x13] = 14  // SGT
	s.OpcodeMultipliers[0x14] = 35  // EQ
	s.OpcodeMultipliers[0x15] = 8   // ISZERO
	s.OpcodeMultipliers[0x16] = 8   // AND
	s.OpcodeMultipliers[0x17] = 9   // OR
	s.OpcodeMultipliers[0x18] = 9   // XOR
	s.OpcodeMultipliers[0x19] = 6   // NOT
	s.OpcodeMultipliers[0x1a] = 9   // BYTE
	s.OpcodeMultipliers[0x1b] = 20  // SHL
	s.OpcodeMultipliers[0x1c] = 19  // SHR
	s.OpcodeMultipliers[0x1d] = 29  // SAR
	s.OpcodeMultipliers[0x20] = 85  // KECCAK256
	s.OpcodeMultipliers[0x30] = 22  // ADDRESS
	s.OpcodeMultipliers[0x31] = 6   // BALANCE
	s.OpcodeMultipliers[0x32] = 21  // ORIGIN
	s.OpcodeMultipliers[0x33] = 21  // CALLER
	s.OpcodeMultipliers[0x34] = 13  // CALLVALUE
	s.OpcodeMultipliers[0x35] = 20  // CALLDATALOAD
	s.OpcodeMultipliers[0x36] = 11  // CALLDATASIZE
	s.OpcodeMultipliers[0x37] = 12  // CALLDATACOPY
	s.OpcodeMultipliers[0x38] = 11  // CODESIZE
	s.OpcodeMultipliers[0x39] = 13  // CODECOPY
	s.OpcodeMultipliers[0x3a] = 14  // GASPRICE
	s.OpcodeMultipliers[0x3b] = 6   // EXTCODESIZE
	s.OpcodeMultipliers[0x3c] = 6   // EXTCODECOPY
	s.OpcodeMultipliers[0x3d] = 10  // RETURNDATASIZE
	s.OpcodeMultipliers[0x3e] = 9   // RETURNDATACOPY
	s.OpcodeMultipliers[0x3f] = 8   // EXTCODEHASH
	s.OpcodeMultipliers[0x40] = 7   // BLOCKHASH
	s.OpcodeMultipliers[0x41] = 21  // COINBASE
	s.OpcodeMultipliers[0x42] = 11  // TIMESTAMP
	s.OpcodeMultipliers[0x43] = 11  // NUMBER
	s.OpcodeMultipliers[0x44] = 28  // PREVRANDAO
	s.OpcodeMultipliers[0x45] = 11  // GASLIMIT
	s.OpcodeMultipliers[0x46] = 11  // CHAINID
	s.OpcodeMultipliers[0x47] = 85  // SELFBALANCE
	s.OpcodeMultipliers[0x48] = 11  // BASEFEE
	s.OpcodeMultipliers[0x49] = 10  // BLOBHASH
	s.OpcodeMultipliers[0x4a] = 15  // BLOBBASEFEE
	s.OpcodeMultipliers[0x50] = 5   // POP
	s.OpcodeMultipliers[0x51] = 20  // MLOAD
	s.OpcodeMultipliers[0x52] = 22  // MSTORE
	s.OpcodeMultipliers[0x53] = 9   // MSTORE8
	s.OpcodeMultipliers[0x54] = 5   // SLOAD
	s.OpcodeMultipliers[0x55] = 13  // SSTORE
	s.OpcodeMultipliers[0x56] = 3   // JUMP
	s.OpcodeMultipliers[0x57] = 5   // JUMPI
	s.OpcodeMultipliers[0x58] = 12  // PC
	s.OpcodeMultipliers[0x59] = 11  // MSIZE
	s.OpcodeMultipliers[0x5a] = 11  // GAS
	s.OpcodeMultipliers[0x5b] = 9   // JUMPDEST
	s.OpcodeMultipliers[0x5c] = 1   // TLOAD
	s.OpcodeMultipliers[0x5d] = 6   // TSTORE
	s.OpcodeMultipliers[0x5e] = 5   // MCOPY
	s.OpcodeMultipliers[0x5f] = 10  // PUSH0
	s.OpcodeMultipliers[0x60] = 5   // PUSH1
	s.OpcodeMultipliers[0x61] = 5   // PUSH2
	s.OpcodeMultipliers[0x62] = 6   // PUSH3
	s.OpcodeMultipliers[0x63] = 8   // PUSH4
	s.OpcodeMultipliers[0x64] = 6   // PUSH5
	s.OpcodeMultipliers[0x65] = 8   // PUSH6
	s.OpcodeMultipliers[0x66] = 7   // PUSH7
	s.OpcodeMultipliers[0x67] = 7   // PUSH8
	s.OpcodeMultipliers[0x68] = 9   // PUSH9
	s.OpcodeMultipliers[0x69] = 10  // PUSH10
	s.OpcodeMultipliers[0x6a] = 8   // PUSH11
	s.OpcodeMultipliers[0x6b] = 9   // PUSH12
	s.OpcodeMultipliers[0x6c] = 7   // PUSH13
	s.OpcodeMultipliers[0x6d] = 12  // PUSH14
	s.OpcodeMultipliers[0x6e] = 10  // PUSH15
	s.OpcodeMultipliers[0x6f] = 11  // PUSH16
	s.OpcodeMultipliers[0x70] = 13  // PUSH17
	s.OpcodeMultipliers[0x71] = 11  // PUSH18
	s.OpcodeMultipliers[0x72] = 11  // PUSH19
	s.OpcodeMultipliers[0x73] = 13  // PUSH20
	s.OpcodeMultipliers[0x74] = 14  // PUSH21
	s.OpcodeMultipliers[0x75] = 16  // PUSH22
	s.OpcodeMultipliers[0x76] = 12  // PUSH23
	s.OpcodeMultipliers[0x77] = 16  // PUSH24
	s.OpcodeMultipliers[0x78] = 13  // PUSH25
	s.OpcodeMultipliers[0x79] = 14  // PUSH26
	s.OpcodeMultipliers[0x7a] = 15  // PUSH27
	s.OpcodeMultipliers[0x7b] = 17  // PUSH28
	s.OpcodeMultipliers[0x7c] = 17  // PUSH29
	s.OpcodeMultipliers[0x7d] = 12  // PUSH30
	s.OpcodeMultipliers[0x7e] = 18  // PUSH31
	s.OpcodeMultipliers[0x7f] = 17  // PUSH32
	s.OpcodeMultipliers[0x80] = 6   // DUP1
	s.OpcodeMultipliers[0x81] = 5   // DUP2
	s.OpcodeMultipliers[0x82] = 6   // DUP3
	s.OpcodeMultipliers[0x83] = 6   // DUP4
	s.OpcodeMultipliers[0x84] = 6   // DUP5
	s.OpcodeMultipliers[0x85] = 6   // DUP6
	s.OpcodeMultipliers[0x86] = 5   // DUP7
	s.OpcodeMultipliers[0x87] = 6   // DUP8
	s.OpcodeMultipliers[0x88] = 7   // DUP9
	s.OpcodeMultipliers[0x89] = 5   // DUP10
	s.OpcodeMultipliers[0x8a] = 6   // DUP11
	s.OpcodeMultipliers[0x8b] = 8   // DUP12
	s.OpcodeMultipliers[0x8c] = 6   // DUP13
	s.OpcodeMultipliers[0x8d] = 7   // DUP14
	s.OpcodeMultipliers[0x8e] = 8   // DUP15
	s.OpcodeMultipliers[0x8f] = 6   // DUP16
	s.OpcodeMultipliers[0x90] = 16  // SWAP1
	s.OpcodeMultipliers[0x91] = 18  // SWAP2
	s.OpcodeMultipliers[0x92] = 18  // SWAP3
	s.OpcodeMultipliers[0x93] = 19  // SWAP4
	s.OpcodeMultipliers[0x94] = 16  // SWAP5
	s.OpcodeMultipliers[0x95] = 17  // SWAP6
	s.OpcodeMultipliers[0x96] = 17  // SWAP7
	s.OpcodeMultipliers[0x97] = 15  // SWAP8
	s.OpcodeMultipliers[0x98] = 18  // SWAP9
	s.OpcodeMultipliers[0x99] = 17  // SWAP10
	s.OpcodeMultipliers[0x9a] = 18  // SWAP11
	s.OpcodeMultipliers[0x9b] = 19  // SWAP12
	s.OpcodeMultipliers[0x9c] = 19  // SWAP13
	s.OpcodeMultipliers[0x9d] = 18  // SWAP14
	s.OpcodeMultipliers[0x9e] = 17  // SWAP15
	s.OpcodeMultipliers[0x9f] = 18  // SWAP16
	s.OpcodeMultipliers[0xa0] = 6   // LOG0
	s.OpcodeMultipliers[0xa1] = 7   // LOG1
	s.OpcodeMultipliers[0xa2] = 4   // LOG2
	s.OpcodeMultipliers[0xa3] = 5   // LOG3
	s.OpcodeMultipliers[0xa4] = 5   // LOG4
	s.OpcodeMultipliers[0xf0] = 1   // CREATE
	s.OpcodeMultipliers[0xf1] = 25  // CALL
	s.OpcodeMultipliers[0xf2] = 24  // CALLCODE
	s.OpcodeMultipliers[0xf3] = 0   // RETURN
	s.OpcodeMultipliers[0xf4] = 21  // DELEGATECALL
	s.OpcodeMultipliers[0xf5] = 1   // CREATE2
	s.OpcodeMultipliers[0xfa] = 24  // STATICCALL
	s.OpcodeMultipliers[0xfd] = 0   // REVERT
	s.OpcodeMultipliers[0xfe] = 0   // INVALID
	s.OpcodeMultipliers[0xff] = 0   // SELFDESTRUCT

	// Failsafe default: all 256 precompile slots start at MaxUint16.
	for i := range s.PrecompileMultipliers {
		s.PrecompileMultipliers[i] = math.MaxUint16
	}

	// Known precompile multipliers (from the ZK gas spec).
	s.PrecompileMultipliers[0x01] = 81   // ecrecover
	s.PrecompileMultipliers[0x02] = 10   // sha256
	s.PrecompileMultipliers[0x03] = 3    // ripemd160
	s.PrecompileMultipliers[0x04] = 2    // identity
	s.PrecompileMultipliers[0x05] = 1363 // modexp
	s.PrecompileMultipliers[0x06] = 38   // bn128_add
	s.PrecompileMultipliers[0x07] = 87   // bn128_mul
	s.PrecompileMultipliers[0x08] = 82   // bn128_pairing
	s.PrecompileMultipliers[0x09] = 243  // blake2f
	s.PrecompileMultipliers[0x0a] = 398  // point_evaluation
	s.PrecompileMultipliers[0x0b] = 112  // bls12_g1add
	s.PrecompileMultipliers[0x0c] = 52   // bls12_g1msm
	s.PrecompileMultipliers[0x0e] = 111  // bls12_g2add
	s.PrecompileMultipliers[0x0f] = 39   // bls12_g2msm
	s.PrecompileMultipliers[0x11] = 134  // bls12_pairing
	s.PrecompileMultipliers[0x12] = 159  // bls12_map_fp_to_g1
	s.PrecompileMultipliers[0x13] = 112  // bls12_map_fp2_to_g2

	return s
}
```

Notes:
- The body is byte-identical to today's IIFE except: `BlockLimit: 100_000_000` → `BlockLimit: blockLimit`, the IIFE wrapper is replaced by a named function returning `ZkGasSchedule`, and `var UnzenZkGasSchedule = func() ZkGasSchedule { ... }()` becomes two `var` lines that call the named builder.
- `params.MasayaDevnetNetworkID` is the existing constant for chain id 167011 (`params/taiko_config.go:42`).

- [ ] **Step 2: Run the new tests and verify they pass**

Run: `go test ./core/vm/ -run 'TestUnzenBlockLimits|TestMasayaUnzenSchedule_SharesTablesWithDefault|TestUnzenZkGasScheduleFor' -v`
Expected: all three tests `PASS`.

- [ ] **Step 3: Run the full `core/vm` test suite to confirm no regression**

Run: `go test ./core/vm/...`
Expected: all tests pass. Existing references to `UnzenZkGasSchedule` (in `taiko_zk_gas_test.go` and `taiko_zk_gas_runtime_test.go`) still resolve because the variable's name and type are unchanged.

- [ ] **Step 4: Commit**

```bash
git add core/vm/taiko_zk_gas_unzen.go core/vm/taiko_zk_gas_unzen_test.go
git commit -m "feat(vm): add MasayaUnzenZkGasSchedule with shared tables

Refactor the Unzen schedule into a builder parameterized by block limit
so Devnet/Hoodi/Mainnet keep the 100M budget while Taiko Masaya runs
with a 1B budget. Multipliers and spawn estimates are unchanged.

Adds UnzenZkGasScheduleFor(chainID) for chain-id-driven selection.

Mirrors alethia-reth #170.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Wire selector into `core/state_processor.go`

**Files:**
- Modify: `core/state_processor.go:91`

- [ ] **Step 1: Edit the call site**

In `core/state_processor.go`, replace line 91:

```go
		cfg.ZkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
```

with:

```go
		cfg.ZkGasMeter = vm.NewZkGasMeter(vm.UnzenZkGasScheduleFor(config.ChainID))
```

(`config` is already in scope at line 66: `config = p.chainConfig()`.)

- [ ] **Step 2: Run state-processor tests**

Run: `go test ./core/ -run TestUnzen -v`
Expected: all pass. The existing Unzen state-processor tests use chain id 167000 (`core/taiko_state_processor_unzen_test.go:31`), which routes through the default schedule — behavior is unchanged.

- [ ] **Step 3: Run the full `core/` test suite**

Run: `go test ./core/...`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add core/state_processor.go
git commit -m "feat(core): select Unzen zk-gas schedule by chain id

State processor now consults UnzenZkGasScheduleFor so Masaya blocks are
metered against the 1B budget while other chains keep 100M.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Wire selector into `miner/taiko_worker.go`

**Files:**
- Modify: `miner/taiko_worker.go:265`

- [ ] **Step 1: Edit the call site**

In `miner/taiko_worker.go`, replace line 265:

```go
		zkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
```

with:

```go
		zkGasMeter = vm.NewZkGasMeter(vm.UnzenZkGasScheduleFor(w.chainConfig.ChainID))
```

(`w.chainConfig` is the worker's chain config; `ChainID` is referenced the same way at `miner/taiko_worker.go:282`.)

- [ ] **Step 2: Run miner tests**

Run: `go test ./miner/...`
Expected: all tests pass. `miner/taiko_worker_test.go` constructs its own inline schedule (line 124) and does not depend on the selector.

- [ ] **Step 3: Commit**

```bash
git add miner/taiko_worker.go
git commit -m "feat(miner): select Unzen zk-gas schedule by chain id

Block-builder worker now consults UnzenZkGasScheduleFor so Masaya
proposals are budgeted against 1B and others against 100M.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Workspace verification

**Files:** none modified.

- [ ] **Step 1: Build the binary**

Run: `make geth`
Expected: build succeeds.

- [ ] **Step 2: Run the full test suite**

Run: `make test`
Expected: all tests pass. If any unrelated test was already failing on `taiko` HEAD, note it but don't fix it as part of this change.

- [ ] **Step 3: Run linters**

Run: `make lint`
Expected: clean.

- [ ] **Step 4: (Optional) Confirm `go vet`**

Run: `go vet ./...`
Expected: no diagnostics.

No commit for this task — it's a verification gate. If anything fails, fix it in a follow-up commit before declaring done.

---

## Self-review notes

- **Spec coverage:** every section of the spec maps to a task. Schedule definitions and selector → Task 3. Call-site updates → Tasks 4, 5. Tests: Masaya block limit, table-sharing, selector behavior → Task 2 (failing) + Task 3 (passing). Chain-id pin → Task 1. Verification commands → Task 6.
- **Type consistency:** `UnzenZkGasScheduleFor` returns `*ZkGasSchedule` everywhere it's referenced. `BlockZkGasLimit` and `MasayaBlockZkGasLimit` are typed `uint64` to match `ZkGasSchedule.BlockLimit`. Selector takes `*big.Int` and uses `.Cmp` (the same pattern as `core/taiko_genesis.go:75` for chain-id matching).
- **No placeholders:** every code block is complete and ready to paste.
