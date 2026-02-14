# Repository Guidelines

## Project Structure & Module Organization
Taiko-geth extends go-ethereum, managed via the module declared in `go.mod`. Entry points live in `cmd/` (notably `cmd/geth`), while execution and consensus packages sit in `core/`, `consensus/`, and `eth/`, with Taiko forks under `beacon/` and `params/`. Builds land in `build/bin/` through `build/ci.go`, and regression suites plus fixtures reside in `tests/`; continue the `taiko_*.go` pattern and `CHANGE(taiko)` tags to keep upstream parity.

## Build, Test, and Development Commands
- `make geth` – compile the Taiko-flavored `geth` into `build/bin/`.
- `make all` – build every CLI in `cmd/` via `build/ci.go`.
- `make test` – execute unit and protocol tests.
- `make lint` – run the curated linters enforced in CI.
- `make fmt` – apply `gofmt -s` across the repository.
- `make devtools` – install generators such as `abigen`, `stringer`, and protobuf plugins.

## Coding Style & Naming Conventions
Keep all Go code `gofmt`-clean (tabs for indentation, trailing newline). Exported names use CamelCase; locals remain lowerCamel. Tag Taiko-only changes with inline `CHANGE(taiko): ...` comments and prefix new Taiko files with `taiko_`. Group imports as stdlib, third-party, then local packages; `goimports` helps maintain that order.

## Testing Guidelines
Write `_test.go` files beside the code they cover. Run focused suites with `go test ./consensus/...` or `go test ./tests -run Block`. Regenerate artifacts in `tests/gen_*.go` whenever fixtures change, and prefer deterministic cases under `tests/spec-tests/` when extending protocol coverage.

## Commit & Pull Request Guidelines
Commits follow Conventional Commit syntax (`type(scope): summary`) and typically cite the PR, e.g. `feat(consensus): introduce Shasta fork (#431)`. Squash fixups before pushing. PRs should describe protocol impact, link Taiko issues, capture validation steps for RPC changes, and update `docs/` or operator guidance when behavior shifts. Call out any security-relevant notes, especially if `SECURITY.md` needs updates.
When `core/taiko_genesis/` changes, the PR must print the corresponding chain ID and genesis hash.

## Security & Configuration Tips
Review `SECURITY.md` before reporting vulnerabilities. For configuration changes, include the flag or TOML snippet (`geth --config path/to/config.toml`) and note RPC exposure adjustments. Align Docker guidance with the repository `Dockerfile`, including required port mappings and volume mounts.
