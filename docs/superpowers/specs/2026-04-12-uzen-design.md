# Uzen Fork Design For taiko-geth

## Objective

Bring taiko-geth to runtime parity with the `Uzen` changes already landed in `alethia-reth` after commit `2415860654d0`, with `zk gas` treated as an in-fork requirement rather than a separate feature.

This spec targets behavior parity for Alethia mainnet/runtime paths. It does not attempt broad operator rollout work or force taiko-geth to mirror reth's internal module layout.

## Source Scope

The reference delta in `alethia-reth` is the set of commits after `2415860654d0`:

- `dd2d4b5974d43f652f785e4b69b1e56417c3f853` `feat(chainspec): add Uzen fork (timestamp-based), map to OSAKA, and no blob transaction`
- `dfbe4c7769c8115f06a5e9f96c910f07f9dbb57b` `feat(evm): introduce ZKGas in Uzen fork`
- `a61761ee9a8c172a222c6892325c6521a3abbfa5` `chore(block): add Uzen parent beacon block root normalization`
- `b3f4ee9f56d9ee9638b1de4b688bd57907ea7818` `chore(rpc): normalize Uzen parent beacon block root in engine validator`
- `c564833c8af5330ec6e88df115fe156a80daeb7c` `chore(block): update Uzen requests hash reconstruction`

The zk-gas behavior follows the protocol spec at `packages/protocol/docs/zk_gas_spec.md` in `taiko-mono`.

## Scope

`Uzen` is a new Taiko timestamp fork in taiko-geth, similar in activation style to `Shasta`.

At or after `Uzen`, taiko-geth must:

- activate the fork from `UzenTime`
- use Osaka-era execution payload semantics where needed for payload validation
- reject blob transactions
- normalize `parentBeaconBlockRoot` for Taiko payload round-tripping
- use the empty requests hash expected by the fork
- enforce block-level zk-gas limits during execution, including anchor/system transactions

The implementation must be reusable for future Taiko networks through config/genesis changes, not Alethia-only hardcoding.

## Non-Goals

- Operator-facing rollout docs beyond what is needed to keep config/genesis coherent
- Reproducing alethia-reth's internal file/module boundaries
- Retrofitting every historical Taiko network unless shared config requires it
- Over-expanding the work into a generic execution refactor unrelated to Uzen

## Recommended Approach

Adopt a first-class `Uzen` fork layer on top of the existing Taiko fork model.

This keeps taiko-geth aligned with its current pattern:

- chain config owns fork activation
- consensus/engine/block-processing code branches on explicit Taiko fork helpers
- fork-specific runtime behavior is centralized at the seam where it matters
- zk-gas metering is introduced as a dedicated execution concern rather than scattered special cases

This is preferred over inline patching because Uzen touches multiple subsystems and would otherwise become hard to reason about.

## Architecture

### 1. Chain config and genesis

Add `UzenTime` to `params.ChainConfig` and expose `IsUzen(time uint64) bool`.

The config layer must also:

- include `UzenTime` in string/banner output
- include `UzenTime` in compatibility checks
- wire Alethia defaults through Taiko genesis/network config
- preserve the pattern used by other Taiko forks so future networks can enable Uzen through config only

`Uzen` is a Taiko fork keyed by block timestamp. It is not a block-number fork.

### 2. Effective protocol context

Uzen should be treated as Osaka-era for payload semantics where alethia-reth does so, while still applying Taiko-specific restrictions such as no blob transactions.

The design assumption is:

- upstream fork-era payload rules may still come from Osaka-era execution expectations
- Taiko-specific Uzen rules override any otherwise-allowed blob behavior

This split keeps taiko-geth compatible with the reth change that maps Uzen to Osaka semantics without inheriting unwanted blob behavior.

### 3. Payload normalization and validation

`Uzen` is the activation boundary for payload/header normalization.

At or after `Uzen`:

