# go-ethereum v1.17.3 Upstream Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Open a fresh draft PR that moves `taiko-geth` from upstream go-ethereum v1.17.2 to upstream go-ethereum v1.17.3 while preserving Taiko behavior.

**Architecture:** Use a fresh `codex/` branch from `origin/taiko`, record upstream v1.17.2 as an ancestor with an empty `ours` merge, then merge the upstream v1.17.3 commit by hash. Resolve conflicts by preserving existing Taiko consensus behavior and adopting upstream API changes only where necessary.

**Tech Stack:** Go, git merge, go generate, go build, go test, GitHub CLI.

---

## File Structure

- Modify upstream merge files across the repository as produced by `git merge 117e067f0f0bae1a17082321f224dedb6765b10f`.
- Review and resolve consensus-sensitive conflicts in:
  - `core/state_transition.go`
  - `core/vm/evm.go`
  - `core/vm/interpreter.go`
  - `miner/taiko_worker.go`
  - `internal/ethapi/simulate.go`
  - `beacon/engine/types.go`
  - `beacon/engine/gen_ed.go`
  - `params/config.go`
- Remove the temporary planning docs before opening the final PR:
  - `docs/superpowers/specs/2026-06-14-go-ethereum-v1.17.3-upstream-merge-design.md`
  - `docs/superpowers/plans/2026-06-14-go-ethereum-v1.17.3-upstream-merge.md`

## Task 1: Confirm Baseline and Upstream Release Commits

**Files:**
- Inspect only: repository metadata

- [ ] **Step 1: Verify branch and cleanliness**

Run:

```bash
git status --short --branch
```

Expected: branch is `codex/go-ethereum-v1.17.3-upstream-merge`; no unstaged source edits except the committed design and plan checkpoint before execution.

- [ ] **Step 2: Verify upstream commit hashes without using local taiko-geth tags**

Run:

```bash
git ls-remote upstream refs/tags/v1.17.2 refs/tags/v1.17.3
git -C /Users/davidcai/Workspace/go-ethereum rev-parse v1.17.3
```

Expected:

```text
be4dc0c4be2fe316dbdd0a73e48421f64978232f refs/tags/v1.17.2
117e067f0f0bae1a17082321f224dedb6765b10f refs/tags/v1.17.3
117e067f0f0bae1a17082321f224dedb6765b10f
```

- [ ] **Step 3: Confirm the local tag ambiguity**

Run:

```bash
git show --no-patch --pretty=oneline v1.17.3
```

Expected: local `taiko-geth` tag does not point to `117e067f0f0bae1a17082321f224dedb6765b10f`; therefore all merge commands use commit hashes.

## Task 2: Record v1.17.2 as the Merge Base

**Files:**
- Modify: git history only; no tree files should change

- [ ] **Step 1: Create the empty ancestry merge**

Run:

```bash
git merge -s ours --no-ff be4dc0c4be2fe316dbdd0a73e48421f64978232f -m "chore: record go-ethereum v1.17.2 as merge base"
```

Expected: merge commit succeeds and reports no file changes.

- [ ] **Step 2: Verify the tree stayed unchanged**

Run:

```bash
git diff --stat HEAD^ HEAD
git status --short
```

Expected: `git diff --stat` is empty and `git status --short` is empty.

## Task 3: Merge go-ethereum v1.17.3

**Files:**
- Modify: files changed by upstream v1.17.3 release delta
- Resolve conflicts expected in:
  - `appveyor.yml`
  - `beacon/engine/gen_ed.go`
  - `beacon/engine/types.go`
  - `core/state_transition.go`
  - `core/vm/evm.go`
  - `core/vm/interpreter.go`
  - `params/config.go`

- [ ] **Step 1: Start the upstream merge by commit hash**

Run:

```bash
git merge --no-ff 117e067f0f0bae1a17082321f224dedb6765b10f -m "Merge go-ethereum v1.17.3 into taiko"
```

Expected: merge stops only on the true v1.17.2 to v1.17.3 conflict set.

- [ ] **Step 2: List conflicts**

Run:

```bash
git diff --name-only --diff-filter=U
```

Expected conflict list includes the files listed for this task. If extra files appear, resolve them under the same behavior-preserving policy and include them in the final PR notes.

- [ ] **Step 3: Resolve mechanical conflicts**

Resolution policy:

