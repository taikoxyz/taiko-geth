# Uzen Fork Design Spec for taiko-geth

## Overview

Uzen is the next Taiko protocol upgrade after Shasta. It is timestamp-activated and introduces:

1. **ZK Gas Metering** — per-opcode/precompile weighted gas accounting with a block-level limit
2. **EVM upgrade to Osaka** — activates Cancun + Prague + Osaka semantics (BLS precompiles, EIP-7702, EIP-7939 CLZ)
3. **Blob transaction rejection** — already exists, formally enforced in consensus
4. **Header difficulty repurposed** — carries finalized block zk gas for hash stability
5. **Parent beacon block root normalization** — defaults to zero hash
6. **Requests hash** — set to `EmptyRequestsHash` for Uzen blocks

The implementation must produce byte-identical block hashes to alethia-reth for the same input transactions.

## Reference Implementation

- **alethia-reth**: commits `dd2d4b5..4e3c880` (after base `2415860`)
- **ZK Gas Spec**: https://github.com/taikoxyz/taiko-mono/blob/main/packages/protocol/docs/zk_gas_spec.md
- **Client**: taiko-client-rs in taiko-mono

## 1. Fork Configuration

### Files Modified
- `params/config.go` — add `UzenTime` field and `IsUzen()` method
- `params/taiko_config.go` — set `UzenTime` in `TaikoChainConfig`
- `core/taiko_genesis.go` — add activation timestamps per network

### Design

Add to `ChainConfig`:
```go
UzenTime *uint64 `json:"uzenTime,omitempty"` // Uzen switch time (nil = no fork, 0 = already activated)
```

Add method:
```go
// CHANGE(taiko): IsUzen returns whether time is either equal to the Uzen fork time or greater.
func (c *ChainConfig) IsUzen(time uint64) bool {
    return isTimestampForked(c.UzenTime, time)
}
```

When `IsUzen` is true, `CancunTime`, `PragueTime`, and `OsakaTime` must also be active (set equal to `UzenTime` in genesis config). This leverages go-ethereum's existing fork gating for all Cancun/Prague/Osaka EVM features.

### Activation Timestamps
```go
InternalUzenTime uint64 = 0           // devnet: active from genesis
MasayaUzenTime   uint64 = 0           // placeholder: Never (use math.MaxUint64 or nil)
MainnetUzenTime  // nil (not yet scheduled)
HoodiUzenTime    // nil (not yet scheduled)
```

Note: For networks where Uzen is not active, `UzenTime` is left as `nil`. For devnet, it's `0`.

## 2. ZK Gas Meter

### New Files
- `core/vm/taiko_zk_gas.go` — meter and schedule types
- `core/vm/taiko_zk_gas_uzen.go` — Uzen-specific multiplier tables

### ZkGasSchedule

```go
// CHANGE(taiko): ZkGasSchedule defines consensus-owned zk gas parameters for a Taiko fork.
type ZkGasSchedule struct {
    BlockLimit            uint64
    OpcodeMultipliers     [256]uint16
    PrecompileMultipliers [256]uint16
    SpawnEstimates        SpawnEstimates
}

type SpawnEstimates struct {
    Call         uint64 // 12,500
    CallCode     uint64 // 12,500
    DelegateCall uint64 // 3,500
    StaticCall   uint64 // 3,500
    Create       uint64 // 37,000
    Create2      uint64 // 44,500
}
```

### ZkGasMeter

```go
// CHANGE(taiko): ZkGasMeter provides checked zk gas accounting for a single block execution.
type ZkGasMeter struct {
    schedule        *ZkGasSchedule
    blockZkGasUsed  uint64
    txZkGasUsed     uint64
}
```

Methods:
- `NewZkGasMeter(schedule) *ZkGasMeter`
- `ChargeOpcode(opcode byte, rawGas uint64) error` — charges `rawGas * multiplier`, returns error on overflow/exceeded
- `ChargePrecompile(addrLowByte byte, gasUsed uint64) error` — charges `gasUsed * multiplier`
- `CommitTransaction() error` — promotes tx gas to block total, returns error if exceeds limit
- `ResetTransaction()` — discards in-flight tx gas
- `BlockZkGasUsed() uint64` — returns finalized block total
- `TxZkGasUsed() uint64` — returns in-flight tx total