- missing `parentBeaconBlockRoot` must be normalized to zero in Taiko payload handling paths that would otherwise fail round-trip validation
- `requestsHash` must resolve to the empty requests hash expected by Uzen payloads
- blob transactions must be rejected in inbound payload validation and in local payload building

Before `Uzen`:

- existing validation rules remain unchanged
- Uzen-only normalization must not silently affect earlier forks

### 4. Transaction admission and local block building

Blob-transaction rejection must be enforced at more than one layer.

The design requires:

- tx admission validation rejects blob txs once `Uzen` is active
- engine/payload validation rejects blocks or payload contents that include blob txs once `Uzen` is active
- local Taiko block building never includes blob txs once `Uzen` is active

Existing miner-side blob skipping is useful but insufficient on its own because parity requires deterministic rejection, not just best-effort omission during block assembly.

### 5. zk-gas metering

Introduce a dedicated zk-gas metering module around EVM execution.

The module owns:

- `BLOCK_ZK_GAS_LIMIT = 100_000_000`
- opcode multiplier table
- precompile multiplier table
- fixed spawn estimates for `CALL`, `CALLCODE`, `DELEGATECALL`, `STATICCALL`, `CREATE`, `CREATE2`
- block-scoped zk-gas accumulator
- tx-scoped zk-gas accumulator
- overflow-safe accounting using unsigned 64-bit arithmetic

Metering rules:

- every opcode contributes `raw_gas * opcode_multiplier[opcode]`
- `raw_gas` is `step_gas` for ordinary opcodes
- for spawn opcodes that actually create a child frame or invoke a precompile, `raw_gas` uses the fixed spawn estimate instead of observed `step_gas`
- precompile work is metered separately as `precompile_gas_used * precompile_multiplier[address_low_byte]`
- unknown opcode/precompile multipliers default to a fail-safe maximum that will immediately exceed the limit and make missing coverage obvious

### 6. Block processing semantics

zk gas is a block-level budget enforced during sequential tx execution.

Processing rules:

- all transactions in the block are metered, including anchor/system transactions
- successful transactions before the offending transaction are preserved
- when the limit is exceeded during a transaction, that transaction is fully discarded
- once the offending transaction is discarded, all remaining transactions are skipped
- this outcome is not a normal EVM revert and not a fatal block-import failure

This implies a dedicated internal execution outcome such as `zk gas exceeded` that block processing can distinguish from:

- ordinary transaction revert
- fatal execution/import error

## Component Boundaries

### `params/`

Responsibility:

- fork activation API for `Uzen`
- config serialization/banner/compatibility
- reusable network-level activation data

Expected changes:

- `ChainConfig.UzenTime`
- `IsUzen`
- config description/banner output
- compatibility checks

### `core/taiko_genesis.go` and Taiko network config

Responsibility:

- Alethia network defaults
- reusable activation plumbing for future Taiko networks

Expected changes:

- set `UzenTime` for Alethia-targeted config
- keep network selection and genesis wiring consistent with existing Taiko patterns

### `eth/catalyst` and `beacon/engine`

Responsibility:

- engine API payload validation
- executable payload conversion
- fork-aware header normalization

Expected changes:

- Uzen-aware `parentBeaconBlockRoot` normalization
- Uzen-aware empty `requestsHash` handling
- blob-tx rejection in payload validation/building

### tx admission / txpool / RPC ingress

Responsibility:

- reject blob transactions once `Uzen` is active before they can be pooled or broadcast as valid Uzen-era txs

Expected changes:

- add explicit Uzen-era tx-type validation guard
- preserve pre-Uzen behavior

### `core/vm` plus a dedicated zk-gas package

Responsibility:

- per-opcode and per-precompile metering
- overflow-safe accounting
- fork-gated activation at `Uzen`

Expected changes:

- execution hooks or equivalent instrumentation points
- dedicated data tables and accounting helpers
- a dedicated error/outcome for zk-gas limit breach

### `core/state_processor`

Responsibility:

