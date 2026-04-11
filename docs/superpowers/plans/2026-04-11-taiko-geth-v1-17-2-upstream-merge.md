# Taiko-geth go-ethereum v1.17.2 Upstream Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge upstream go-ethereum v1.15.5 → v1.17.2 into taiko-geth on an integration branch (`upstream-v1.17.2-merge`), preserving all Taiko features and the Shanghai-capped Taiko fork config, landing via GitHub squash-merge.

**Architecture:** Big-bang merge with `git replace --graft` to restore v1.15.5 as the effective merge base (the previous upstream sync was a GitHub squash commit, so git currently resolves the merge base to v1.13.14). Conflicts resolved in seven subsystem tiers, each producing one stacked commit with a clear exit gate. Two verification tests (Shanghai-cap invariant, RPC compatibility surface) are written on the baseline before the merge so they catch regressions during and after resolution.

**Tech Stack:** Go 1.23+, go-ethereum v1.17.2, git worktrees, git replace-refs, GNU Make, golangci-lint.

**Spec:** `docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md`

**Reference SHAs (verified, do not guess):**
- Upstream v1.17.2 release: `be4dc0c4be2fe316dbdd0a73e48421f64978232f` (short `be4dc0c4b`)
- Upstream v1.15.5 release: `4263936a0` (via `v1.15.5` tag, which IS the upstream tag — safe to use)
- Prior squash commit to graft: `364acd00d` (PR #395, "feat(repo): `go-ethereum` v1.15.5 upstream merge")
- Original parent of `364acd00d`: `03f614fb2` (`chore(core): revert []*ethapi.RPCTransaction changes in miner (#394)`)
- **DO NOT USE** bare `v1.17.2` tag — it resolves to `3ac2f7be8` (Taiko commit), not the upstream release.

**Conflict resolution policy:**
- **Upstream wins on structure.** Adopt upstream's new interfaces, types, and function signatures.
- **Taiko preserves behavior.** Every `if config.Taiko { ... }` branch, every `CHANGE(taiko):` comment-marked change, and every `taiko_*.go` file's logic stays intact.
- **When upstream removes a helper Taiko depends on**, check Taiko callers and port them to the new upstream API — do NOT resurrect the removed helper.
- **When upstream adds a new post-Shanghai fork field**, add it to the `ChainConfig` struct but leave it `nil` on `TaikoChainConfig`.

---

## Phase 0: Baseline verification and setup

### Task 1: Write the Shanghai-cap invariant test on the current `taiko` branch

**Why first:** This test must pass on the current baseline before the merge starts. It becomes a regression canary for the Shanghai-cap invariant throughout the merge. Without this test, an accidental fork activation on Taiko networks during merge conflict resolution could silently slip through.

**Files:**
- Modify: `params/taiko_config_test.go`

- [ ] **Step 1.1: Read the current test file to understand the testing conventions**

Run: `cat params/taiko_config_test.go`

Expected output: the existing `TestNetworkIDToChainConfigOrDefault` test using table-driven style with `t.Run` subtests.

- [ ] **Step 1.2: Add the Shanghai-cap invariant test**

Append the following test to `params/taiko_config_test.go` (after the existing `TestNetworkIDToChainConfigOrDefault` function, before the closing of the file):

```go
// TestTaikoChainConfigShanghaiCap asserts TaikoChainConfig stops at Shanghai
// and does not accidentally activate any later fork. Any upstream merge that
// adds a new post-Shanghai fork field must leave it nil on TaikoChainConfig.
//
// If upstream adds a new post-Shanghai fork (e.g. Amsterdam), extend the
// `laterForkPredicates` slice below with a call to the new predicate.
func TestTaikoChainConfigShanghaiCap(t *testing.T) {
	// Every post-Shanghai fork predicate that currently exists on ChainConfig.
	// When upstream adds a new one, append to this slice.
	laterForkPredicates := []struct {
		name string
		fn   func(*ChainConfig, *big.Int, uint64) bool
	}{
		{"IsCancun", (*ChainConfig).IsCancun},
		{"IsPrague", (*ChainConfig).IsPrague},
		{"IsOsaka", (*ChainConfig).IsOsaka},
	}

	// TaikoChainConfig must be Shanghai-capped.
	if !TaikoChainConfig.IsShanghai(big.NewInt(0), 0) {
		t.Fatal("TaikoChainConfig must have Shanghai active at time 0")
	}

	// At the edge timestamps, no later fork may be active on TaikoChainConfig.
	for _, ts := range []uint64{0, ^uint64(0)} {
		for _, p := range laterForkPredicates {
			if p.fn(TaikoChainConfig, big.NewInt(0), ts) {
				t.Fatalf("TaikoChainConfig.%s must be false at timestamp %d", p.name, ts)
			}
		}
	}

	// The Taiko flag must be set so that config.Taiko-gated code paths are
	// reachable for Taiko networks.
	if !TaikoChainConfig.Taiko {
		t.Fatal("TaikoChainConfig.Taiko must be true")
	}
}
```

- [ ] **Step 1.3: Run the new test on the current baseline**

Run: `go test ./params/... -run TestTaikoChainConfigShanghaiCap -v`

Expected: `--- PASS: TestTaikoChainConfigShanghaiCap`. If it fails on the current baseline, STOP — the baseline is already broken and the merge plan is premised on a working start state.

- [ ] **Step 1.4: Commit the baseline test**

```bash
git add params/taiko_config_test.go
git commit -m "$(cat <<'EOF'
test(params): add Shanghai-cap invariant test for TaikoChainConfig

Asserts TaikoChainConfig activates Shanghai but never Cancun, Prague,
Osaka, or any later fork, at both timestamp 0 and max uint64. Regression
canary for the v1.17.2 upstream merge — any future upstream fork field
that leaks a non-nil default onto Taiko networks will fail this test.

When upstream adds a new post-Shanghai fork, extend laterForkPredicates
with the new predicate.
EOF
)"
```

### Task 2: Write the RPC compatibility surface test on the current `taiko` branch

**Why first:** Same rationale as Task 1 — this test captures the full `taiko_*` / `taikoAuth_*` method surface so the merge cannot silently drop or rename an RPC method.

**Files:**
- Modify: `eth/taiko_api_backend_test.go`

- [ ] **Step 2.1: Read the existing test patterns**

Run: `grep -n 'reflect\|MethodByName' eth/taiko_api_backend_test.go`

Expected: the existing `TestTaikoAuthBackendExposesBatchLookupMethods` and `TestTaikoAPIBackendHidesBatchLookupMethods` functions using the `reflect` pattern. The new test will extend this pattern for the full method surface.

- [ ] **Step 2.2: Add the full RPC compatibility test**

Append the following to `eth/taiko_api_backend_test.go`:

```go
// TestTaikoAPIBackendFullSurface asserts that every public taiko_* and
// taikoAuth_* RPC method from the documented compatibility surface is still
// present on its backend after the v1.17.2 upstream merge. If this test
// fails, a merge conflict resolution has silently removed or renamed an RPC
// method — that is a compatibility break and must be fixed before landing.
//
// Reference: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md
// See "Compatibility surface" section.
func TestTaikoAPIBackendFullSurface(t *testing.T) {
	publicMethods := []string{
		"HeadL1Origin",
		"L1OriginByID",
		"GetSyncMode",
	}
	authMethods := []string{
		"LastL1OriginByBatchID",
		"LastBlockIDByBatchID",
		"LastCertainBlockIDByBatchID",
		"LastCertainL1OriginByBatchID",
		"SetHeadL1Origin",
		"SetBatchToLastBlock",
		"UpdateL1Origin",
		"SetL1OriginSignature",
		"TxPoolContent",
		"TxPoolContentWithMinTip",
	}

	publicType := reflect.TypeOf(&TaikoAPIBackend{})
	for _, name := range publicMethods {
		if _, ok := publicType.MethodByName(name); !ok {
			t.Errorf("TaikoAPIBackend is missing method %s", name)
		}
	}
	authType := reflect.TypeOf(&TaikoAuthAPIBackend{})
	for _, name := range authMethods {
		if _, ok := authType.MethodByName(name); !ok {
			t.Errorf("TaikoAuthAPIBackend is missing method %s", name)
		}
	}
}

// TestL1OriginJSONShape asserts that the L1Origin JSON representation still
// exposes every field downstream Taiko clients rely on, including the
// hexutil.Bytes-marshaled signature. If upstream refactors rawdb.L1Origin via
// struct embedding or a JSON-tag change, this test fails and blocks the merge.
//
// Signature is a [65]byte on the struct but marshals as a 0x-prefixed hex
// string via the gencodec override in gen_taiko_l1_origin.go. This behavior
// was added specifically for L1Origin.Signature in commit 01dfc264a.
func TestL1OriginJSONShape(t *testing.T) {
	var sig [65]byte
	sig[0], sig[1], sig[2], sig[3] = 0xde, 0xad, 0xbe, 0xef
	origin := &rawdb.L1Origin{
		BlockID:            new(big.Int).SetUint64(42),
		L2BlockHash:        common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		L1BlockHeight:      new(big.Int).SetUint64(100),
		L1BlockHash:        common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		BuildPayloadArgsID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		IsForcedInclusion:  true,
		Signature:          sig,
	}
	data, err := json.Marshal(origin)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded := map[string]interface{}{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{
		"blockID",
		"l2BlockHash",
		"l1BlockHeight",
		"l1BlockHash",
		"buildPayloadArgsID",
		"isForcedInclusion",
		"signature",
	} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("L1Origin JSON missing field %q; got %s", field, string(data))
		}
	}
	// Signature must marshal as a 0x-prefixed hex string. The full 65 bytes
	// are encoded, so we just check the prefix and the first 4 bytes of the
	// payload.
	sigStr, ok := decoded["signature"].(string)
	if !ok {
		t.Fatalf("L1Origin.signature: expected string, got %T: %v", decoded["signature"], decoded["signature"])
	}
	if len(sigStr) != 2+2*65 { // "0x" + hex of 65 bytes
		t.Errorf("L1Origin.signature: expected length %d, got %d (%q)", 2+2*65, len(sigStr), sigStr)
	}
	if sigStr[:10] != "0xdeadbeef" {
		t.Errorf("L1Origin.signature: expected 0xdeadbeef prefix, got %s", sigStr[:10])
	}
}
```

- [ ] **Step 2.3: Add the `encoding/json` import if not already present**

Run: `grep -n '"encoding/json"' eth/taiko_api_backend_test.go`

If the output is empty, edit `eth/taiko_api_backend_test.go` to add `"encoding/json"` to the import block (alphabetical position). If the output shows it's already imported, skip this step.

- [ ] **Step 2.4: Run the new tests on baseline**

Run: `go test ./eth/... -run 'TestTaikoAPIBackendFullSurface|TestL1OriginJSONShape' -v`

Expected: both tests PASS. If they fail on baseline, STOP — something is already wrong with the current taiko branch.

- [ ] **Step 2.5: Commit the baseline test**

```bash
git add eth/taiko_api_backend_test.go
git commit -m "$(cat <<'EOF'
test(eth): add full RPC compatibility surface test for Taiko backends

Asserts every method in the documented taiko_* and taikoAuth_* RPC
namespaces is present on its backend, plus the L1Origin JSON shape
(blockID, l2BlockHash, l1BlockHeight, l1BlockHash, signature as
hexutil.Bytes). Regression canary for the v1.17.2 upstream merge.

If upstream refactors struct embedding or JSON tags during the merge,
this test blocks the landing until the compatibility surface is
restored.
EOF
)"
```

### Task 3: Run the full baseline test suite

**Why:** Record a known-good state so any post-merge regression is clearly attributable to the merge, not to pre-existing flakiness.

- [ ] **Step 3.1: Run `make test` on the current `taiko` branch and capture output**

```bash
make test 2>&1 | tee /tmp/taiko-baseline-test.log
echo "exit=$?" >> /tmp/taiko-baseline-test.log
```

Expected: `make test` exits 0. The log is retained at `/tmp/taiko-baseline-test.log`.

If `make test` fails on the current `taiko` branch, STOP and report the failures. The merge cannot proceed on a broken baseline.

- [ ] **Step 3.2: Save the baseline log into the plan directory**

```bash
cp /tmp/taiko-baseline-test.log docs/superpowers/plans/2026-04-11-baseline-test.log
git add docs/superpowers/plans/2026-04-11-baseline-test.log
git commit -m "test(baseline): record v1.15.5-baseline make test output for merge diff"
```

### Task 4: Push baseline commits and open a short PR to merge into `taiko`

**Why:** Tasks 1–3 add verification tests to `taiko` that must exist before the merge. They can land on `taiko` as a separate small PR so the big merge PR stays focused on upstream reconciliation. If you prefer not to split PRs, skip Task 4 entirely and carry these commits on the integration branch instead.

- [ ] **Step 4.1: Create the baseline-test branch and push**

```bash
git switch -c taiko-v1-17-2-baseline-tests
git push -u origin taiko-v1-17-2-baseline-tests
```

- [ ] **Step 4.2: Open the baseline-test PR**

```bash
gh pr create --base taiko --head taiko-v1-17-2-baseline-tests \
  --title "test: baseline regression canaries for v1.17.2 upstream merge" \
  --body "$(cat <<'EOF'
## Summary
- Adds Shanghai-cap invariant test for `TaikoChainConfig`
- Adds full RPC compatibility surface test for `taiko_*`/`taikoAuth_*` namespaces
- Records current `make test` baseline log

These tests must pass on the current taiko branch before the v1.17.2 upstream merge begins. See `docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md`.

## Test plan
- [x] `go test ./params/... -run TestTaikoChainConfigShanghaiCap`
- [x] `go test ./eth/... -run 'TestTaikoAPIBackendFullSurface|TestL1OriginJSONShape'`
- [x] Full `make test` baseline recorded

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4.3: Wait for CI and land**

After CI passes and the PR is reviewed/approved, merge it to `taiko`. Then in your local worktree:

```bash
git switch taiko
git pull origin taiko
```

### Task 5: Create the integration worktree

**Files:**
- New worktree: `.worktrees/upstream-v1.17.2-merge`
- New local branch: `upstream-v1.17.2-merge`

- [ ] **Step 5.1: Confirm no stale local branch exists**

Run: `git branch --list upstream-v1.17.2-merge`

If a stale branch exists (left over from the prior codex attempt), delete it:

```bash
git branch -D upstream-v1.17.2-merge
```

- [ ] **Step 5.2: Confirm current taiko HEAD**

```bash
git rev-parse taiko
git log taiko --oneline -1
```

Record the output — this is the rollback point.

- [ ] **Step 5.3: Fetch upstream**

```bash
git fetch upstream --tags
```

- [ ] **Step 5.4: Verify upstream v1.17.2 SHA**

```bash
git rev-parse be4dc0c4b
git log be4dc0c4b --oneline -1
```

Expected: the commit shows `version: release go-ethereum v1.17.2 stable (#34618)`. If the SHA doesn't resolve or shows a different commit, STOP — the upstream remote is pointing at a different repo.

- [ ] **Step 5.5: Verify the `v1.17.2` tag collision**

```bash
git rev-parse v1.17.2
git log $(git rev-parse v1.17.2) --oneline -1
```

Expected: `3ac2f7be8 remove golden touch from mempool (#484)` — a Taiko commit. This confirms the collision described in the spec. NEVER use the bare `v1.17.2` tag name in merge commands.

- [ ] **Step 5.6: Create the worktree**

```bash
git worktree add .worktrees/upstream-v1.17.2-merge -b upstream-v1.17.2-merge taiko
cd .worktrees/upstream-v1.17.2-merge
git status
```

Expected: clean worktree on branch `upstream-v1.17.2-merge`, based on `taiko`.

From this point on, **all git and build commands run inside `.worktrees/upstream-v1.17.2-merge`** unless explicitly stated otherwise.

### Task 6: Apply the ancestry graft

**Why:** The previous upstream sync (`364acd00d`) was a GitHub squash-merge, so git has lost the v1.15.5 → taiko ancestry link. `git merge-base taiko be4dc0c4b` currently resolves to `9038ba694` (v1.13.14), which would cause git to re-merge four years of upstream changes already integrated by 364acd00d. The graft creates a local replacement ref that tells git v1.15.5 is a parent of 364acd00d, without modifying any commit SHA.

- [ ] **Step 6.1: Capture the pre-graft merge base**

```bash
git merge-base upstream-v1.17.2-merge be4dc0c4b
```

Expected: `9038ba694` (or the full SHA `9038ba69428a6ecada1f2acace6981854482748b`, a.k.a. `v1.13.14`). This is the broken state we're fixing.

- [ ] **Step 6.2: Apply the graft**

```bash
git replace --graft 364acd00d 4263936a0 03f614fb2
```

This tells git that commit `364acd00d` (the Taiko squash merge of v1.15.5) has two parents: `4263936a0` (upstream v1.15.5) and `03f614fb2` (its original parent on the Taiko branch).

- [ ] **Step 6.3: Verify the graft worked**

```bash
git merge-base upstream-v1.17.2-merge be4dc0c4b
```

Expected: `4263936a0402e2b175a366091ed3cb54133bc6a8` (upstream v1.15.5). If the output is still `9038ba694` or something else, the graft did not take effect — STOP and debug.

- [ ] **Step 6.4: Sanity-check the replace ref is recorded**

```bash
git replace --list
```

Expected output includes: `364acd00d1f2b45a07b7b1d20ec9b0f77be50b91` (or the short form).

**Exit gate**: `git merge-base upstream-v1.17.2-merge be4dc0c4b` resolves to `4263936a0`. The Phase 0 setup is complete.

---

## Phase 1: Raw merge and conflict triage

### Task 7: Run the raw merge without committing

**Files:**
- All files in the worktree (merge state, not yet committed)

- [ ] **Step 7.1: Start the merge**

```bash
git merge --no-commit --no-ff be4dc0c4b
```

Expected: git prints a stream of auto-merge notices and conflict markers, then reports `Automatic merge failed; fix conflicts and then commit the result.`

Do NOT run `git merge --abort`. Do NOT edit any file yet. Leave the merge in progress.

- [ ] **Step 7.2: Capture the conflict set**

```bash
git diff --name-only --diff-filter=U > /tmp/conflicts.txt
wc -l /tmp/conflicts.txt
cat /tmp/conflicts.txt
```

Record the count and the full list. Expected: somewhere between 20 and 80 conflicted files, concentrated in the tiers identified in the spec.

- [ ] **Step 7.3: Capture the auto-merged and newly-added file sets**

```bash
git status --short > /tmp/merge-status.txt
head -100 /tmp/merge-status.txt
```

`M` = modified (auto-merged or conflicted), `A` = added, `U` = unmerged conflict, `??` = untracked. Keep the file for reference.

### Task 8: Classify conflicts into the tier buckets

**Files:**
- Create: `docs/superpowers/plans/2026-04-11-upstream-v1-17-2-conflict-triage.md`

- [ ] **Step 8.1: Create the triage document**

Create `docs/superpowers/plans/2026-04-11-upstream-v1-17-2-conflict-triage.md` with the following template (filled in from `/tmp/conflicts.txt`):

```markdown
# v1.17.2 Upstream Merge Conflict Triage

Generated during Phase 1 from `git diff --name-only --diff-filter=U`.
Every conflicted file MUST be listed under exactly one tier.

## Tier 1 — Shared infrastructure and CLI
Expected files: build/ci.go, cmd/geth/main.go, cmd/utils/flags.go,
cmd/utils/taiko_flags.go, node/defaults.go, node/endpoints.go,
eth/ethconfig/config.go, README.md, AGENTS.md, CLAUDE.md

Actual files:
- [list conflicted files from /tmp/conflicts.txt that belong here]

## Tier 2 — Params and chain config
Expected: params/config.go, params/protocol_params.go, params/taiko_config.go,
core/taiko_genesis.go, core/taiko_genesis/*.json

Actual:
- [list]

## Tier 3 — Core execution and transaction types
Expected: core/blockchain.go, core/state_processor.go, core/state_transition.go,
core/types/transaction.go, core/types/tx_dynamic_fee.go,
core/types/taiko_transaction.go, core/txpool/validation.go, core/tracing/hooks.go

Actual:
- [list]

## Tier 4 — Consensus engine
Expected: consensus/taiko/consensus.go, consensus/misc/taiko_eip4396.go

Actual:
- [list]

## Tier 5 — Engine API, miner, payload building
Expected: eth/catalyst/api.go, eth/catalyst/queue.go, miner/worker.go,
miner/payload_building.go, miner/taiko_miner.go, miner/taiko_worker.go,
miner/taiko_payload_building.go, beacon/engine/types.go

Actual:
- [list]

## Tier 6 — RPC surface
Expected: eth/api_backend.go, eth/api_debug.go, eth/state_accessor.go,
eth/tracers/api.go, eth/taiko_api_backend.go, ethclient/taiko_api.go,
rpc/server.go, rpc/json.go

Actual:
- [list]

## Tier 7 — Persistence and genesis
Expected: core/rawdb/taiko_l1_origin.go, core/rawdb/gen_taiko_l1_origin.go,
core/taiko_genesis/*

Actual:
- [list]

## Tier 8 — Tests
All conflicted *_test.go files. Resolved in Phase 9 after all source tiers.

Actual:
- [list]

## Uncategorized
Files that don't fit any tier — require review before deciding where they go.

Actual:
- [list]
```

- [ ] **Step 8.2: Classify every file**

For each file in `/tmp/conflicts.txt`, add it to exactly one tier section. If a file doesn't fit, add it to "Uncategorized" and STOP to discuss before proceeding. A complete classification is the prerequisite for Phase 2.

- [ ] **Step 8.3: Commit the triage document (on the merge-in-progress state)**

The merge is still in progress, so git will allow committing new untracked files even though the merge isn't done:

```bash
git add docs/superpowers/plans/2026-04-11-upstream-v1-17-2-conflict-triage.md
```

Do NOT run `git commit` yet — committing would finalize the merge with conflicts still present. The triage document stays staged through the merge resolution and lands in the Phase 9 final commit.

**Exit gate**: triage document exists and every conflicted file is categorized into exactly one tier.

---

## Phase 2: Tier 1 reconcile — shared infrastructure

### Task 9: Resolve Tier 1 files

**Files:**
- Resolve: `build/ci.go`, `cmd/geth/main.go`, `cmd/utils/flags.go`, `cmd/utils/taiko_flags.go`, `node/defaults.go`, `node/endpoints.go`, `eth/ethconfig/config.go`, `README.md`, `AGENTS.md`, `CLAUDE.md` (whichever of these are in the Tier 1 triage list)

**Strategy:** Upstream wins on structure. Taiko-specific flags (`--taiko`, taiko genesis flags) and defaults are preserved. These files are low semantic risk but high build-blockage impact — resolve them first so the compiler starts giving feedback.

- [ ] **Step 9.1: List the Tier 1 files from triage and open each one in turn**

Use the Tier 1 list from the triage document. For each file, inspect the conflict markers:

```bash
grep -n '^<<<<<<< \|^======= \|^>>>>>>> ' <file>
```

- [ ] **Step 9.2: For each Tier 1 file, resolve the conflict**

Resolution rules for this tier:

1. **CLI flag files** (`cmd/geth/main.go`, `cmd/utils/flags.go`, `cmd/utils/taiko_flags.go`):
   - Adopt upstream's flag registration structure.
   - Preserve every Taiko-specific flag defined in `cmd/utils/taiko_flags.go` and every registration of those flags in `cmd/geth/main.go`.
   - Preserve the Taiko RPC namespace registration (`taiko_*` and `taikoAuth_*`).

2. **Node wiring** (`node/defaults.go`, `node/endpoints.go`, `eth/ethconfig/config.go`):
   - Adopt upstream's defaults struct layout.
   - Preserve any `CHANGE(taiko):`-marked override of a default value.

3. **Build scripts** (`build/ci.go`): adopt upstream changes unless Taiko-specific builder logic is present (check for `CHANGE(taiko):` markers). If such markers exist, preserve their logic on top of upstream structure.

4. **Documentation files** (`README.md`, `AGENTS.md`, `CLAUDE.md`): adopt upstream's README structure but preserve the Taiko-specific sections. Do NOT lose the Taiko documentation.

- [ ] **Step 9.3: After each file, verify no conflict markers remain**

```bash
grep -rn '^<<<<<<< \|^======= \|^>>>>>>> ' <file>
```

Expected: no output.

- [ ] **Step 9.4: Incrementally `git add` each resolved file**

```bash
git add <file>
```

- [ ] **Step 9.5: Compile the Tier 1 packages**

```bash
go build ./cmd/... ./node/... ./eth/ethconfig/... ./build/...
```

Expected: either clean compilation, or compiler errors that reference symbols defined in Tier 2+ (still-conflicted) packages. Errors referencing Tier 1 symbols mean the resolution is wrong — go back and fix.

If cross-package errors reference `params`, `core`, etc., that's acceptable at this stage — those tiers haven't been resolved yet.

- [ ] **Step 9.6: Commit Tier 1 resolution**

```bash
git commit -m "chore(upstream-merge): reconcile shared infrastructure for v1.17.2

Tier 1 of the v1.17.2 upstream merge. Adopts upstream's CLI, node wiring,
ethconfig, and build script structure while preserving all Taiko-specific
flags, defaults, and RPC namespace registrations.

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

Note: this commit still leaves the merge in progress (other tiers are not yet resolved). Git allows partial commits while in a merge state — each tier advances the resolved file count.

**Exit gate**: `go build ./cmd/... ./node/... ./eth/ethconfig/... ./build/...` compiles cleanly OR only fails on references into unresolved lower-tier packages.

---

## Phase 3: Tier 2 reconcile — params and chain config

### Task 10: Resolve Tier 2 files

**Files:**
- Resolve: `params/config.go`, `params/protocol_params.go`, `params/taiko_config.go`, `core/taiko_genesis.go`, `core/taiko_genesis/*.json` (from triage list)

**Critical invariant**: `TaikoChainConfig` must remain Shanghai-capped. Every new upstream post-Shanghai fork field stays `nil` on `TaikoChainConfig`.

- [ ] **Step 10.1: Resolve `params/config.go` — the hardest file in this tier**

Existing `CHANGE(taiko):` markers that MUST be preserved (lines from current `taiko` HEAD, may shift after upstream edits — search by comment text, not line number):

- `// CHANGE(taiko): add Taiko network name.` (around line 341) — the network name constant for display.
- `// CHANGE(taiko): Taiko network flag.` (around line 409) — the `Taiko bool` field on `ChainConfig`.
- `// CHANGE(taiko): print Taiko consensus engine in banner.` (around line 446) — banner printing branch.
- `// CHANGE(taiko): add Taiko forks to banner.` (around line 515) — Ontake/Pacaya/Shasta banner entries.
- `// CHANGE(taiko): IsOntake returns...` (around line 650) — fork predicate.
- `// CHANGE(taiko): IsPacaya returns...` (around line 655) — fork predicate.
- `// CHANGE(taiko): IsShasta returns...` (around line 660) — fork predicate.
- The `OntakeBlock`, `PacayaBlock`, and `ShastaTime` fields on `ChainConfig`.

Resolution rule: if upstream adds new fork fields to `ChainConfig`, add them in upstream's position. Taiko fields (`Ontake`, `Pacaya`, `Shasta`, `Taiko bool`) stay exactly where they are. Predicates stay exactly as-is.

```bash
grep -n '^<<<<<<< ' params/config.go
```

For each conflict hunk, apply upstream's structural change + preserve the Taiko marker.

- [ ] **Step 10.2: Resolve `params/protocol_params.go`**

Existing Taiko markers:
- `// CHANGE(taiko): add ShastaInitialBaseFee for Shasta fork` (constant `ShastaInitialBaseFee = 25_000_000`)
- `// CHANGE(taiko): extraData layout for Shasta blocks.`

Both must survive. Adopt upstream's other protocol parameter changes as-is.

- [ ] **Step 10.3: Resolve `params/taiko_config.go`**

This file is Taiko-exclusive (no CHANGE markers, the whole file is a Taiko addition). Conflicts here only happen if upstream touched the file — extremely unlikely. If a conflict exists:

- Preserve the `TaikoChainConfig` struct definition VERBATIM.
- Preserve the `networkIDToChainConfig` map.
- Preserve the `NetworkIDToChainConfigOrDefault` function.
- Only accept upstream changes if they're purely mechanical type renames and don't touch semantics.

- [ ] **Step 10.4: Resolve `core/taiko_genesis.go`**

Preserve VERBATIM:
- `MainnetOntakeBlock = new(big.Int).SetUint64(538_304)`
- `MainnetPacayaBlock = new(big.Int).SetUint64(1_166_000)`
- `MainnetShastaTime = 1_775_135_700`
- `HoodiShastaTime = 1_770_296_400`
- `InternalShastaTime = 0`
- `MasayaShastaTime = 0`
- The `TaikoGenesisBlock` function's network ID switch.

- [ ] **Step 10.5: Resolve `core/taiko_genesis/*.json` files**

These files should not conflict (upstream never modifies them). If any of them are in the conflict list, STOP and investigate — a rebase-style merge may be rewriting them incorrectly.

- [ ] **Step 10.6: Verify no markers remain**

```bash
grep -rn '^<<<<<<< \|^======= \|^>>>>>>> ' params/ core/taiko_genesis.go core/taiko_genesis/
```

Expected: no output.

- [ ] **Step 10.7: Stage the resolved files**

```bash
git add params/ core/taiko_genesis.go core/taiko_genesis/
```

- [ ] **Step 10.8: Compile the params and genesis packages**

```bash
go build ./params/... ./core/taiko_genesis/...
```

Expected: clean compilation. Errors into `core` or other packages are OK (still unresolved).

- [ ] **Step 10.9: Run the baseline Shanghai-cap invariant test**

```bash
go test ./params/... -run 'TestTaikoChainConfigShanghaiCap|TestNetworkIDToChainConfigOrDefault' -v
```

Expected: both tests PASS. If `TestTaikoChainConfigShanghaiCap` fails, a new upstream fork field has leaked a non-nil default onto `TaikoChainConfig` — STOP, find the leak, and fix it before advancing.

- [ ] **Step 10.10: Commit Tier 2 resolution**

```bash
git commit -m "chore(upstream-merge): reconcile params and chain config for v1.17.2

Tier 2 of the v1.17.2 upstream merge. Adds upstream's new fork config
fields to ChainConfig while keeping TaikoChainConfig Shanghai-capped.
Preserves Ontake/Pacaya/Shasta fork fields, predicates, and Taiko
genesis block values.

Shanghai-cap invariant test (TestTaikoChainConfigShanghaiCap) passes.

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

**Exit gate**: `go build ./params/... ./core/taiko_genesis/...` compiles, and `TestTaikoChainConfigShanghaiCap` passes. Any fork predicate returning `true` for Taiko networks at timestamp 0 or max uint64 is a blocker.

---

## Phase 4: Tier 3 reconcile — core execution and transaction types

### Task 11: Resolve Tier 3 files

**Files:**
- Resolve: `core/blockchain.go`, `core/state_processor.go`, `core/state_transition.go`, `core/types/transaction.go`, `core/types/tx_dynamic_fee.go`, `core/types/taiko_transaction.go`, `core/txpool/validation.go`, `core/tracing/hooks.go`

**Strategy**: upstream wins on EIP-7702/7623/7708 plumbing, state transition refactor, and tracing hook signatures. Every `if p.config.Taiko { ... }` branch and every `CHANGE(taiko):`-marked line stays.

- [ ] **Step 11.1: Resolve `core/state_processor.go`**

Existing Taiko markers (from current `taiko` HEAD):

- Around line 94: `// CHANGE(taiko): mark the first transaction as anchor transaction.`
- Around line 95: `if i == 0 && p.config.Taiko { ... }` — marks the first tx as the anchor tx. This branch MUST be preserved.
- Around line 219: `// CHANGE(taiko): decode the basefeeSharingPctg config from the extradata.`

Resolution: let upstream refactor the state processor loop, but reinsert the `if i == 0 && p.config.Taiko` branch in the equivalent loop position in the new upstream code. If upstream changed how the tx index is tracked, use the new idiom but preserve the behavior (first tx is an anchor on Taiko chains).

- [ ] **Step 11.2: Resolve `core/state_transition.go`**

Taiko markers at approximately:
- Line 169: `// CHANGE(taiko): whether the current transaction is the first TaikoL2.anchor transaction in a block.`
- Line 171: `// CHANGE(taiko): basefeeSharingPctg of the basefee will be sent to the block.coinbase,`
- Line 294: `// CHANGE(taiko): if the transaction is an anchor transaction, the balance check is skipped.`
- Line 559: `// CHANGE(taiko): basefee is not burnt, but sent to a treasury and block.coinbase instead.`
- Line 692: `// CHANGE(taiko): returns the treasury address based on chain ID.`
- Line 706: `// CHANGE(taiko): DecodeShastaBasefeeSharingPctg returns the basefee sharing pctg`
- Line 715: `// CHANGE(taiko): DecodeShastaProposalID decodes the proposalId from bytes 1..6.`
- Line 725: `// CHANGE(taiko): decodes an Ontake/Pacaya block's extradata, returns basefeeSharingPctg.`

All must survive. Upstream between v1.15.5 and v1.17.2 changed state transition significantly (EIP-7702 authorization lists) — reconcile by:
1. Adopting upstream's new state transition structure.
2. Reinserting each Taiko branch at the semantically equivalent point.
3. Exporting the `DecodeShasta*` helpers unchanged (they're used by tests in `eth/taiko_api_backend_test.go`).

- [ ] **Step 11.3: Resolve `core/blockchain.go`**

Taiko marker at approximately line 295: `// CHANGE(taiko): check the chain config.`

Preserve the chain config check. Adopt upstream's blockchain refactors around it.

- [ ] **Step 11.4: Resolve `core/types/transaction.go` and `core/types/tx_dynamic_fee.go`**

If these files are conflicted, upstream probably added new transaction types (EIP-7702 sets code). Resolution:

1. Adopt upstream's new transaction type plumbing.
2. Preserve any Taiko-specific methods/fields added to the `Transaction` type.
3. Ensure `core/types/taiko_transaction.go` still compiles against the new `Transaction` interface — adapt the Taiko tx type to match new signatures if needed.

- [ ] **Step 11.5: Resolve `core/types/taiko_transaction.go`**

This file is Taiko-exclusive. Conflicts here only occur if upstream changed the `Transaction` type's internal interface that `taiko_transaction.go` depends on. Adapt the Taiko tx type to match upstream's new interface. Do NOT resurrect removed helpers — port forward.

- [ ] **Step 11.6: Resolve `core/txpool/validation.go` and `core/tracing/hooks.go`**

- `validation.go`: preserve any `config.Taiko` branches (e.g., anchor-tx allowance). Adopt upstream validation refactors.
- `tracing/hooks.go`: upstream changed tracing hook signatures (EIP-7708 burn logs). Preserve Taiko tracing hooks if any exist. Adopt upstream's new hook types.

- [ ] **Step 11.7: Verify no markers remain**

```bash
grep -rn '^<<<<<<< \|^======= \|^>>>>>>> ' core/blockchain.go core/state_processor.go core/state_transition.go core/types/ core/txpool/ core/tracing/
```

- [ ] **Step 11.8: Stage and compile the core packages**

```bash
git add core/
go build ./core/...
```

Expected: clean compilation within `./core/...`. Errors into `miner`, `eth`, etc. are still OK.

- [ ] **Step 11.9: Commit Tier 3 resolution**

```bash
git commit -m "chore(upstream-merge): reconcile core execution preserving Taiko branches

Tier 3 of the v1.17.2 upstream merge. Adopts upstream's state processor,
state transition, blockchain, transaction types, txpool validation, and
tracing hook changes (EIP-7702/7623/7708 plumbing). Preserves every
config.Taiko-gated branch including:

- first-tx anchor marking in state_processor
- basefee treasury/coinbase split in state_transition
- basefee sharing pctg decoding helpers
- Shasta extradata helpers (DecodeShastaProposalID, DecodeShastaBasefeeSharingPctg)
- anchor-tx balance-check skip

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

**Exit gate**: `go build ./core/...` compiles cleanly.

---

## Phase 5: Tier 4 reconcile — consensus engine

### Task 12: Resolve Tier 4 files

**Files:**
- Resolve: `consensus/taiko/consensus.go`, `consensus/misc/taiko_eip4396.go`, plus their test files (`consensus/taiko/consensus_test.go`, `consensus/misc/taiko_eip4396_test.go`)

**Strategy**: the Taiko consensus engine implements upstream's `consensus.Engine` interface. If upstream changed the interface, adapt. EIP-4396 behavior is UNCHANGED.

- [ ] **Step 12.1: Check for upstream `consensus.Engine` interface changes**

```bash
grep -n 'type Engine interface' consensus/consensus.go
```

Compare the current interface method list to the implementation in `consensus/taiko/consensus.go`:

```bash
grep -n '^func (.*Taiko)' consensus/taiko/consensus.go
```

Any interface method in `consensus.Engine` that the Taiko engine doesn't implement will cause a compile error — add a stub that delegates appropriately (usually: call a helper on the embedded ethash engine, or panic for Taiko-unreachable methods).

- [ ] **Step 12.2: Resolve `consensus/taiko/consensus.go`**

If conflicted, let upstream interface changes win on method signatures. Preserve Taiko-specific logic:

- Anchor transaction verification (`AnchorSelector`, `AnchorV2Selector`, `AnchorV3Selector`, `AnchorV4Selector` function selectors).
- Golden Touch address check: `0x0000777735367b36bC9B61C50022d9D0700dB4Ec`.
- Basefee sharing pctg parsing from extradata.
- Shasta fork-specific block header validation.

- [ ] **Step 12.3: Resolve `consensus/misc/taiko_eip4396.go`**

This is Taiko-exclusive. Preserve unchanged:

- Min basefee clamp: `0.01 Gwei` on mainnet, `0.005 Gwei` elsewhere.
- Shasta fork EIP-4396 calculation logic.

If upstream refactored `consensus/misc/` around it (e.g., moving helpers), adapt imports only; do not touch Taiko logic.

- [ ] **Step 12.4: Verify and stage**

```bash
grep -rn '^<<<<<<< \|^======= \|^>>>>>>> ' consensus/
git add consensus/
```

- [ ] **Step 12.5: Compile and test**

```bash
go build ./consensus/...
go test ./consensus/taiko/... ./consensus/misc/...
```

Expected: all consensus tests pass. Any failure in `consensus/taiko/consensus_test.go` or `consensus/misc/taiko_eip4396_test.go` is a blocker for this tier.

- [ ] **Step 12.6: Commit Tier 4 resolution**

```bash
git commit -m "chore(upstream-merge): reconcile Taiko consensus engine for v1.17.2

Tier 4 of the v1.17.2 upstream merge. Adapts consensus/taiko/consensus.go
to any upstream consensus.Engine interface changes. EIP-4396 min basefee
clamp (0.01 Gwei mainnet, 0.005 Gwei elsewhere), anchor selector
verification, golden touch enforcement, and Shasta header validation are
unchanged.

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

**Exit gate**: `go build ./consensus/...` and `go test ./consensus/taiko/... ./consensus/misc/...` both pass.

---

## Phase 6: Tier 5 reconcile — engine API, miner, payload building (hardest tier)

**IMPORTANT**: this is the largest and riskiest tier. Split into sub-tasks by file group. Each sub-task gets its own commit so the stack remains reviewable and revertible.

### Task 13: Resolve `miner/worker.go` and `miner/payload_building.go`

**Files:**
- Resolve: `miner/worker.go`, `miner/payload_building.go`

**Strategy**: Upstream restructured worker scheduling, added witness stats relocation, changed payload attribute plumbing. Adopt the new structure; reinsert Taiko behavior against it.

- [ ] **Step 13.1: Enumerate Taiko markers in `miner/worker.go`**

Current markers:
- Line ~94: `// CHANGE(taiko): The base fee per gas for the next block, used by the legacy Taiko blocks.` (`baseFeePerGas` field on `generateParams`)
- Line ~176: `// CHANGE(taiko): block.timestamp == parent.timestamp is allowed in Taiko protocol.`
- Line ~177: `if !miner.chainConfig.Taiko { ... }` — timestamp validation skip for Taiko.
- Line ~206: `if miner.chainConfig.Taiko && genParams.baseFeePerGas != nil { ... }` — basefee override for legacy Taiko blocks.

These must all survive, even if upstream reshaped the surrounding code. Search by comment text, not line number.

- [ ] **Step 13.2: Resolve `miner/worker.go`**

Resolution walkthrough:
1. Adopt upstream's new `generateParams` / `prepareWork` / `commit` structure.
2. Add the Taiko `baseFeePerGas` field to whatever struct holds generation parameters in the new upstream code.
3. Reinsert the timestamp-equality-allowed branch in upstream's timestamp validation code (still `if !miner.chainConfig.Taiko { ... }`).
4. Reinsert the legacy Taiko basefee override in upstream's basefee calculation branch.

- [ ] **Step 13.3: Enumerate Taiko markers in `miner/payload_building.go`**

Current markers:
- Line ~47: `TxListHash *common.Hash // CHANGE(taiko): The hash of the transaction list`
- Line ~48: `Extra []byte // CHANGE(taiko): The extra data of the block`
- Line ~62: `// CHANGE(taiko): include the transaction list hash in the payload id calculation`
- Line ~66: `// CHANGE(taiko): include the extra data in the payload id calculation`
- Line ~94: `// CHANGE(taiko): done channel to communicate we shouldnt write to 'stop' channel.`
- Line ~106: `// CHANGE(taiko): buffered channel to communicate done to taiko payload builder`
- Line ~159: `// CHANGE(taiko): signal to taiko payload builder to not write to 'payload.stop' channel`
- Line ~217: `// CHANGE(taiko): signal to taiko payload builder to not write to 'payload.stop' channel`
- Line ~281: `// CHANGE(taiko): do not update payload.`
- Line ~282: `if miner.chainConfig.Taiko { ... }` — Taiko-specific payload update skip.

All must survive.

- [ ] **Step 13.4: Resolve `miner/payload_building.go`**

Resolution walkthrough:
1. Adopt upstream's new `BuildPayloadArgs` struct + payload ID calculation.
2. Add `TxListHash *common.Hash` and `Extra []byte` as fields on `BuildPayloadArgs` (or whatever upstream renamed it to).
3. Include them in the payload ID hash calculation in the new location.
4. Preserve the done-channel signaling pattern for the Taiko payload builder — if upstream rewrote channel handling, adapt but keep the semantic that the Taiko builder can opt out of writing to `payload.stop`.
5. Reinsert the Taiko payload-update skip (`if miner.chainConfig.Taiko`).

- [ ] **Step 13.5: Verify and stage**

```bash
grep -n '^<<<<<<< \|^======= \|^>>>>>>> ' miner/worker.go miner/payload_building.go
git add miner/worker.go miner/payload_building.go
```

- [ ] **Step 13.6: Compile just these files**

```bash
go build ./miner/
```

Errors about Taiko-specific miner files (`miner/taiko_*.go`) are expected at this point — they'll be resolved in Task 14.

- [ ] **Step 13.7: Commit**

```bash
git commit -m "chore(upstream-merge): reconcile generic miner worker and payload builder

Tier 5 sub-task of the v1.17.2 upstream merge. Adopts upstream's
restructured worker scheduling and payload ID calculation. Preserves:

- baseFeePerGas override for legacy Taiko blocks
- timestamp-equality-allowed branch (parent == block on Taiko)
- TxListHash and Extra fields in BuildPayloadArgs
- Taiko payload-update skip signal
- done-channel pattern so Taiko builder can opt out of payload.stop

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

### Task 14: Adapt Taiko-exclusive miner files to the new interfaces

**Files:**
- Resolve or adapt: `miner/taiko_miner.go`, `miner/taiko_worker.go`, `miner/taiko_payload_building.go`

**Strategy**: These files are Taiko-exclusive and unlikely to textually conflict, but they will almost certainly break to compile against the new upstream miner package. Adapt them.

- [ ] **Step 14.1: Attempt a miner build**

```bash
go build ./miner/ 2>&1 | tee /tmp/miner-build.log
```

Look for errors like "undefined: ...", "cannot use ... as ...", "wrong number of arguments".

- [ ] **Step 14.2: For each compile error, patch the Taiko-exclusive file**

Common adaptations needed:
- Upstream renamed a helper → call the renamed helper.
- Upstream changed a function signature → update the call site.
- Upstream removed a struct field → use upstream's new accessor or equivalent field.
- Upstream moved a type → update the import.

Do NOT reintroduce removed upstream symbols. Port Taiko callers forward to new upstream APIs.

- [ ] **Step 14.3: Rebuild until clean**

```bash
go build ./miner/
```

Expected: clean. Repeat Step 14.2 if errors remain.

- [ ] **Step 14.4: Run the Taiko miner tests**

```bash
go test ./miner/... -run Taiko -v
```

Expected: all Taiko miner tests pass. If a test fails, root-cause the regression — likely a subtle behavior drift in the reconciled worker/payload-building code. Fix the source, not the test.

- [ ] **Step 14.5: Stage and commit**

```bash
git add miner/taiko_miner.go miner/taiko_worker.go miner/taiko_payload_building.go
git commit -m "chore(upstream-merge): adapt Taiko miner files to v1.17.2 interfaces

Tier 5 sub-task of the v1.17.2 upstream merge. Adapts taiko_miner.go,
taiko_worker.go, taiko_payload_building.go to compile against upstream's
restructured miner package. No behavior change to Taiko L2 payload
building; only API adaptation.

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

### Task 15: Resolve `eth/catalyst/api.go`, `eth/catalyst/queue.go`, and `beacon/engine/types.go`

**Files:**
- Resolve: `eth/catalyst/api.go`, `eth/catalyst/queue.go`, `beacon/engine/types.go`

**Strategy**: Upstream's Engine API v4 changed payload attributes, added blob/sidecar handling, reworked forkchoice updated. Adopt the new engine API; reinsert Taiko's L2-specific branches.

- [ ] **Step 15.1: Enumerate Taiko markers in `eth/catalyst/api.go`**

Current markers:
- Line ~383: `// CHANGE(taiko): check whether '--taiko' flag is set.`
- Line ~384: `isTaiko := api.eth.BlockChain().Config().Taiko`
- Line ~395: `} else if isTaiko { // CHANGE(taiko): reorg is allowed in L2.`
- Line ~440: `// CHANGE(taiko): create a L2 block by Taiko protocol.`
- Line ~441: `if isTaiko { ... }` — L2 block creation path.
- Line ~671: `(api.eth.BlockChain().Config().Taiko && params.WithdrawalsHash == (common.Hash{}))` — Taiko withdrawals-hash handling.
- Line ~944: `// CHANGE(taiko): allow passing the executable data with txHash instead of all transactions.`
- Line ~949: `params.TaikoBlock = api.eth.BlockChain().Config().Taiko`
- Line ~950: `if api.eth.BlockChain().Config().Taiko && params.Transactions == nil && params.Withdrawals == nil { ... }`
- Line ~1030: `// CHANGE(taiko): a block that has the same timestamp as its parents is ... allowed in Taiko protocol.`
- Line ~1032: `if api.eth.BlockChain().Config().Taiko { ... }`

All must survive.

- [ ] **Step 15.2: Resolve `eth/catalyst/api.go`**

Resolution walkthrough:
1. Adopt upstream's Engine API v4 structure (new method signatures, blob handling, new forkchoice paths).
2. Reinsert the `isTaiko` variable derivation in each handler that had one.
3. Preserve the reorg-allowed branch (`else if isTaiko`) in forkchoice-updated handling.
4. Preserve the L2-block-creation branch in NewPayloadV* handlers.
5. Preserve the withdrawals-hash Taiko carve-out.
6. Preserve the `params.TaikoBlock = ...` assignment and the txHash-with-nil-transactions path for L2 blocks.
7. Preserve the timestamp-equality-allowed branch.

If upstream added a new NewPayloadV4 / V5 handler that doesn't exist on current taiko branch, add Taiko branches to it symmetrically with the older handlers.

- [ ] **Step 15.3: Resolve `eth/catalyst/queue.go`**

If conflicted, preserve any Taiko-specific queue behavior. Most likely upstream only changed payload queue internals, and Taiko doesn't touch them.

- [ ] **Step 15.4: Resolve `beacon/engine/types.go`**

Preserve the `TaikoBlock bool` field (or equivalent) on `ExecutableData` / `PayloadAttributes`. Adopt upstream's new payload attribute fields (e.g., blob sidecar fields) as added fields — they stay nil/zero on Taiko payloads.

- [ ] **Step 15.5: Verify and stage**

```bash
grep -n '^<<<<<<< ' eth/catalyst/api.go eth/catalyst/queue.go beacon/engine/types.go
git add eth/catalyst/ beacon/engine/types.go
```

- [ ] **Step 15.6: Compile the engine packages**

```bash
go build ./eth/catalyst/... ./beacon/...
```

Expected: clean. Errors from unresolved `eth/taiko_api_backend.go` are OK — that's Tier 6.

- [ ] **Step 15.7: Run catalyst tests**

```bash
go test ./eth/catalyst/... -v
```

Expected: all tests pass including Taiko-specific ones. Any `NewPayload` or `ForkchoiceUpdated` test failure on a non-Taiko test indicates the reconciliation broke the upstream path — fix immediately.

- [ ] **Step 15.8: Commit**

```bash
git commit -m "chore(upstream-merge): reconcile Engine API for v1.17.2

Tier 5 sub-task of the v1.17.2 upstream merge. Adopts upstream's Engine
API v4 payload handlers, blob/sidecar handling, and forkchoice updated
structure. Preserves Taiko:

- reorg-allowed L2 semantics in ForkchoiceUpdated
- L2 block creation branch in NewPayload
- withdrawals-hash Taiko carve-out
- TaikoBlock payload attribute propagation
- txHash-with-nil-transactions path for L2 blocks
- timestamp-equality-allowed branch in new-payload validation

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

**Exit gate for Phase 6**: `go build ./miner/... ./eth/catalyst/... ./beacon/...` and `go test ./miner/... ./eth/catalyst/...` all pass.

---

## Phase 7: Tier 6 reconcile — RPC surface

### Task 16: Resolve RPC-related shared files

**Files:**
- Resolve: `eth/api_backend.go`, `eth/api_debug.go`, `eth/state_accessor.go`, `eth/tracers/api.go`, `rpc/server.go`, `rpc/json.go`

**Strategy**: Adopt upstream RPC fixes. Taiko markers in these files are usually small carve-outs (tracing support for Taiko blocks, taiko debug endpoints, auth namespace registration in rpc/server).

- [ ] **Step 16.1: Search for Taiko markers**

```bash
grep -n 'CHANGE.taiko\|config\.Taiko\|taiko' eth/api_backend.go eth/api_debug.go eth/state_accessor.go eth/tracers/api.go rpc/server.go rpc/json.go
```

Note every marker — each one must survive.

- [ ] **Step 16.2: Resolve each file**

For each conflicted file, adopt upstream structure and reinsert Taiko markers in the semantically equivalent location.

Special attention for `rpc/server.go` and `rpc/json.go`: the `29fa1d998` fix (secp256k1 + ECIES handshake curve handling) is a recent Taiko change — it may still need to apply to the new upstream code. If upstream's new code has a similar crypto validation, check that the fix is still there.

- [ ] **Step 16.3: Verify and stage**

```bash
grep -n '^<<<<<<< ' eth/api_backend.go eth/api_debug.go eth/state_accessor.go eth/tracers/api.go rpc/server.go rpc/json.go
git add eth/api_backend.go eth/api_debug.go eth/state_accessor.go eth/tracers/api.go rpc/server.go rpc/json.go
```

### Task 17: Resolve `eth/taiko_api_backend.go` and `ethclient/taiko_api.go`

**Files:**
- Resolve or adapt: `eth/taiko_api_backend.go`, `ethclient/taiko_api.go`

- [ ] **Step 17.1: Attempt to build**

```bash
go build ./eth/ ./ethclient/
```

- [ ] **Step 17.2: For each compile error, adapt to new upstream interfaces**

Common changes:
- Upstream renamed `eth.Ethereum` helper methods.
- Upstream changed `rawdb` function signatures.
- Upstream changed the `api.Backend` interface.

Do NOT reintroduce removed upstream symbols.

- [ ] **Step 17.3: Rebuild until clean**

```bash
go build ./eth/ ./ethclient/ ./rpc/
```

- [ ] **Step 17.4: Run the RPC compatibility test**

```bash
go test ./eth/... -run 'TestTaikoAPIBackendFullSurface|TestL1OriginJSONShape|TestTaikoAuthBackendExposesBatchLookupMethods|TestTaikoAPIBackendHidesBatchLookupMethods' -v
```

Expected: all four tests pass. Failures here are compatibility breaks — an RPC method was silently removed, renamed, or had its JSON shape changed. BLOCK the merge and fix.

- [ ] **Step 17.5: Run the full eth, ethclient, and rpc package tests**

```bash
go test ./eth/... ./ethclient/... ./rpc/...
```

Expected: all pass.

- [ ] **Step 17.6: Stage and commit**

```bash
git add eth/ ethclient/ rpc/
git commit -m "chore(upstream-merge): reconcile RPC surface preserving taikoAuth_ namespace

Tier 6 of the v1.17.2 upstream merge. All 13 documented taiko_* and
taikoAuth_* RPC methods remain registered. L1Origin JSON marshaling
including hexutil.Bytes signature override preserved. Taiko RPC
compatibility surface test passes.

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

**Exit gate**: `go test ./eth/... ./ethclient/... ./rpc/...` all pass, including all four RPC compatibility tests.

---

## Phase 8: Tier 7 reconcile — persistence and genesis

### Task 18: Resolve `core/rawdb/taiko_l1_origin.go` and related files

**Files:**
- Resolve or adapt: `core/rawdb/taiko_l1_origin.go`, `core/rawdb/gen_taiko_l1_origin.go`, `core/rawdb/taiko_l1_origin_test.go`

- [ ] **Step 18.1: Check if these files are in the conflict list**

```bash
git status --short core/rawdb/
```

These files are Taiko-exclusive. Text conflicts are unlikely; compile failures are possible if upstream changed the `rawdb` package's database interface.

- [ ] **Step 18.2: Attempt to build**

```bash
go build ./core/rawdb/
```

- [ ] **Step 18.3: For each compile error, adapt**

Common adaptations:
- Upstream may have changed `ethdb.KeyValueStore` methods.
- Upstream may have changed how freezer tables register.
- Upstream may have added new batch APIs.

Port forward; do NOT reintroduce removed APIs.

- [ ] **Step 18.4: Verify L1Origin struct hasn't drifted**

```bash
grep -A 20 'type L1Origin struct' core/rawdb/taiko_l1_origin.go
```

Expected fields: `BlockID`, `L2BlockHash`, `L1BlockHeight`, `L1BlockHash`, `Signature` (hexutil.Bytes). Any added/removed field is a schema change that breaks existing Taiko databases.

- [ ] **Step 18.5: Regenerate gen_taiko_l1_origin.go only if the struct changed**

If (and only if) the `L1Origin` struct was modified, run:

```bash
go generate ./core/rawdb/
```

If the struct was NOT modified, do NOT run `go generate` — skip this step.

- [ ] **Step 18.6: Run the L1Origin tests**

```bash
go test ./core/rawdb/... -run L1Origin -v
```

Expected: all pass.

- [ ] **Step 18.7: Run the RPC compat test again to re-verify L1Origin JSON shape**

```bash
go test ./eth/... -run TestL1OriginJSONShape -v
```

Expected: pass.

- [ ] **Step 18.8: Stage and commit**

```bash
git add core/rawdb/
git commit -m "chore(upstream-merge): reconcile L1Origin persistence for v1.17.2

Tier 7 of the v1.17.2 upstream merge. Adapts core/rawdb/taiko_l1_origin.go
to upstream's v1.17.2 rawdb interfaces. L1Origin struct fields unchanged,
so existing Taiko node databases remain byte-compatible.

Ref: docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md"
```

**Exit gate**: `go test ./core/rawdb/... -run L1Origin` passes and L1Origin struct is unchanged.

---

## Phase 9: Test restoration and full-repo build

### Task 19: Resolve remaining conflicted test files

**Files:**
- Resolve: all conflicted `*_test.go` files from the Tier 8 list in the triage document

- [ ] **Step 19.1: List remaining conflicts**

```bash
git diff --name-only --diff-filter=U
```

At this point, only test files should remain conflicted (plus possibly a few stragglers).

- [ ] **Step 19.2: For each conflicted test file**

Resolve by:
1. Adopting upstream's new test setup (new fixtures, new helpers).
2. Preserving Taiko-specific test cases.
3. If a Taiko test uses a helper that upstream removed, port the test to the new upstream helper.

- [ ] **Step 19.3: Full-repo build**

```bash
go build ./...
```

Expected: clean compilation across the entire repository. Any remaining compile error is a missed adaptation — fix immediately.

- [ ] **Step 19.4: Stage remaining files**

```bash
git add .
```

### Task 20: Run the full test suite and root-cause any failures

- [ ] **Step 20.1: Run `make test` and capture output**

```bash
make test 2>&1 | tee /tmp/merge-test-output.log
echo "exit=$?" >> /tmp/merge-test-output.log
tail -200 /tmp/merge-test-output.log
```

- [ ] **Step 20.2: For each failing test**

1. Identify the failing package and test name from the log.
2. Re-run just that test with `-v` to get the full error:

```bash
go test ./path/to/package -run TestName -v
```

3. Root-cause the regression:
   - Is it a missed `CHANGE(taiko):` marker? (Grep the file for `CHANGE(taiko)` and compare against the baseline log.)
   - Is it a behavior drift in reconciled code? (Compare the new code path to `git show taiko:<file>` for the original.)
   - Is it an upstream test that now assumes an API not present in the Taiko fork? (Likely an adaptation is needed in a non-Taiko file.)

4. Fix the ROOT CAUSE in the source file. Do NOT skip the test. Do NOT add build tags. Do NOT mark as `t.Skip`.

5. Re-run until passing.

- [ ] **Step 20.3: Repeat until `make test` exits 0**

Every fix goes in its own commit with a message like:

```
fix(upstream-merge): restore <thing> in <file>

Root-caused during post-merge test run. Upstream <change> removed/renamed
<X>, which broke <test>. Fix: <one-line summary>.
```

Commit example:

```bash
git add path/to/fixed/file.go
git commit -m "fix(upstream-merge): restore anchor tx gas limit in state processor"
```

- [ ] **Step 20.4: Compare against baseline test log**

```bash
diff <(grep 'FAIL\|PASS\|ok\|---' /tmp/merge-test-output.log) \
     <(grep 'FAIL\|PASS\|ok\|---' docs/superpowers/plans/2026-04-11-baseline-test.log) \
     | head -60
```

Investigate any test that passes on baseline but is missing or failing on merge.

### Task 21: Run `go generate` and commit any regenerated files

- [ ] **Step 21.1: Run go generate**

```bash
go generate ./...
```

- [ ] **Step 21.2: Check for unexpected changes**

```bash
git status
```

- [ ] **Step 21.3: Review each regenerated file**

For each changed file:
- Is it a legitimate regeneration (e.g., `gen_*.go`, ABI bindings)?
- Or is it an unexpected diff from a broken generator?

If legitimate, stage and commit:

```bash
git add <files>
git commit -m "chore(upstream-merge): regenerate <file> for v1.17.2"
```

If unexpected, STOP and investigate.

- [ ] **Step 21.4: Run `make test` one more time**

```bash
make test
```

Expected: exit 0.

### Task 22: Finalize the merge commit

- [ ] **Step 22.1: Verify no conflict markers anywhere**

```bash
grep -rn '^<<<<<<< \|^======= \|^>>>>>>> ' --include='*.go' --include='*.md' --include='*.json' .
```

Expected: no output.

- [ ] **Step 22.2: Finalize the merge**

```bash
git commit --no-edit
```

This creates the merge commit with the default message listing upstream changes. If you want a cleaner message, use:

```bash
git commit -m "Merge go-ethereum v1.17.2 upstream (be4dc0c4b) into taiko

See docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md
for the full integration plan. Reconciled in seven tiers with per-tier
exit gates; Shanghai-cap invariant and RPC compatibility surface tests
pass. All Taiko features preserved."
```

**Exit gate**: `make test` exits 0 and no conflict markers remain in the worktree.

---

## Phase 10: Finalization and PR prep

### Task 23: Update documentation version references

**Files:**
- Modify: `README.md`, possibly `params/version.go` (only if Taiko maintains a version string)

- [ ] **Step 23.1: Find and update upstream version references**

```bash
grep -rn 'v1\.15\.5\|go-ethereum.*1\.15' README.md AGENTS.md CLAUDE.md 2>/dev/null
```

For each match, evaluate whether it should now say `v1.17.2`. Most likely `CLAUDE.md` has a line like `This is a fork of go-ethereum v1.15.5` that needs updating to `v1.17.2`.

- [ ] **Step 23.2: Update CLAUDE.md**

```bash
grep -n 'v1.15.5' CLAUDE.md
```

If matched, edit to say `v1.17.2`.

- [ ] **Step 23.3: Check if Taiko maintains its own version metadata**

```bash
grep -rn 'var Version\|const Version' params/version.go 2>/dev/null
```

If Taiko has its own version string (separate from upstream's), it does not need bumping for this merge — that's a release concern. Skip if in doubt.

- [ ] **Step 23.4: Commit doc updates**

```bash
git add README.md CLAUDE.md AGENTS.md
git commit -m "docs: update upstream go-ethereum base to v1.17.2"
```

### Task 24: Run lint and fix findings

- [ ] **Step 24.1: Run make lint**

```bash
make lint 2>&1 | tee /tmp/lint.log
```

- [ ] **Step 24.2: For each lint finding**

Fix in source — do not suppress. Common findings after a merge:
- Unused imports after struct field removal.
- Shadowed variables in new code paths.
- Missing doc comments on newly exported symbols.

Commit fixes individually:

```bash
git add <file>
git commit -m "chore(upstream-merge): fix lint finding in <file>"
```

- [ ] **Step 24.3: Re-run lint until clean**

```bash
make lint
```

Expected: clean exit.

### Task 25: Final build and test pass

- [ ] **Step 25.1: Final make geth**

```bash
make geth
```

Expected: build succeeds, produces `./build/bin/geth`.

- [ ] **Step 25.2: Final make test**

```bash
make test
```

Expected: exit 0.

- [ ] **Step 25.3: Final verification gates**

Run each verification gate explicitly:

```bash
# Gate 1: Build
make geth && go build ./...

# Gate 3: Taiko regression
go test ./consensus/taiko/... ./consensus/misc/...
go test ./eth/... -run Taiko
go test ./ethclient/...
go test ./miner/... -run Taiko
go test ./core/rawdb/... -run L1Origin
go test ./params/...

# Gate 4: Shanghai-cap invariant
go test ./params/... -run TestTaikoChainConfigShanghaiCap -v

# Gate 5: RPC compatibility
go test ./eth/... -run 'TestTaikoAPIBackendFullSurface|TestL1OriginJSONShape' -v

# Gate 7: Lint
make lint

# Gate 8: Merge base
git merge-base upstream-v1.17.2-merge be4dc0c4b
# Expected: 4263936a0402e2b175a366091ed3cb54133bc6a8
```

Each command must succeed / produce the expected output. If any fails, return to the relevant tier's tasks and fix.

### Task 26: Push the branch and open the PR

- [ ] **Step 26.1: Push the integration branch**

```bash
git push -u origin upstream-v1.17.2-merge
```

- [ ] **Step 26.2: Open the PR**

```bash
cd /Users/davidcai/taiko/taiko-geth  # back to main checkout for gh CLI
gh pr create --base taiko --head upstream-v1.17.2-merge \
  --title "feat(repo): \`go-ethereum\` v1.17.2 upstream merge" \
  --body "$(cat <<'EOF'
## Summary

Merges upstream go-ethereum v1.15.5 → v1.17.2 (`be4dc0c4b`) into taiko.
Preserves all Taiko features and keeps TaikoChainConfig Shanghai-capped.
Follows the `364acd00d` big-bang upstream-merge pattern.

## Approach

Seven tiers of subsystem reconciliation on an integration branch, each
committed as a stacked subsystem commit:

1. Shared infrastructure (cmd, node, build, ethconfig)
2. Params and chain config (Shanghai-cap preserved)
3. Core execution and tx types (config.Taiko branches preserved)
4. Consensus engine (Engine interface adapted, EIP-4396 unchanged)
5. Engine API, miner, payload building (hardest tier — split into sub-commits)
6. RPC surface (taiko_* and taikoAuth_* namespaces preserved)
7. Persistence and genesis (L1Origin byte-compatible)
8. Test restoration and full-repo fixes

## Verification gates (all passing)

- [x] `make geth` and `go build ./...`
- [x] `make test` exits 0
- [x] Taiko regression suite (consensus, miner, eth, ethclient, rawdb, params)
- [x] `TestTaikoChainConfigShanghaiCap` (Shanghai-cap invariant)
- [x] `TestTaikoAPIBackendFullSurface` + `TestL1OriginJSONShape` (RPC compat)
- [x] `make lint`
- [x] `git merge-base` resolves to v1.15.5 (graft verified)

## Design doc

`docs/superpowers/specs/2026-04-11-taiko-geth-v1-17-2-upstream-merge-design.md`

## Post-merge

Live-network devnet validation is intentionally out of scope for this PR.
Follow-up work on a Taiko devnet will exercise runtime behavior (gas
accounting, fee distribution, reorg handling) that unit tests cannot
cover.

## Test plan
- [ ] CI green
- [ ] Reviewer walkthrough of each stacked subsystem commit
- [ ] Manual spot-check of L1Origin DB compatibility with a prior-build node
- [ ] Post-land: devnet runtime validation (tracked separately)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 26.3: Record the PR URL**

The `gh pr create` command prints the URL. Paste it into the plan document for future reference:

```bash
echo "PR: <paste URL here>" >> docs/superpowers/plans/2026-04-11-taiko-geth-v1-17-2-upstream-merge.md
```

**Exit gate**: PR open with all CI green. The merge is complete.

---

## Post-merge: clean up the graft

After the PR lands on `taiko` via squash-merge, the graft ref in your local repo is no longer needed. Remove it to avoid confusion:

- [ ] **Step 27.1: Remove the graft ref**

```bash
git replace -d 364acd00d
```

- [ ] **Step 27.2: Verify it's gone**

```bash
git replace --list
```

Expected: the 364acd00d entry is no longer listed.

- [ ] **Step 27.3: Clean up the worktree**

```bash
cd /Users/davidcai/taiko/taiko-geth
git worktree remove .worktrees/upstream-v1.17.2-merge
```

---

## Rollback procedures

**Mid-phase rollback** (a single tier went wrong):

```bash
# Find the commit sha of the prior phase exit
git log --oneline -20
# Reset to just before the broken phase
git reset --hard <prior-phase-sha>
```

The worktree is now back at the prior phase's exit state. Re-attempt the phase.

**Abandon the merge entirely**:

```bash
git merge --abort  # if still in merge-in-progress
# OR
git reset --hard taiko  # if merge was committed
```

Then remove the worktree if wanted:

```bash
cd /Users/davidcai/taiko/taiko-geth
git worktree remove --force .worktrees/upstream-v1.17.2-merge
git branch -D upstream-v1.17.2-merge
git replace -d 364acd00d  # also drop the graft
```

**Post-land rollback** (regression found after the squash-merge lands on `taiko`):

```bash
git switch taiko
git pull
git revert <squash-sha>
git push
```

The single squash commit revert rolls back the entire merge. All Taiko PRs merged after the upstream merge are preserved.

---

## Notes for the executor

- **Always work inside the worktree** (`.worktrees/upstream-v1.17.2-merge`) unless a step says otherwise.
- **Do not run `git merge --abort` during Phase 1–8** unless you have decided to abandon the whole merge. Abort throws away all in-progress resolution.
- **Commit after every phase** — the stacked commits are your checkpoint mechanism. If one fails, you can revert just that phase.
- **If a grep for a `CHANGE(taiko):` marker returns nothing after resolution**, the marker was lost. Go back and reinsert it before committing.
- **Trust the compiler** — after each tier, the compiler tells you what you missed. Use its errors as a to-do list.
- **Root-cause test failures** — never skip or quarantine a failing test to get the merge through. If you can't root-cause it, STOP and escalate.
- **The graft is local-only** — do not push it. `git push` will not upload replace refs by default, so this is usually automatic.
- **If the merge base gate fails after Step 6.3**, the graft command used the wrong SHAs. Verify `4263936a0` is upstream v1.15.5 (`git log 4263936a0 -1` should say `version: release v1.15.5 stable`) and `03f614fb2` is the original parent of `364acd00d` (`git log 03f614fb2 -1` should say `chore(core): revert []*ethapi.RPCTransaction changes in miner (#394)`).
