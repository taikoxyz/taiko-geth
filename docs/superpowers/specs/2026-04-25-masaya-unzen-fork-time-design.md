# Masaya Unzen Fork Time Activation Design

## Summary

Set the Masaya devnet's Unzen fork activation time to `2026-05-07 13:00:00 UTC` (Unix `1778158800`), replacing the current "never activate" sentinel `math.MaxUint64`.

## Goal

Activate the Unzen fork on the Masaya shared devnet at the agreed time so that Unzen-gated behavior (zk-gas metering, canonical Unzen header fields, Unzen Engine API V2 allowances, Unzen transaction/body validation, and the implied Cancun/Prague/Osaka enablement) becomes live on Masaya.

## Non-Goals

- Activating Unzen on Mainnet, Hoodi, or the internal devnet. Their `*UzenTime` constants remain unchanged.
- Renaming `Uzen` to `Unzen` in code. That rename is tracked separately in `docs/superpowers/specs/2026-04-24-unzen-fork-rename-design.md` and is not blocked by this change. If the rename lands first, this same value will live on the renamed `MasayaUnzenTime` symbol.
- Changing the on-chain genesis allocation file `core/taiko_genesis/masaya.json`.

## Background

`core/taiko_genesis.go` defines per-network `*UzenTime` constants. For each non-devnet network, the file applies a conditional: when the value differs from `math.MaxUint64`, it also sets `CancunTime`, `PragueTime`, and `OsakaTime` to the same timestamp. Masaya currently has the sentinel value, so Unzen is effectively disabled. Updating the single constant is sufficient because the existing cascade at lines 69-72 will pick it up.

The `Uzen` spelling in identifiers reflects a typo that is being corrected via a separate spec. This change uses the current `Uzen` spelling so it can land independently of, and in any order with, that rename.

## Design

### Change

In `core/taiko_genesis.go`:

```go
// before
MasayaUzenTime  uint64 = math.MaxUint64
// after
MasayaUzenTime  uint64 = 1_778_158_800 // 2026-05-07 13:00:00 UTC
```

### Why no other edits are needed

- The block at `core/taiko_genesis.go:69-72` already gates the `CancunTime` / `PragueTime` / `OsakaTime` assignments on `MasayaUzenTime != math.MaxUint64`. Updating the constant flips that branch on automatically.
- `core/taiko_genesis/masaya.json` is allocs only and contains no fork-time fields.
- No CLI flag (`--taiko.devnet-uzen-time`) or env var path is involved for Masaya — Masaya does not read the devnet override.
- Mainnet, Hoodi, and the internal devnet keep their existing values.

## Verification

- `go build ./...` succeeds.
- `go test ./core/...` and `go test ./consensus/taiko/...` pass.
- Spot-check via `gofmt -l core/taiko_genesis.go` to confirm formatting.
- Manual sanity check: at a Masaya block timestamp `>= 1778158800`, `chainConfig.IsUzen(ts)` returns true; below it, false.

## Rollout

1. Branch `feature/masaya-unzen-fork-time` off `taiko`.
2. Apply the one-line edit and run the verification commands locally.
3. Commit with a `feat(core):` or `chore(core):` message referencing the Masaya activation time.
4. Open a PR targeting `taiko`.

## Risks

- **Wrong timestamp constant:** mitigated by computing `1_778_158_800` from `date -u -j -f "%Y-%m-%d %H:%M:%S" "2026-05-07 13:00:00" "+%s"` and embedding the human-readable comment next to the constant.
- **Premature activation if a node's clock is skewed:** standard L2 risk; no change here.
- **Coordination with the Uzen→Unzen rename:** both edits touch the same constant. Whichever lands second performs a trivial textual rebase (rename the symbol or update the value, depending on order).
