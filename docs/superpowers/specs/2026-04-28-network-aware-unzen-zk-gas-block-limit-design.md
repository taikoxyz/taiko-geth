# Network-aware Unzen zk-gas block limit (taiko-geth port of alethia-reth #170)

## Background

alethia-reth commit `7e8ee81` ("feat(chainspec): differentiate block limit by
network — Masaya 1B, others 100M", PR #170) splits the Unzen zk-gas block
budget by network: Taiko Masaya (chain id `167_011`) gets a 10× higher block
limit (`1_000_000_000`), while Devnet (`167_001`), Hoodi (`167_013`) and
Mainnet (`167_000`) keep the original `100_000_000`. Opcode multipliers,
precompile multipliers, and spawn estimates are identical across both
schedules — only the block budget differs.

This spec ports that change to taiko-geth so the two execution clients stay
consensus-aligned during Unzen.

## Goals

- Match alethia-reth's network-aware Unzen schedule selection exactly:
  chain id `167_011` → 1B block limit, all other Taiko chains → 100M.
- Keep all non-block-limit fields (`OpcodeMultipliers`, `PrecompileMultipliers`,
  `SpawnEstimates`) identical across both schedules and identical to today's
  `UnzenZkGasSchedule`.
- Minimize churn: no rename of existing variables, no refactor of unrelated
  code paths.
- Add tests that pin the Masaya schedule values and the chain-id-driven
  selector behavior, mirroring the alethia-reth test additions.

## Non-goals

- Renaming `params.MasayaDevnetNetworkID` (decided: option A — reuse as-is).
- Introducing a new chain-id constant in `params/`.
- Changing the call sites that wire the meter into the EVM beyond the two
  existing construction points.
- Touching the `ZkGasMeter.Schedule()` accessor — it already exists in
  taiko-geth (`core/vm/taiko_zk_gas.go:81`), so the alethia-reth `meter.schedule()`
  addition is a no-op for us.

## Mapping to alethia-reth

| alethia-reth | taiko-geth |
|---|---|
| `crates/chainspec/src/lib.rs` chain-id consts | `params/taiko_config.go` (already has `MasayaDevnetNetworkID = 167011`) — no change |
| `crates/evm/src/zk_gas/unzen.rs` `MASAYA_UNZEN_ZK_GAS_SCHEDULE` + `unzen_schedule_with_block_limit` builder | `core/vm/taiko_zk_gas_unzen.go` — add `MasayaUnzenZkGasSchedule`, refactor builder |
| `crates/evm/src/zk_gas/schedule.rs` `schedule_for(spec, chain_id)` | `core/vm/taiko_zk_gas_unzen.go` — new `UnzenZkGasScheduleFor(chainID *big.Int) *ZkGasSchedule` |
| `crates/evm/src/zk_gas/adapter.rs` `shared_meter_for_spec(spec, chain_id)` | n/a — taiko-geth constructs the meter directly at the two call sites |
| `crates/evm/src/factory.rs` factory plumbing | `core/state_processor.go:91`, `miner/taiko_worker.go:265` — pass chain id into selector |
| `crates/evm/src/zk_gas/meter.rs` `schedule()` accessor | already present at `core/vm/taiko_zk_gas.go:81` — no change |
| Cargo.lock dep added | n/a |
| chain-id pinning test | new test in `params/` covering existing network-id values |
| Masaya schedule values test | new test in `core/vm/` |
| factory schedule selection test | new selector test in `core/vm/` + adjusted assertions in existing block-limit tests |

## Design

### 1. Schedule definitions (`core/vm/taiko_zk_gas_unzen.go`)

Refactor the current single-schedule file so the multipliers and spawn
estimates are produced by a private builder function that accepts the block
limit. Today the schedule is built by an immediately-invoked anonymous
function at package init; we keep that pattern but parameterize the block
limit and call it twice — once at 100M and once at 1B.

Add two exported package-level constants for documentation/symmetry with
alethia-reth:

```go
// CHANGE(taiko): zk-gas block limit on Devnet, Hoodi, and Mainnet during Unzen.
const BlockZkGasLimit uint64 = 100_000_000

// CHANGE(taiko): zk-gas block limit on the Taiko Masaya network during Unzen.
const MasayaBlockZkGasLimit uint64 = 1_000_000_000
```

Add the new schedule:

```go
// CHANGE(taiko): MasayaUnzenZkGasSchedule is the Unzen schedule used on Taiko
// Masaya. It shares all multipliers and spawn estimates with UnzenZkGasSchedule
// and only raises the per-block budget to 1B.
var MasayaUnzenZkGasSchedule = unzenZkGasScheduleWithBlockLimit(MasayaBlockZkGasLimit)

// existing variable, now produced by the same builder
var UnzenZkGasSchedule = unzenZkGasScheduleWithBlockLimit(BlockZkGasLimit)
```

The private builder `unzenZkGasScheduleWithBlockLimit(blockLimit uint64) ZkGasSchedule`
contains the body that the existing IIFE builds — same multiplier table, same
precompile table, same spawn estimates, only the `BlockLimit` field differs.

### 2. Selector (`core/vm/taiko_zk_gas_unzen.go`)

Add a small selector function at the bottom of the same file:

```go
// CHANGE(taiko): UnzenZkGasScheduleFor returns the Unzen zk-gas schedule for
// the given chain id. Taiko Masaya (167_011) runs the 1B-budget schedule; all
// other chains use the default 100M-budget schedule.
func UnzenZkGasScheduleFor(chainID *big.Int) *ZkGasSchedule {
    if chainID != nil && chainID.Cmp(params.MasayaDevnetNetworkID) == 0 {
        return &MasayaUnzenZkGasSchedule
    }
    return &UnzenZkGasSchedule
}
```

`core/vm` already imports `params` (verified at `core/vm/evm.go:30`), so this
introduces no new package-level dependency.

### 3. Call-site updates

Two sites construct the meter today. Both already have the chain config in
scope, so this is a one-line change at each site.

**`core/state_processor.go:91`** — replace
```go
cfg.ZkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
```
with
```go
cfg.ZkGasMeter = vm.NewZkGasMeter(vm.UnzenZkGasScheduleFor(config.ChainID))
```

**`miner/taiko_worker.go:265`** — replace
```go
zkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
```
with
```go
zkGasMeter = vm.NewZkGasMeter(vm.UnzenZkGasScheduleFor(w.chainConfig.ChainID))
```

### 4. Tests

**`core/vm/taiko_zk_gas_unzen_test.go` (new file)** — three tests:

1. `TestMasayaUnzenSchedule_BlockLimit` — assert
   `MasayaUnzenZkGasSchedule.BlockLimit == 1_000_000_000` and
   `UnzenZkGasSchedule.BlockLimit == 100_000_000`.
2. `TestMasayaUnzenSchedule_SharesTablesWithDefault` — assert that
   `MasayaUnzenZkGasSchedule.OpcodeMultipliers == UnzenZkGasSchedule.OpcodeMultipliers`,
   `PrecompileMultipliers` likewise, and `SpawnEstimates` likewise. This is the
   Go analogue of alethia-reth's `PartialEq`-based pinning and guards against
   accidental drift between the two tables.
3. `TestUnzenZkGasScheduleFor` — table-driven test:
   - `nil` → `&UnzenZkGasSchedule`
   - `params.TaikoMainnetNetworkID` (167000) → `&UnzenZkGasSchedule`
   - `params.TaikoInternalNetworkID` (167001) → `&UnzenZkGasSchedule`
   - `params.TaikoHoodiNetworkID` (167013) → `&UnzenZkGasSchedule`
   - `params.MasayaDevnetNetworkID` (167011) → `&MasayaUnzenZkGasSchedule`

   Compare by pointer identity to confirm the selector returned the right
   global, not a copy.

**`params/config_test.go` (extend)** — analogue of alethia-reth's
`test_taiko_genesis_chain_ids_are_pinned`: a small test pinning the
numeric values of `TaikoMainnetNetworkID`, `TaikoInternalNetworkID`,
`MasayaDevnetNetworkID`, and `TaikoHoodiNetworkID` to `167000`, `167001`,
`167011`, `167013` respectively. Genesis-hash tests in this repo (if any)
do not cover chain id, so this is the consensus-relevant safety net.

**No changes** to `core/vm/taiko_zk_gas_test.go`, `core/vm/taiko_zk_gas_runtime_test.go`,
`core/taiko_state_processor_unzen_test.go`, or `miner/taiko_worker_test.go`:
existing tests construct schedules inline (`&vm.ZkGasSchedule{BlockLimit: ...}`)
or reference `UnzenZkGasSchedule` directly — none of them need to know about
the chain-id-aware selector.

## Risk and verification

- **Consensus risk:** the only behavior change is `BlockLimit` going from
  100M → 1B on chain id `167_011`. All other chains see byte-identical
  schedules. Verified by the new `SharesTablesWithDefault` test.
- **Off-by-one chain-id risk:** the selector is keyed off
  `params.MasayaDevnetNetworkID`. The new `params/config_test.go` chain-id
  pin guards against silent drift if anyone edits the network IDs.
- **Verification commands:**
  - `go test ./core/vm/...`
  - `go test ./core/...`
  - `go test ./miner/...`
  - `go test ./params/...`
  - `make lint`

## Out of scope

- Renaming `MasayaDevnetNetworkID`. Cleanup, can be done later.
- Adding a `params.TaikoMasayaChainID` constant. Decided against — keep diff
  minimal.
- Threading chain id through additional layers (factory pattern). taiko-geth
  doesn't have an EVM factory analogue worth introducing for two call sites.

## File-touch summary

Modified:
- `core/vm/taiko_zk_gas_unzen.go` — refactor builder, add Masaya schedule + selector
- `core/state_processor.go` — pass chain id into selector
- `miner/taiko_worker.go` — pass chain id into selector

New:
- `core/vm/taiko_zk_gas_unzen_test.go` — Masaya schedule + selector tests

Extended:
- `params/config_test.go` — chain-id pin test
