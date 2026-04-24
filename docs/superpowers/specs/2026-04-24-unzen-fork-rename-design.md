# Unzen Fork Rename Design

## Context

The Taiko fork name is currently spelled `Uzen` throughout the codebase. The intended fork name is `Unzen`. The typo appears in internal Go identifiers, exported Go symbols, serialized chain configuration, CLI flags, environment variables, filenames, tests, comments, logs, and error strings.

The selected approach is a hard rename with no backward-compatible aliases. Existing users of the typo must update their configs, scripts, and downstream Go references.

## Goals

- Make `Unzen` the only canonical fork name in code and user-facing text.
- Rename public serialized and CLI surfaces from `uzen` to `unzen`.
- Preserve all consensus and execution behavior exactly.
- Leave no meaningful `Uzen`, `uzen`, or `UZEN` references in source files after the rename.

## Non-Goals

- Do not add compatibility shims for the old typo.
- Do not change fork activation times, zk-gas parameters, header rules, or Engine API behavior.
- Do not refactor unrelated Taiko or upstream geth code.

## Public Surface Changes

The hard rename changes public names intentionally:

- `params.ChainConfig.UzenTime` becomes `params.ChainConfig.UnzenTime`.
- The chain config JSON key `uzenTime` becomes `unzenTime`.
- `(*params.ChainConfig).IsUzen` becomes `IsUnzen`.
- CLI flag `--taiko.devnet-uzen-time` becomes `--taiko.devnet-unzen-time`.
- Environment variable `TAIKO_DEVNET_UZEN_TIME` becomes `TAIKO_DEVNET_UNZEN_TIME`.
- Genesis constants such as `DevnetUzenTime` become `DevnetUnzenTime`.
- VM schedule symbol `UzenZkGasSchedule` becomes `UnzenZkGasSchedule`.

Existing configs and automation that still use the old typo will fail until updated.

## Implementation Design

Rename the fork name mechanically but review each changed site for context:

- Update `params/config.go` to use `UnzenTime`, `json:"unzenTime,omitempty"`, `IsUnzen`, and banner output `Unzen`.
- Update Taiko genesis setup in `core/taiko_genesis.go` to use `*UnzenTime` constants and assign `chainConfig.UnzenTime`.
- Update CLI registration and config application in `cmd/geth/main.go`, `cmd/utils/taiko_flags.go`, and `cmd/utils/flags.go`.
- Update execution, consensus, miner, and catalyst checks from `IsUzen` to `IsUnzen`.
- Update zk-gas schedule references from `UzenZkGasSchedule` to `UnzenZkGasSchedule`.
- Rename files containing `uzen` in their names to `unzen`, including test files and the zk-gas schedule file.
- Update comments, log messages, error strings, and test names to use `Unzen`.

All behavior tied to the fork remains unchanged. The rename must not alter zk-gas accounting, transaction truncation behavior, header difficulty semantics, blob transaction rejection, or Engine API version allowances.

## Data Flow

The canonical fork activation time flows from genesis defaults or CLI override into `params.ChainConfig.UnzenTime`. Runtime checks call `IsUnzen(timestamp)` to decide whether Unzen-specific behavior is active. That behavior then enables zk-gas metering, canonical Unzen header fields, Unzen Engine API V2 allowances, and Unzen transaction/body validation.

Only names on this flow change; the values and decision points remain identical.

## Error Handling And Migration

No compatibility layer will translate old names. Invalid or stale usage should fail through existing mechanisms:

- JSON configs using `uzenTime` will no longer populate the fork time.
- CLI invocations using `--taiko.devnet-uzen-time` will be rejected as unknown flags.
- Environments using `TAIKO_DEVNET_UZEN_TIME` will no longer override the devnet fork time.
- Downstream Go code using exported `Uzen` names will fail to compile.

This is intentional under the hard-rename decision.

## Testing And Verification

After implementation:

- Run `gofmt` on touched Go files.
- Run `rg "Uzen|uzen|UZEN"` and review all remaining matches. Ignore only confirmed false positives such as generated binary or base64 data.
- Run focused tests for touched packages:
  - `go test ./params`
  - `go test ./core`
  - `go test ./core/vm`
  - `go test ./consensus/taiko`
  - `go test ./eth/catalyst`
  - `go test ./miner`
- Run broader tests if focused failures point to shared behavior outside those packages.

## Risks

- The hard rename breaks existing configs, scripts, and downstream Go references that still use `Uzen`.
- Missing a source occurrence could leave inconsistent naming in logs, tests, or documentation.
- Generated or fixture files may need updates if they serialize `uzenTime`.

These risks are acceptable for this change because the requested outcome is to replace the typo everywhere without compatibility aliases.
