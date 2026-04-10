# Repository Guidelines

## Project Structure & Module Organization
Taiko-geth extends go-ethereum, managed via the module declared in `go.mod`. Entry points live in `cmd/` (notably `cmd/geth`), while execution and consensus packages sit in `core/`, `consensus/`, and `eth/`, with Taiko forks under `beacon/` and `params/`. Builds land in `build/bin/` through `build/ci.go`, and regression suites plus fixtures reside in `tests/`; continue the `taiko_*.go` pattern and `CHANGE(taiko)` tags to keep upstream parity.

## Build, Test, and Development Commands
- `make geth` compiles the Taiko-flavored `geth` into `build/bin/`.
- `make all` builds every CLI in `cmd/` via `build/ci.go`.
- `make test` executes unit and protocol tests.
- `make lint` runs the curated linters enforced in CI.
- `make fmt` applies `gofmt -s` across the repository.
- `make devtools` installs generators such as `abigen`, `stringer`, and protobuf plugins.

## Coding Style & Naming Conventions
Keep all Go code `gofmt`-clean. Exported names use CamelCase; locals remain lowerCamel. Tag Taiko-only changes with inline `CHANGE(taiko): ...` comments and prefix new Taiko files with `taiko_`. Group imports as stdlib, third-party, then local packages.

## Testing Guidelines
Write `_test.go` files beside the code they cover. Run focused suites with `go test ./consensus/...` or `go test ./tests -run Block`. Regenerate artifacts in `tests/gen_*.go` whenever fixtures change, and prefer deterministic cases under `tests/spec-tests/` when extending protocol coverage.

## Commit & Pull Request Guidelines
Commits follow Conventional Commit syntax (`type(scope): summary`) and typically cite the PR, for example `feat(consensus): introduce Shasta fork (#431)`. Squash fixups before pushing. PRs should describe protocol impact, link Taiko issues, capture validation steps for RPC changes, and update `docs/` or operator guidance when behavior shifts. When `core/taiko_genesis/` changes, print the corresponding chain ID and genesis hash.

## Security & Configuration Tips
Review `SECURITY.md` before reporting vulnerabilities. For configuration changes, include the flag or TOML snippet (`geth --config path/to/config.toml`) and note RPC exposure adjustments. Align Docker guidance with the repository `Dockerfile`, including required port mappings and volume mounts.

## Agent Guidelines
- Keep changes minimal and focused. Only modify code directly related to the task at hand.
- Do not add, remove, or update dependencies unless the task explicitly requires it.
- Do not commit binaries or other build byproducts.

## Pre-Commit Checklist
Before every commit, run these checks and make sure they pass:

```sh
gofmt -w <modified files>
goimports -w <modified files>
make all
go run ./build/ci.go test
go run ./build/ci.go lint
go run ./build/ci.go check_generate
go run ./build/ci.go check_baddeps
```

During iteration, `go run ./build/ci.go test -short` is acceptable for faster feedback.

## Commit Message Format
Commit messages should be prefixed with the package(s) they modify, followed by a short lowercase description:

```text
<package(s)>: description
```

Examples:
- `core/vm: fix stack overflow in PUSH instruction`
- `eth, rpc: make trace configs optional`
- `cmd/geth: add new flag for sync mode`

## Pull Request Title Format
PR titles follow the same convention as commit messages:

```text
<list of modified paths>: description
```

Examples:
- `core/vm: fix stack overflow in PUSH instruction`
- `core, eth: add arena allocator support`
- `cmd/geth, internal/ethapi: refactor transaction args`
