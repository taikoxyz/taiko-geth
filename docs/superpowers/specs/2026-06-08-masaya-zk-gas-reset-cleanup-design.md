# Masaya Zk-Gas Reset Cleanup Design

## Context

Masaya is being reset, so taiko-geth no longer needs the historical Masaya-only
Unzen zk-gas schedule that preserved consensus for already-finalized Masaya
blocks. The cleanup should align with `taikoxyz/alethia-reth#205`, whose target
shape is:

- Masaya activates Unzen from genesis.
- Every chain uses the single standard Unzen zk-gas schedule.
- Masaya-specific frozen zk-gas constants, tables, and tests are removed.
- Unrelated Masaya behavior stays out of scope.

The current taiko-geth code carries the matching special cases in:

- `core/taiko_genesis.go`: `MasayaUnzenTime` is `1_778_158_800`.
- `core/vm/taiko_zk_gas_unzen.go`: Masaya-specific block limit, zero intrinsic
  charge, frozen opcode table, frozen precompile table, and chain-id schedule
  selector.
- `core/state_processor.go`: Unzen block execution selects the zk-gas schedule
  through `vm.UnzenZkGasScheduleFor(config.ChainID)`.
- Tests under `core/vm/` and `core/` that pin the special Masaya schedule.

## Goals

- Reset Masaya to fork into Unzen at genesis.
- Remove the special Masaya Unzen zk-gas implementation.
- Keep all chains on the standard Unzen zk-gas block limit, intrinsic charge,
  opcode multipliers, and precompile multipliers.
- Preserve unrelated Masaya code paths and identifiers.
- Verify the Masaya genesis hash after the fork timestamp change.

## Non-Goals

- Do not remove the Masaya network ID.
- Do not edit Masaya genesis allocation JSON unless a focused verification shows
  it is required.
- Do not change unrelated Masaya lookup, RPC, batch, or topology behavior.
- Do not broaden this into mainnet, Hoodi, or internal-devnet fork scheduling
  changes.

## Design

### Fork Activation

In `core/taiko_genesis.go`, set `MasayaUnzenTime` to `0`. The existing Masaya
case already assigns `UnzenTime`, `CancunTime`, `PragueTime`, and `OsakaTime`
from `MasayaUnzenTime` when the value is not `math.MaxUint64`, so this makes
Masaya genesis an Osaka-era Unzen block without adding a new branch.

Add or adjust a focused test that proves the Masaya genesis config has Unzen,
Cancun, Prague, and Osaka active at block `0`, timestamp `0`, using the
existing `ChainConfig` fork predicate methods.

### Zk-Gas Schedule

In `core/vm/taiko_zk_gas_unzen.go`, keep only the standard Unzen schedule:

- `BlockZkGasLimit = 100_000_000`
- `TxIntrinsicZkGas = 243_000`
- `UnzenZkGasSchedule`
- `unzenOpcodeMultipliers`
- `unzenPrecompileMultipliers`

Delete the Masaya-only schedule surface:

- `MasayaBlockZkGasLimit`
- `MasayaTxIntrinsicZkGas`
- `MasayaUnzenZkGasSchedule`
- `masayaUnzenOpcodeMultipliers`
- `masayaUnzenPrecompileMultipliers`

Collapse schedule selection so Unzen execution always receives
`UnzenZkGasSchedule`. The simplest shape is to remove
`UnzenZkGasScheduleFor(chainID)` and update `core/state_processor.go` to use
`vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)` directly. If keeping a helper is
cleaner for local style, it must not accept or branch on `chainID`.

### Tests

Update `core/vm/taiko_zk_gas_unzen_test.go` to pin the single standard schedule:

- The Unzen block limit is `100_000_000`.
- The per-tx intrinsic charge is `243_000`.
- Representative opcode and precompile multipliers remain pinned.
- Full-address precompile lookup and failsafe behavior remain tested.

Remove tests that assert Masaya differs from the default schedule, including:

- The 1B Masaya block budget assertions.
- The zero Masaya tx-intrinsic assertions.
- Frozen Masaya opcode and precompile table assertions.
- Schedule selector tests that route chain id `167011` to a separate schedule.

Update `core/taiko_state_processor_unzen_test.go` by removing the Masaya
zero-intrinsic test. Keep the standard intrinsic inclusion test because it
continues to verify the consensus wiring from `ApplyTransactionWithEVM` into the
zk-gas meter.

### Error Handling and Compatibility

No new runtime error path is introduced. Removing the Masaya selector means
Masaya reset nodes will reject old pre-reset Masaya blocks whose header
difficulty depended on the frozen schedule. That is expected because those
blocks are discarded by the reset.

Existing Unzen error behavior remains unchanged:

- Blob transactions are still rejected in Unzen blocks.
- `ErrZkGasLimitExceeded` still propagates through the existing state processor
  and EVM paths.
- Missing precompile multipliers still resolve to the failsafe multiplier.

### Verification

Run focused verification first:

- `go test ./core/vm`
- `go test ./core -run 'Taiko|Genesis|Unzen|ZkGas'`

If those pass, run a broader package check that covers the changed execution
path:

- `go test ./core/...`

When `core/taiko_genesis.go` changes, print the Masaya chain id and resulting
genesis hash as required by repository guidelines:

- Chain ID: `167011`
- Genesis hash: compute from `core.TaikoGenesisBlock(167011).ToBlock().Hash()`
  after the change.

## Acceptance Criteria

- Masaya Unzen activates at timestamp `0` in taiko-geth.
- Masaya uses the same Unzen zk-gas schedule as every other chain.
- No Masaya-specific zk-gas constants, tables, or schedule selector branches
  remain.
- Unrelated Masaya code paths remain untouched.
- Focused tests pass.
- The final result reports chain ID `167011` and the computed Masaya genesis
  hash.