```text
appveyor.yml: accept upstream deletion.
beacon/engine/types.go: keep Taiko ExecutableData fields and upstream slotNumber field.
params/config.go: keep Taiko fork predicates and adopt upstream IsUBTGenesis naming.
beacon/engine/gen_ed.go: regenerate after types.go is resolved.
```

Use the closed PR #574 branch only as a reference:

```bash
git show origin/feat/go-ethereum-v1.17.3-upstream-merge:beacon/engine/types.go > /tmp/ref-types.go
git show origin/feat/go-ethereum-v1.17.3-upstream-merge:params/config.go > /tmp/ref-config.go
```

Expected: final files compile and retain Taiko fields/predicates.

- [ ] **Step 4: Resolve consensus-critical gas and EVM conflicts**

Resolution policy:

```text
core/state_transition.go: adapt Taiko anchor and refund logic to upstream uint256 Message fields and GasBudget API.
core/vm/evm.go: preserve Taiko ZK-gas call-path behavior while adapting to GasBudget.
core/vm/interpreter.go: preserve sticky ZK-gas error behavior on OOG and precompile paths.
```

Use #574 as reference for conflict decisions:

```bash
git show origin/feat/go-ethereum-v1.17.3-upstream-merge:core/state_transition.go > /tmp/ref-state_transition.go
git show origin/feat/go-ethereum-v1.17.3-upstream-merge:core/vm/evm.go > /tmp/ref-evm.go
git show origin/feat/go-ethereum-v1.17.3-upstream-merge:core/vm/interpreter.go > /tmp/ref-interpreter.go
```

Expected: Taiko ZK-gas tracker paths still charge against regular gas semantics and do not wire upstream state gas into Taiko ZK-gas accounting.

- [ ] **Step 5: Regenerate engine API codec output if needed**

Run:

```bash
go generate ./beacon/engine
```

Expected: generated engine files are updated consistently with `beacon/engine/types.go`.

- [ ] **Step 6: Complete the merge commit**

Run:

```bash
git status --short
git add appveyor.yml beacon/engine/gen_ed.go beacon/engine/types.go core/state_transition.go core/vm/evm.go core/vm/interpreter.go params/config.go
git diff --check --cached
git commit --no-edit
```

Expected: no unresolved conflicts, no whitespace errors, merge commit created.

## Task 4: Review Taiko Behavior Preservation

**Files:**
- Inspect and possibly modify:
  - `core/state_transition.go`
  - `core/vm/evm.go`
  - `core/vm/interpreter.go`
  - `miner/taiko_worker.go`
  - `internal/ethapi/simulate.go`
  - `core/vm/taiko_zk_gas_runtime.go`
  - `core/vm/taiko_zk_gas_runtime_test.go`

- [ ] **Step 1: Search for unresolved conflict markers and Taiko API drift**

Run:

```bash
rg -n "<<<<<<<|=======|>>>>>>>"
rg -n "FinalizeAndAssemble|GasBudget|StateGas|RegularGas|zkGas|IsAnchor|TaikoBlock|HeaderDifficulty|slotNumber" core beacon miner internal params
```

Expected: no conflict markers; search output is used to review behavior-sensitive call sites.

- [ ] **Step 2: Compare against the closed reference branch for critical paths**

Run:

```bash
git diff --stat origin/feat/go-ethereum-v1.17.3-upstream-merge -- core/state_transition.go core/vm/evm.go core/vm/interpreter.go miner/taiko_worker.go internal/ethapi/simulate.go beacon/engine/types.go params/config.go
git diff origin/feat/go-ethereum-v1.17.3-upstream-merge -- core/state_transition.go core/vm/evm.go core/vm/interpreter.go miner/taiko_worker.go internal/ethapi/simulate.go beacon/engine/types.go params/config.go
```

Expected: differences are either from fresh branch metadata or intentionally corrected conflict resolution. Unexpected consensus differences must be fixed before verification.

- [ ] **Step 3: Commit any follow-up behavior-preservation fix**

If review requires a source fix, run:

```bash
git add <fixed-files>
git diff --check --cached
git commit -m "fix(taiko): preserve behavior after v1.17.3 merge"
```

Expected: no follow-up commit if merge conflict resolution is already correct.

## Task 5: Build and Focused Tests

**Files:**
- Inspect test results only unless failures require fixes

- [ ] **Step 1: Build all packages**

Run:

```bash
go build ./...
```

Expected: build succeeds.

- [ ] **Step 2: Run focused tests for consensus and touched APIs**

Run:

```bash
go test ./consensus/taiko/... ./core/... ./core/vm/... ./miner/... ./eth/catalyst/... ./internal/ethapi/...
```

Expected: all selected packages pass. If a package is duplicated by `./core/...` and `./core/vm/...`, Go handles the overlap as separate listed packages without changing test semantics.

- [ ] **Step 3: Fix local failures caused by the merge**

If a failure appears, first reproduce only the failing package:

```bash
go test <failing-package> -run <failing-test-name> -count=1 -v
```

Then apply the smallest behavior-preserving fix, stage it, and commit:

```bash
git add <fixed-files>
git diff --check --cached
git commit -m "fix(taiko): address v1.17.3 merge test failure"
```

Expected: no fix commit unless the failure is introduced by the merge.

## Task 6: Clean Planning Docs From PR Diff

**Files:**
- Delete:
  - `docs/superpowers/specs/2026-06-14-go-ethereum-v1.17.3-upstream-merge-design.md`
  - `docs/superpowers/plans/2026-06-14-go-ethereum-v1.17.3-upstream-merge.md`

- [ ] **Step 1: Remove temporary workflow docs**

Run:

```bash
git rm docs/superpowers/specs/2026-06-14-go-ethereum-v1.17.3-upstream-merge-design.md docs/superpowers/plans/2026-06-14-go-ethereum-v1.17.3-upstream-merge.md
git commit -m "chore: remove working design and plan docs before PR"
```

Expected: PR diff no longer contains planning docs.

## Task 7: Push and Open Fresh Draft PR

**Files:**
- Modify: remote branch and GitHub PR metadata

- [ ] **Step 1: Inspect final branch shape**

Run:

```bash
git log --oneline --decorate --max-count=12
git diff --stat origin/taiko...HEAD
git status --short --branch
```

Expected: branch contains the design checkpoint, plan checkpoint, ancestry merge, v1.17.3 merge, optional fixes, and final docs-removal commit; working tree is clean.

- [ ] **Step 2: Push fresh branch**

Run:

```bash
git push -u origin codex/go-ethereum-v1.17.3-upstream-merge
```

Expected: remote branch created or updated under the fresh `codex/` branch name.

- [ ] **Step 3: Open draft PR**

Run:

```bash
gh pr create --repo taikoxyz/taiko-geth --base taiko --head codex/go-ethereum-v1.17.3-upstream-merge --draft --title 'feat(repo): `go-ethereum` v1.17.3 upstream merge' --body-file /tmp/taiko-geth-v1.17.3-pr-body.md
```

Before running, write `/tmp/taiko-geth-v1.17.3-pr-body.md` with:

```markdown
## Summary

Re-bases taiko-geth from go-ethereum **v1.17.2 -> v1.17.3** using upstream release commit `117e067f0f0bae1a17082321f224dedb6765b10f`.

This is intended to be behavior-preserving for Taiko. Upstream API changes are adopted where required, while Taiko ZK-gas, anchor transaction, payload, and fork semantics are preserved.

## Merge mechanics

PR #541 was squash-merged, so public `taiko` history did not retain upstream v1.17.2 as an ancestor. This branch first records v1.17.2 (`be4dc0c4be2fe316dbdd0a73e48421f64978232f`) as an empty `ours` merge, then merges v1.17.3 by hash. This keeps the visible conflict set to the true v1.17.2 -> v1.17.3 delta.

## Consensus-impact review

- ZK gas: Taiko ZK-gas accounting remains mapped to regular gas semantics after upstream's `GasBudget` refactor.
- Anchor handling: anchor balance-skip and refund behavior are preserved after upstream's uint256 `core.Message` changes.
- Engine payloads: Taiko `ExecutableData` fields are retained alongside upstream v1.17.3 fields.
- Fork predicates: Taiko fork predicates remain in `params/config.go`; upstream UBT naming is adopted where needed.

## Verification

- `go build ./...`
- `go test ./consensus/taiko/... ./core/... ./core/vm/... ./miner/... ./eth/catalyst/... ./internal/ethapi/...`

## Notes for reviewers

This is a fresh PR and branch. The closed PR #574 was used only as a reference for prior conflict decisions.
```

Expected: new draft PR URL is printed.