### Overflow/Limit Behavior

All arithmetic uses `uint64`. Overflow or exceeding `BlockLimit` returns `ErrZkGasLimitExceeded`. The caller (state processor) must:
1. Abort the offending transaction (revert state)
2. Skip all remaining transactions
3. Keep previously committed transactions in the block

### Multiplier Tables

The `taiko_zk_gas_uzen.go` file contains the full opcode and precompile multiplier tables from the spec. Unlisted opcodes use `math.MaxUint16` as failsafe (any execution of unlisted opcode with non-zero gas will immediately overflow and halt).

Key multipliers (sample):
- `MULMOD (0x09)`: 152
- `KECCAK256 (0x20)`: 85
- `CALL (0xf1)`: 25
- `ADD (0x01)`: 12
- `SLOAD (0x54)`: 5
- Precompile `modexp (0x05)`: 1363
- Precompile `ecrecover (0x01)`: 81
- Precompile `sha256 (0x02)`: 10

## 3. EVM Integration

### Files Modified
- `core/vm/interpreter.go` — add zk gas charging in opcode loop
- `core/vm/evm.go` — add spawn flag, precompile charging
- `core/vm/interpreter_types.go` or similar — add `ZkGasMeter` to `Config`

### vm.Config Extension

```go
// CHANGE(taiko): ZkGasMeter enables per-opcode zk gas metering when non-nil (Uzen fork).
ZkGasMeter *ZkGasMeter
```

### Interpreter Loop Integration

In `interpreter.go`, after each opcode executes:

```go
// CHANGE(taiko): charge zk gas after opcode execution
if in.cfg.ZkGasMeter != nil {
    if isSpawnOpcode(op) {
        if in.evm.childSpawned {
            if err := in.cfg.ZkGasMeter.ChargeOpcode(op, spawnEstimate(in.cfg.ZkGasMeter, op)); err != nil {
                return nil, err // halt with zk gas exceeded
            }
            in.evm.childSpawned = false
        } else {
            if err := in.cfg.ZkGasMeter.ChargeOpcode(op, gasCost); err != nil {
                return nil, err
            }
        }
    } else {
        if err := in.cfg.ZkGasMeter.ChargeOpcode(op, gasCost); err != nil {
            return nil, err
        }
    }
}
```

Where `isSpawnOpcode` checks `op` against `CALL, CALLCODE, DELEGATECALL, STATICCALL, CREATE, CREATE2`.

### EVM Child-Spawn Detection

Add to `EVM` struct:
```go
childSpawned bool // CHANGE(taiko): set when Call/Create passes pre-checks
```

In each of `evm.Call()`, `evm.CallCode()`, `evm.DelegateCall()`, `evm.StaticCall()`:
- After passing depth check and balance check (right before actual execution): set `evm.childSpawned = true`

In `evm.Create()` / `evm.Create2()`:
- After passing depth check and balance check: set `evm.childSpawned = true`

For precompile calls within `evm.Call()` (and variants):
- After precompile executes successfully: call `ZkGasMeter.ChargePrecompile(addr[19], gasUsed)`
- Also set `evm.childSpawned = true` (precompile dispatch counts as spawned)

### Error Handling

When `ChargeOpcode` or `ChargePrecompile` returns error:
- The interpreter halts execution with `ErrZkGasLimitExceeded`
- This propagates up as a transaction execution failure
- The state processor handles it at the block level (abort tx, skip rest)

## 4. Block Execution / State Processor

### Files Modified
- `core/state_processor.go` — integrate zk gas meter lifecycle
- `miner/taiko_worker.go` — same for block building path

### Execution Flow

