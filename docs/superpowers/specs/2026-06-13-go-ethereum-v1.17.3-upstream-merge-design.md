# Design: go-ethereum v1.17.3 upstream merge

- **Date:** 2026-06-13
- **Status:** Approved
- **Reference precedent:** PR #541 (`feat(repo): go-ethereum v1.17.2 upstream merge`)

## 1. Goal & scope

Re-base taiko-geth from go-ethereum **v1.17.2 → v1.17.3** with a **mechanical,
behavior-preserving merge**:

- Resolve every conflict so Taiko's current behavior is preserved exactly.
- Make it build (`go build ./...`) and pass targeted tests.
- Bump the geth-base version string to `1.17.3`.

New upstream features (e.g. EIP-7981 wiring) are **not adopted** in this PR — any
feature integration is deferred to follow-up PRs. This mirrors PR #541's workflow
and PR shape.

> **Note (discovered during planning, via merge dry-run):** v1.17.3 includes a
> significant upstream EVM gas refactor — gas became a 2-D vector
> `GasBudget{RegularGas, StateGas}` (#34691 "turn gas into a vector", #34712
> "introduce gas budget") plus a uint256 `core.Message` (#34934). Adapting Taiko's
> ZK-gas layer and anchor logic to this new API is **required to preserve
> behavior** and is therefore in-scope — this is adaptation forced by upstream, not
> feature adoption. It makes the merge consensus-sensitive rather than trivial.
> See §5. Resolution of the affected files is gated on a human review checkpoint.

### Pinned commits (use SHAs, not the local tags)

- go-ethereum **v1.17.2** = `be4dc0c4be2fe316dbdd0a73e48421f64978232f`
- go-ethereum **v1.17.3** = `117e067f0f0bae1a17082321f224dedb6765b10f`

> ⚠️ The local git tags `v1.17.3` / `v1.17.4` are **bogus** — they point to
> unrelated commits, not the upstream releases. Always merge by SHA. Upstream's
> latest release is genuinely v1.17.3 (no v1.17.4 exists upstream).

### Delta being merged

138 commits, 312 files (`+22,715 / -14,142`). ~22 files carry both upstream
changes and `CHANGE(taiko):` markers — the conflict surface.

## 2. Merge mechanics (Approach A — graft base + merge)

PR #541 was **squash-merged**, so git's merge-base between the `taiko` branch and
upstream is actually **v1.15.5**, not v1.17.2. A naive `git merge v1.17.3` would
replay ~1,200 commits and re-conflict everything already resolved in #541. To
avoid that, re-establish v1.17.2 as an ancestor first:

```bash
# 1. Working branch off taiko
git checkout -b feat/go-ethereum-v1.17.3-upstream-merge

# 2. Graft the base: record v1.17.2 as an ancestor WITHOUT changing any files
#    (taiko already contains v1.17.2's content, so -s ours keeps our tree).
git merge -s ours be4dc0c4be2fe316dbdd0a73e48421f64978232f \
  -m "chore: record go-ethereum v1.17.2 as merge base"

# 3. Merge v1.17.3 — git now diffs only the true v1.17.2->v1.17.3 delta.
git merge 117e067f0f0bae1a17082321f224dedb6765b10f
```

After step 3 git presents only the ~22-file delta, with full 3-way context for
high-quality conflict resolution. The net PR diff equals the v1.17.2→v1.17.3
changes as applied to Taiko (plus our resolutions and the version bump).

## 3. Conflict resolution

The merge dry-run (graft + `git merge v1.17.3`) produced **7 conflicted files** —
the other ~15 `CHANGE(taiko)` candidates auto-merged cleanly. Resolve each **by
hand**, preserving every `CHANGE(taiko):` block while taking upstream's changes,
checked against the upstream commit that drove the conflict.

**Mechanical / low-risk (3):**
- `appveyor.yml` — upstream **deleted** it (#34720); Taiko had edits → accept the
  deletion (Taiko uses gitea/github workflows).
- `beacon/engine/gen_ed.go` — generated file; regenerate via `go generate` rather
  than hand-merge.
- `beacon/engine/types.go` (1 hunk) — keep upstream's `slotNumber,omitempty` tag
  **and** Taiko's `ExecutableData` additions (`TxHash`, `WithdrawalsHash`,
  `HeaderDifficulty`, `TaikoBlock`, `HeaderDifficultyOrZero`).
- `params/config.go` (1 hunk) — keep Taiko's `IsOntake`/`IsPacaya`/`IsShasta`/
  `IsUnzen` helpers **and** upstream's `IsVerkleGenesis`→`IsUBTGenesis` rename.

**Consensus-critical — gated on human review (3):**
- `core/state_transition.go` (3 hunks) — upstream uint256 `Message`; re-apply
  Taiko's `IsAnchor` field, anchor balance-skip, and anchor gas-refund guard
  against the new uint256 types.
- `core/vm/evm.go` (10 hunks) — every Call-family signature went
  `uint64`→`GasBudget`; re-apply `markPending{Call,Create}Spawn`, the
  `gasBeforePrecompile`/`ChargePrecompile` zk-gas charging, and `zkGasErr`/
  `zkGasTracker` fields, adopting upstream's new `RunPrecompiledContract(..., gas
  GasBudget, ..., rules)` signature. Zk-gas helpers consume `.RegularGas`.
- `core/vm/interpreter.go` (2 hunks) — keep Taiko's `zkGasTracker` OOG handling,
  using upstream's `contract.Gas.RegularGas`/`dynamicCost.RegularGas`.

After resolving, a **compile-fix pass** is expected (bounded to `core/vm` + `core`)
where Taiko's zk-gas helpers (`precompileZkGasUsed`, `ZkGasStepTracker.Begin/
FinishAndCharge`) are adapted to read `.RegularGas` from `GasBudget`.

## 4. Dependencies & generated code

- `go.mod` / `go.sum`: dependency `karalabe/hid` → `ethereum/hid` rename, plus
  `x/crypto`, `x/sync`, `x/text`, `x/tools`, otel exporters, and grpc-gateway
  version bumps. Run `go mod tidy` + `go mod verify`.
- If upstream regenerated any bindings/protobuf within the delta, re-run the
  relevant `go generate` and commit the output.

## 5. Consensus-impact review (flagged for review)

Called out explicitly in the PR body for protocol-team awareness. Confirm Taiko's
behavior is unchanged unless its chain/fork config opts in:

- **🔴 Gas-vector / `GasBudget` refactor (#34691, #34712) — headline risk.**
  Upstream replaced EVM `gas uint64` with `GasBudget{RegularGas, StateGas}` across
  the call API, interpreter, and precompiles. Taiko's ZK-gas layer
  (`zkGasTracker`, `zkGasErr`, `ChargePrecompile`, `precompileZkGasUsed`) and
  anchor logic must be re-expressed on top of it. The contract: Taiko's existing
  gas semantics map to `GasBudget.RegularGas`; the new `StateGas` dimension is
  upstream-only and is **not** wired into Taiko's ZK-gas accounting in this PR.
  These resolutions are the human-review-checkpoint deliverable.
- **`core.Message` → uint256 (#34934)** — `state_transition` / `state_processor`;
  verify anchor-tx handling and the anchor balance/gas-refund skip are preserved.
- **EIP-7981** (access-list gas cost increase, #34755) — gated behind an upstream
  fork; confirm Taiko's chain config does not auto-activate it.
- **catalyst reorg-to-parent** + **`eth_call` header block-overrides** — verify
  Taiko payload-building / API paths are unaffected.

## 6. Version bump

`version/version.go`: `Patch = 2` → `Patch = 3` (geth-base version; `Meta` stays
`"stable"`). Taiko's own 2.x release version is bumped separately by a release PR
and is out of scope here.

## 7. Verification

- `go build ./...` is green.
- Targeted test packages pass: `consensus/taiko`, `miner`, `core`, `core/vm`,
  `eth/catalyst`, `internal/ethapi`.
- Full test matrix is left to CI.

## 8. PR & cleanup

- **Strip `docs/superpowers/`** before opening the PR (only code ships;
  consistent with #541, which removed its design/plan docs before the PR).
- PR title: `feat(repo): go-ethereum v1.17.3 upstream merge`.
- PR body summarizes: the delta, conflicts resolved, the synthetic graft commit
  (one-line reviewer note), and the consensus-impact notes from §5.
- **Landing recommendation:** land as a **real merge commit** so v1.17.3 becomes a
  permanent ancestor and the next bump has a correct base. If the team's
  convention is squash (as #541 used), that is acceptable — the cheap graft (§2)
  is simply re-applied at the next upstream merge.

## 9. Risks

- The synthetic graft commit needs a one-line reviewer note (covered in PR body).
- Some upstream tests may be flaky / environment-dependent — the targeted-test bar
  avoids that noise while still covering the high-risk packages.
- All work is isolated on `feat/go-ethereum-v1.17.3-upstream-merge`; the `taiko`
  branch is untouched until merge.
