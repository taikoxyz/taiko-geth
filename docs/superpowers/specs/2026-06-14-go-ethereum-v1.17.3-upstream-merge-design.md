# go-ethereum v1.17.3 upstream merge design

## Goal

Move `taiko-geth` from upstream go-ethereum v1.17.2 to upstream go-ethereum v1.17.3 on a fresh branch and open a new draft PR. The PR should preserve Taiko behavior while adopting the upstream release delta needed for v1.17.3.

## Branch and source of truth

- Start from current `origin/taiko`.
- Use a fresh branch: `codex/go-ethereum-v1.17.3-upstream-merge`.
- Use the upstream release commit `117e067f0f0bae1a17082321f224dedb6765b10f` from `/Users/davidcai/Workspace/go-ethereum` or the `upstream` remote.
- Do not rely on the local `v1.17.3` tag in `taiko-geth`; it points to a Taiko-side branch commit, not the upstream go-ethereum release.
- Treat the closed PR #574 branch as reference material only. The new PR head should be a fresh branch.

## Merge strategy

PR #541 brought `taiko-geth` to go-ethereum v1.17.2 but was squash-merged, so public history does not retain v1.17.2 as a real upstream ancestor. A naive merge of v1.17.3 would re-present already resolved history.

Use the same ancestry-recording pattern as the previous v1.17.3 attempt:

1. Record upstream v1.17.2 as an ancestor with an empty `ours` merge, changing no files.
2. Merge upstream v1.17.3 by commit hash.
3. Resolve only the true v1.17.2 to v1.17.3 conflict set.

This keeps the PR reviewable and makes the next upstream bump easier if the PR lands with merge commits preserved.

## Conflict policy

Conflict resolution should be behavior-preserving for Taiko customizations. Preserve:

- Taiko ZK-gas accounting and sticky error behavior.
- Anchor transaction handling, including balance-skip and refund behavior.
- Taiko-specific engine payload fields and generated engine API codecs.
- Taiko fork predicates and chain config behavior.
- Existing Unzen, Shasta, Pacaya, and Ontake semantics unless upstream API changes require mechanical adaptation.

Adopt upstream changes where they are release mechanics or required API compatibility. Avoid introducing new Taiko behavior beyond what is necessary to compile and preserve the current semantics.

## Consensus-sensitive areas

Review these paths carefully during conflict resolution:

- `core/state_transition.go`
- `core/vm/evm.go`
- `core/vm/interpreter.go`
- `miner/taiko_worker.go`
- `internal/ethapi/simulate.go`
- `beacon/engine/types.go`
- `beacon/engine/gen_ed.go`
- `params/config.go`

The v1.17.3 upstream delta includes the gas-vector refactor and `uint256` `core.Message` changes. Taiko gas semantics should map to upstream regular gas unless a current Taiko path already intentionally handles another dimension.

## Verification

Run verification scaled to this merge:

- `go build ./...`
- Focused tests for Taiko consensus and touched upstream APIs, including `consensus/taiko`, `core`, `core/vm`, `miner`, `eth/catalyst`, and `internal/ethapi`.
- Run `go generate` only for generated files affected by conflict resolution, such as engine API codec output.

Full `make test` can be left to CI unless local focused verification exposes an issue that needs deeper reproduction.

## PR shape

Open a draft PR against `taiko` with:

- A summary that this is a behavior-preserving v1.17.2 to v1.17.3 upstream merge.
- The exact upstream commit hash used.
- A short explanation of the ancestry-recording merge commit.
- A conflict-resolution list.
- Consensus-impact notes for ZK gas, anchor handling, and simulation/block-building decisions.
- Verification commands and results.

Before opening the PR, remove working design or plan documents from the PR branch if the final code diff should stay focused on the upstream merge.