```
Block Start:
  if IsUzen(header.Time):
    meter = NewZkGasMeter(UzenSchedule)
    vmConfig.ZkGasMeter = meter

For each transaction:
  if meter != nil && meter is exhausted:
    skip this and all remaining transactions
    break

  meter.ResetTransaction()  // clear in-flight
  execute transaction with vmConfig
  
  if execution failed due to ErrZkGasLimitExceeded:
    meter.ResetTransaction()  // discard the failed tx's zk gas
    mark block as zk-gas-exhausted
    skip all remaining transactions
    break
  
  if execution succeeded (even if EVM reverted):
    if meter.CommitTransaction() returns error:
      // overflow during commit (shouldn't happen if ChargeOpcode already checks)
      skip remaining
      break
    include transaction in block

Block Finalize:
  if IsUzen:
    header.Difficulty = meter.BlockZkGasUsed()
    header.RequestsHash = &EmptyRequestsHash
    header.ParentBeaconRoot = &common.Hash{} (if not already set)
```

### Block Building (miner/taiko_worker.go)

Same logic as state processor. When building a block for `getPayload`:
- Initialize meter
- Stop including transactions when zk gas is exhausted
- Set header difficulty to finalized block zk gas

## 5. Consensus Validation

### Files Modified
- `consensus/taiko/consensus.go` — update difficulty validation

### Changes

In `verifyHeader()`:
```go
// CHANGE(taiko): Uzen repurposes difficulty for zk gas; only enforce zero before Uzen.
if !t.chainConfig.IsUzen(header.Time) {
    if header.Difficulty != nil && header.Difficulty.Cmp(common.Big0) != 0 {
        return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, common.Big0)
    }
}
```

On block import (after re-executing transactions):
- Compare recomputed `meter.BlockZkGasUsed()` against `header.Difficulty`
- If mismatch: reject block with `ZkGasDifficultyMismatch` error

Validate body doesn't extend past truncation:
- After execution, if zk gas was exhausted at transaction N, the body must contain exactly N transactions (not more)

## 6. Engine API

### Files Modified
- `beacon/engine/types.go` — add `HeaderDifficulty` to `ExecutableData`
- `beacon/engine/gen_ed.go` — regenerate JSON marshaling
- `eth/catalyst/api.go` — modify getPayload/newPayload handling

### ExecutableData Extension

```go
// CHANGE(taiko): Uzen header difficulty transported via sidecar for hash-stable round-trips.
HeaderDifficulty *big.Int `json:"headerDifficulty,omitempty"`
```

### getPayloadV2 Response

When Uzen is active for the built block:
- Set `blockValue = header.Difficulty` (the finalized block zk gas)
- This overwrites the normal fee-based blockValue

The client reads `blockValue` from the envelope and passes it back as `headerDifficulty` in newPayload.

### newPayloadV2 Handling

When receiving a payload with `HeaderDifficulty` set:
- Apply it as `header.Difficulty` during block construction
- After re-execution, validate the recomputed zk gas matches

### Parent Beacon Block Root

When Uzen is active:
- If `parentBeaconBlockRoot` is not provided in the payload: default to `common.Hash{}`
- This matches alethia-reth's `normalize_parent_beacon_block_root()` behavior

## 7. Header Field Assembly

### Uzen Block Header Requirements

| Field | Value |
|-------|-------|
| `Difficulty` | `U256(finalized_block_zk_gas)` |
| `RequestsHash` | `&EmptyRequestsHash` (sha256 of empty string) |
| `ParentBeaconRoot` | `&common.Hash{}` (zero, or provided value) |
| `BlobGasUsed` | `nil` (no blobs) |
| `ExcessBlobGas` | `nil` (no blobs) |
| `Nonce` | `0` (same as pre-Uzen) |
| `MixHash` | `prevRandao` from payload attributes |

### EVM Environment for Uzen Blocks

| Field | Value |
|-------|-------|
| `spec` | Osaka |
| `BlobExcessGasAndPrice` | `{ExcessBlobGas: 0, BlobGasPrice: 1}` (for EVM compatibility) |
| `Difficulty` | `header.Difficulty` (the zk gas value) |

