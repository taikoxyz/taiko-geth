# Design: Rename `taiko.internal-shasta-time` to `taiko.internal-uzen-time`

## Summary

Rename the Taiko internal network CLI override flag from `taiko.internal-shasta-time` to `taiko.internal-uzen-time` and make it a strict breaking change. The old flag name and its environment variable alias will be removed rather than preserved as deprecated compatibility aliases.

## Context

The current CLI flag is defined as `taiko.internal-shasta-time`, but the repository already has a separate Uzen fork concept and an `InternalUzenTime` genesis override in [`core/taiko_genesis.go`](../../../core/taiko_genesis.go). Keeping a Shasta-named CLI entry point for a Uzen override is misleading for operators and leaves the CLI surface out of sync with the underlying fork model.

## Goals

- Expose only `--taiko.internal-uzen-time` on the CLI.
- Rename the matching environment variable to `TAIKO_INTERNAL_UZEN_TIME`.
- Route the CLI override to `core.InternalUzenTime`.
- Remove in-repo references that still advertise the old flag or env var.

## Non-Goals

- Preserve backward compatibility for `--taiko.internal-shasta-time`.
- Add deprecation warnings or migration shims.
- Refactor unrelated Shasta or Uzen genesis constants.
- Change fork activation behavior outside the internal-network Uzen override path.

## Chosen Approach

Perform a strict end-to-end rename across the CLI definition, registration, and assignment path.

This means:

- Rename the flag symbol in `cmd/utils/taiko_flags.go` to Uzen terminology.
- Change the public flag name from `taiko.internal-shasta-time` to `taiko.internal-uzen-time`.
- Change the help text to describe the Uzen override.
- Rename the environment variable from `TAIKO_INTERNAL_SHASTA_TIME` to `TAIKO_INTERNAL_UZEN_TIME`.
- Update `cmd/geth/main.go` to register the renamed flag.
- Update `cmd/utils/flags.go` so the parsed value is assigned to `core.InternalUzenTime`.

## Data Flow

After the change, the override path will be:

`--taiko.internal-uzen-time` or `TAIKO_INTERNAL_UZEN_TIME`
-> `TaikoInternalUzenTimeFlag`
-> `ctx.Uint64(...)`
-> `core.InternalUzenTime`
-> `core.TaikoGenesisBlock(cfg.NetworkId)`
-> internal-network `chainConfig.UzenTime`, `CancunTime`, `PragueTime`, and `OsakaTime`

The existing `core.InternalShastaTime` variable will remain unless it becomes unused as a direct result of the rename. Removing or refactoring other genesis constants is out of scope for this change.

## Error Handling

No special error handling will be added. The rename is intentionally breaking, so older invocations using `--taiko.internal-shasta-time` or `TAIKO_INTERNAL_SHASTA_TIME` should fail through the existing CLI parsing behavior.

## Verification

Verification should stay proportionate to the scope of the change:

- Search the repository to confirm there are no remaining in-repo references to `taiko.internal-shasta-time` or `TAIKO_INTERNAL_SHASTA_TIME`.
- Build `geth` successfully.
- Check `geth --help` output to confirm only `taiko.internal-uzen-time` is exposed.

If an existing CLI flag test covers this path, update it. If there is no existing test coverage for this flag, do not introduce a new test harness just for this rename.

## Risks

- External scripts or operator runbooks that still use the old flag or environment variable will break immediately after upgrade.
- A partial rename that updates the flag string but not the assignment target would silently misconfigure the wrong genesis override. The implementation must update both the CLI surface and the `core.InternalUzenTime` wiring.