- sequential block execution
- tx-level rollback and block-level continuation/termination semantics

Expected changes:

- integrate zk-gas outcome into the transaction loop
- drop offending tx on zk-gas breach
- stop processing remaining txs
- retain prior successful receipts/state transitions

## Data Flow

### Fork activation

1. Header timestamp enters validation or execution.
2. taiko-geth checks `config.IsUzen(header.Time)`.
3. Uzen-only payload rules and zk-gas enforcement activate from that point onward.

### Payload handling

1. Engine API receives payload.
2. If `Uzen` is active:
   - missing `parentBeaconBlockRoot` is normalized to zero where required
   - `requestsHash` is normalized to the empty requests hash
   - blob tx presence is rejected
3. Payload converts/imports under Osaka-era expectations plus Taiko Uzen overrides.

### Block execution

1. Block processor starts with `block_zk_gas_used = 0`.
2. Each tx gets a fresh `tx_zk_gas_used = 0`.
3. Opcode and precompile execution feed the meter during tx execution.
4. If zk-gas accounting exceeds the block limit:
   - halt execution immediately
   - revert the current tx completely
   - stop processing all remaining txs
5. Otherwise, commit `tx_zk_gas_used` into `block_zk_gas_used` and continue.

## Error Handling

### Blob transactions

Post-Uzen blob txs should fail deterministically with an explicit Taiko/Uzen-era validation error rather than being tolerated and merely skipped later.

Pre-Uzen behavior remains unchanged.

### Parent beacon block root

Normalization is only allowed where Taiko payload handling needs it at or after `Uzen`. It must not silently rewrite pre-Uzen payload semantics.

### Requests hash

At or after `Uzen`, the empty requests hash is authoritative for Taiko payload behavior. The design does not rely on request reconstruction to discover the value dynamically.

### zk-gas limit breach

The processor must treat zk-gas breach as its own outcome:

- not a normal receipt-producing revert
- not an import-fatal block error
- a tx-discard + block-truncate condition

### Overflow

Any unsigned-64 overflow in zk-gas arithmetic is treated as a limit breach and follows the same tx-discard + block-truncate behavior.

## Testing Strategy

The test scope is behavior parity only.

### Config and fork activation

- `UzenTime` is recognized as a Taiko timestamp fork
- Alethia-targeted config/genesis enables `Uzen` correctly
- future networks can enable Uzen through config without code-path changes

### Engine and payload behavior

- pre-Uzen payload behavior is unchanged
- post-Uzen missing `parentBeaconBlockRoot` normalizes as expected
- post-Uzen `requestsHash` uses the empty requests hash
- post-Uzen blob tx payloads are rejected

### Tx admission

- blob txs are accepted before `Uzen`
- blob txs are rejected after `Uzen`
- local block building does not include blob txs after `Uzen`

### zk-gas metering

- opcode metering charges according to multiplier table
- precompile metering charges separately
- spawn opcodes use fixed estimates when a child frame/precompile path is taken
- arithmetic overflow is treated as limit breach

### Block processing semantics

- successful txs before the offending tx remain in the block
- offending tx is fully discarded
- remaining txs are skipped
- anchor/system txs are metered too
- ordinary EVM reverts still behave normally and do not trigger block truncation

## Implementation Boundaries

This spec deliberately stops short of a task-by-task implementation plan.

The implementation plan should decompose the work into at least these tracks:

- chain config and genesis activation
- engine/payload normalization and validation
- tx admission and local builder blob rejection
- zk-gas metering internals
- state processor integration
- parity-focused tests

## Open Decisions Resolved By This Spec

- `Uzen` is part of the existing Taiko fork family, not a one-off Alethia hack.
- `Uzen` activates by time, like `Shasta`.
- zk gas is part of the `Uzen` fork, not a separate rollout.
- The target is runtime parity (`A` scope), not full operator/docs parity.
- The implementation should follow existing Taiko fork patterns in taiko-geth.