Notes:
- **Critical (PREVRANDAO)**: go-ethereum only sets `BlockContext.Random` when `header.Difficulty == 0`. Since Uzen repurposes difficulty for zk gas (non-zero), `Random` would be nil, causing a panic on the `PREVRANDAO` opcode. We must set `Random = &header.MixDigest` when Uzen is active regardless of difficulty. In alethia-reth, `prevrandao` is always set from `header.mix_hash()`.
- **Critical (BlobBaseFee)**: go-ethereum only sets `BlobBaseFee` when `header.ExcessBlobGas != nil`. Since Uzen headers have no blob gas fields, we must explicitly set `BlobBaseFee = 1` in the EVM block context when Uzen is active. This is done in `core/evm.go`'s `NewEVMBlockContext()`. Without this, the `BLOBBASEFEE` opcode would return 0 instead of 1, causing state divergence from alethia-reth.
- The EVM `BlockContext.Difficulty` should be `header.Difficulty` (the zk gas value). In alethia-reth, `evm_env()` sets `difficulty: header.difficulty()` when Uzen is active. However, since Osaka-era EVM uses `PREVRANDAO` (not `DIFFICULTY`), and `DIFFICULTY` was removed post-merge, this field primarily flows through block context for informational purposes.

## 8. Transaction Filtering

### Blob Transaction Rejection

Already handled in `miner/taiko_worker.go`:
```go
if tx.Type() == types.BlobTxType {
    log.Debug("Skip a blob transaction", "hash", tx.Hash())
    continue
}
```

For consensus validation on import: verify no blob transactions exist in the block body when Uzen is active. This is a new check in the consensus engine.

## 9. File Summary

### New Files (following `taiko_*.go` convention)
| File | Purpose |
|------|---------|
| `core/vm/taiko_zk_gas.go` | ZkGasMeter, ZkGasSchedule types and methods |
| `core/vm/taiko_zk_gas_uzen.go` | Uzen multiplier tables and schedule constant |
| `core/vm/taiko_zk_gas_test.go` | Unit tests for meter arithmetic |

### Modified Files (with `CHANGE(taiko):` comments)
| File | Changes |
|------|---------|
| `params/config.go` | Add `UzenTime`, `IsUzen()` |
| `params/taiko_config.go` | Set Uzen in TaikoChainConfig |
| `core/taiko_genesis.go` | Add Uzen timestamps per network |
| `core/vm/interpreter.go` | Add zk gas charging after opcode dispatch |
| `core/vm/evm.go` | Add `childSpawned` flag, precompile charging |
| `core/evm.go` | Set `BlobBaseFee=1` and `Difficulty` in block context for Uzen |
| `core/state_processor.go` | Meter lifecycle, tx abort/skip logic |
| `miner/taiko_worker.go` | Same meter logic for block building |
| `consensus/taiko/consensus.go` | Allow non-zero difficulty, validate zk gas |
| `beacon/engine/types.go` | Add `HeaderDifficulty` field |
| `eth/catalyst/api.go` | blockValue override, headerDifficulty handling |

## 10. Testing Strategy

1. **Unit tests** (`core/vm/taiko_zk_gas_test.go`):
   - Meter arithmetic (overflow, limit boundary)
   - Multiplier table correctness (spot-check against spec)
   - Commit/reset semantics

2. **Integration tests** (`consensus/taiko/consensus_test.go`):
   - Block with zk gas exceeding limit mid-transaction
   - Difficulty validation on import
   - Body truncation validation

3. **Cross-client verification**:
   - Process identical transactions through both taiko-geth and alethia-reth
   - Compare block hashes, state roots, and header fields
   - This is the ultimate correctness test

## 11. Critical Invariants

1. **Block hash identity**: For the same ordered transactions, taiko-geth and alethia-reth must produce identical block hashes
2. **Deterministic truncation**: ZK gas limit must truncate at the exact same transaction index
3. **Spawn estimate usage**: CALL/CREATE family opcodes use fixed spawn estimates when child execution occurs (depth/balance pre-checks pass), measured gasCost otherwise
4. **Precompile double-charge**: Precompile calls charge both the CALL opcode (spawn estimate × call multiplier) AND the precompile itself (gasUsed × precompile multiplier)
5. **Overflow halts immediately**: Any uint64 overflow in zk gas arithmetic halts execution of the current transaction
