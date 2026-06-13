# go-ethereum v1.17.3 Upstream Merge — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to
> implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. This is a **git merge**, not a feature build — the "test" is a green
> build + targeted test packages, and the unit of correctness is *behavior
> preserved*. **Task 10 is a hard human-review checkpoint: do NOT commit the merge
> until the user approves the 3 consensus-critical diffs.**

**Goal:** Re-base taiko-geth from go-ethereum v1.17.2 to v1.17.3 via a
behavior-preserving merge — resolve conflicts, adapt Taiko's ZK-gas/anchor logic to
upstream's new `GasBudget` gas vector, build green, pass targeted tests, bump the
geth-base version.

**Architecture:** Graft v1.17.2 as a merge-base (empty `-s ours` merge) so
`git merge v1.17.3` presents only the true v1.17.2→v1.17.3 delta (7 conflicts), not
the whole v1.15.5→v1.17.3 history that the squash-merged PR #541 left stale.
Resolve conflicts preserving every `CHANGE(taiko):` block, then compile-fix Taiko's
ZK-gas helpers onto `GasBudget.RegularGas`.

**Tech Stack:** Go 1.23+, git, go-ethereum fork. No new dependencies.

---

## Conventions & pinned commits

- Branch: `feat/go-ethereum-v1.17.3-upstream-merge` (already created; spec already
  committed here).
- Pinned SHAs (the local `v1.17.x` tags are bogus — **always use SHAs**):
  - v1.17.2 = `be4dc0c4be2fe316dbdd0a73e48421f64978232f`
  - v1.17.3 = `117e067f0f0bae1a17082321f224dedb6765b10f`
- Shell is **zsh**: `$VAR:path` triggers history-modifier parsing. In `git show`,
  use literal SHAs, e.g. `git show 117e067f...:core/vm/evm.go` — not `$V3:...`.
- `GasBudget` (new in v1.17.3, `core/vm/gascosts.go`):
  ```go
  type GasBudget struct {
      RegularGas uint64 // execution + state gas usage
      StateGas   uint64 // state gas reservoir
  }
  func NewGasBudget(gas uint64) GasBudget { return GasBudget{RegularGas: gas} }
  ```
  **Mapping contract:** Taiko's existing gas semantics → `GasBudget.RegularGas`.
  The `StateGas` dimension is upstream-only and is NOT wired into Taiko ZK-gas here.

---

## Task 1: Confirm clean state & graft the v1.17.2 base

**Files:** none (git history only)

- [ ] **Step 1: Confirm on the right branch with a clean tree**

Run:
```bash
cd /Users/davidcai/taiko/taiko-geth
git branch --show-current
git status --short
```
Expected: branch `feat/go-ethereum-v1.17.3-upstream-merge`; empty status (only the
two committed spec files exist, nothing uncommitted).

- [ ] **Step 2: Confirm both upstream commits are present locally**

Run:
```bash
git cat-file -t be4dc0c4be2fe316dbdd0a73e48421f64978232f
git cat-file -t 117e067f0f0bae1a17082321f224dedb6765b10f
```
Expected: `commit` for both. (If v1.17.3 is missing: `git fetch upstream 117e067f0f0bae1a17082321f224dedb6765b10f`.)

- [ ] **Step 3: Graft v1.17.2 as an ancestor (empty merge, changes no files)**

Run:
```bash
git merge -s ours be4dc0c4be2fe316dbdd0a73e48421f64978232f \
  -m "chore: record go-ethereum v1.17.2 as merge base

Squash-merge of PR #541 left the merge-base at v1.15.5. Recording v1.17.2
as an ancestor so the v1.17.3 merge presents only the true delta."
```
Expected: a new commit; `git show --stat HEAD` shows **0 files changed**.

- [ ] **Step 4: Verify the graft fixed the merge-base**

Run:
```bash
git merge-base HEAD 117e067f0f0bae1a17082321f224dedb6765b10f
```
Expected: `be4dc0c4be2fe316dbdd0a73e48421f64978232f` (v1.17.2). If it prints the
v1.15.5 SHA `4263936a...`, STOP — the graft did not take.

---

## Task 2: Begin the v1.17.3 merge (expect conflicts)

**Files:** none yet (starts the merge)

- [ ] **Step 1: Start the merge**

Run:
```bash
git merge --no-commit --no-edit 117e067f0f0bae1a17082321f224dedb6765b10f
```
Expected: `Automatic merge failed; fix conflicts and then commit the result.`
(`--no-commit` keeps the merge open for the review checkpoint in Task 10.)

- [ ] **Step 2: Snapshot the conflict set**

Run:
```bash
git diff --name-only --diff-filter=U | sort
```
Expected exactly these 7:
```
appveyor.yml
beacon/engine/gen_ed.go
beacon/engine/types.go
core/state_transition.go
core/vm/evm.go
core/vm/interpreter.go
params/config.go
```
If the set differs, re-scope before resolving — do not proceed blindly.

---

## Task 3: Resolve the mechanical conflicts (4 files)

**Files:**
- Modify: `appveyor.yml` (delete), `beacon/engine/types.go`, `params/config.go`
- Regenerate: `beacon/engine/gen_ed.go`

- [ ] **Step 1: appveyor.yml — accept upstream's deletion**

Upstream removed appveyor (#34720); Taiko uses gitea/github workflows.
Run:
```bash
git rm appveyor.yml
```
Expected: `appveyor.yml` removed and staged.

- [ ] **Step 2: beacon/engine/types.go — keep both sides**

The conflict is in the `ExecutableData` struct: upstream changed the `SlotNumber`
JSON tag to `slotNumber,omitempty`; Taiko added fields right after it. Resolution
= take upstream's `SlotNumber` line, then keep all Taiko additions:
```go
	SlotNumber    *uint64             `json:"slotNumber,omitempty"`

	TxHash           common.Hash `json:"txHash"`           // CHANGE(taiko): allow passing txHash directly instead of transactions list
	WithdrawalsHash  common.Hash `json:"withdrawalsHash"`  // CHANGE(taiko): allow passing WithdrawalsHash directly instead of withdrawals
	HeaderDifficulty *big.Int    `json:"headerDifficulty"` // CHANGE(taiko): Unzen header difficulty for hash-stable round-trips
	TaikoBlock       bool        // CHANGE(taiko): whether this is a Taiko L2 block, only used by ExecutableDataToBlock
}

// CHANGE(taiko): HeaderDifficultyOrZero returns HeaderDifficulty when set, otherwise common.Big0.
func (data *ExecutableData) HeaderDifficultyOrZero() *big.Int {
	if data.HeaderDifficulty != nil {
		return new(big.Int).Set(data.HeaderDifficulty)
	}
	return common.Big0
}
```
Edit the file to remove the `<<<<<<< / ======= / >>>>>>>` markers, keeping the
above. Then `git add beacon/engine/types.go`.

- [ ] **Step 3: params/config.go — keep Taiko predicates, adopt upstream rename**

Conflict: Taiko's fork predicates abut upstream's `IsVerkleGenesis`→`IsUBTGenesis`
rename. Both methods are arg-less; upstream changed the body to read the new
`EnableUBTAtGenesis` config field (already present in the auto-merged struct). Keep
all Taiko helpers AND adopt upstream's renamed method verbatim:
```go
// CHANGE(taiko): IsOntake returns whether num is either equal to the Ontake fork block or greater.
func (c *ChainConfig) IsOntake(num *big.Int) bool { return isBlockForked(c.OntakeBlock, num) }

// CHANGE(taiko): IsPacaya returns whether num is either equal to the Pacaya fork block or greater.
func (c *ChainConfig) IsPacaya(num *big.Int) bool { return isBlockForked(c.PacayaBlock, num) }

// CHANGE(taiko): IsShasta returns whether time is either equal to the Shasta fork time or greater.
func (c *ChainConfig) IsShasta(time uint64) bool { return isTimestampForked(c.ShastaTime, time) }

// CHANGE(taiko): IsUnzen returns whether time is either equal to the Unzen fork time or greater.
func (c *ChainConfig) IsUnzen(time uint64) bool { return isTimestampForked(c.UnzenTime, time) }

// IsUBTGenesis checks whether the verkle fork is activated at the genesis block.
func (c *ChainConfig) IsUBTGenesis() bool {
	return c.EnableUBTAtGenesis
}
```
Then `git add params/config.go`.

- [ ] **Step 3b: Rename the one Taiko caller of IsVerkleGenesis**

`core/genesis.go:475` calls the old name. Update it:
```go
	return g.Config.IsUBTGenesis()
```
(The only non-test caller; confirm with
`grep -rn "IsVerkleGenesis" --include='*.go' . | grep -v _test` → should be empty
after the edit.) Then `git add core/genesis.go`. This also surfaces in Task 8's
build if missed here.

- [ ] **Step 4: beacon/engine/gen_ed.go — take upstream, regenerate later**

`gen_ed.go` is generated (gencodec). Take upstream's version now for a compiling
baseline; it will be regenerated after `types.go` is final.
Run:
```bash
git checkout --theirs beacon/engine/gen_ed.go && git add beacon/engine/gen_ed.go
```
(Regeneration happens in Task 8 Step 3.)

---

## Task 4: Resolve core/state_transition.go (consensus-critical — uint256 Message)

**Files:** Modify: `core/state_transition.go` (3 conflict hunks)

Upstream commit #34934 made `core.Message` use `uint256`. Re-apply Taiko's anchor
logic against the new types.

- [ ] **Step 1: Hunk 1 — Message construction**

Keep upstream's `BlobGasFeeCap` (now a local uint256 `blobGasFeeCap`) and re-add
Taiko's `IsAnchor`:
```go
		BlobGasFeeCap:         blobGasFeeCap,   // upstream (uint256)
		IsAnchor:              tx.IsAnchor(),   // CHANGE(taiko)
```

- [ ] **Step 2: Hunk 2 — buy-gas balance check**

`balanceCheck` is now `*uint256.Int` (upstream). Preserve Taiko's anchor skip:
```go
	// CHANGE(taiko): anchor txs skip the balance check.
	if st.msg.IsAnchor {
		balanceCheck = common.U2560
		mgval = common.U2560 // VERIFY: if upstream's mgval is still *big.Int, use common.Big0 instead
	}
	if have, want := st.state.GetBalance(st.msg.From), balanceCheck; have.Cmp(want) < 0 {
		return fmt.Errorf("%w: ...", ErrInsufficientFunds, st.msg.From.Hex(), have, want)
	}
```
The compiler (Task 8) will confirm whether `mgval` is `*uint256.Int` (use
`common.U2560`) or `*big.Int` (use `common.Big0`). Drop Taiko's old
`uint256.FromBig(balanceCheck)` conversion — it is unnecessary now that
`balanceCheck` is already uint256.

- [ ] **Step 3: Hunk 3 — gas refund**

Keep Taiko's `if !st.msg.IsAnchor` guard; adopt upstream's `GasBudget` +
uint256 `GasPrice` body:
```go
	// CHANGE(taiko): anchor txs receive no gas refund.
	if !st.msg.IsAnchor {
		remaining := uint256.NewInt(st.gasRemaining.RegularGas)
		remaining.Mul(remaining, st.msg.GasPrice)
		st.state.AddBalance(st.msg.From, remaining, tracing.BalanceIncreaseGasReturn)

		if st.evm.Config.Tracer != nil && st.evm.Config.Tracer.OnGasChange != nil && st.gasRemaining.RegularGas > 0 {
			st.evm.Config.Tracer.OnGasChange(st.gasRemaining.RegularGas, 0, tracing.GasChangeTxLeftOverReturned)
		}
	}
```

- [ ] **Step 4: Stage**

Run: `git add core/state_transition.go` (do NOT commit — Task 10 reviews first).

---

## Task 5: Resolve core/vm/evm.go (consensus-critical — GasBudget × ZK-gas)

**Files:** Modify: `core/vm/evm.go` (10 conflict hunks)

Reference the exact upstream bodies as you go:
`git show 117e067f0f0bae1a17082321f224dedb6765b10f:core/vm/evm.go`.

- [ ] **Step 1: Hunk 1 — EVM struct fields**

Keep upstream's new `arena *stackArena` field AND Taiko's ZK-gas fields
(`zkGasTracker *ZkGasStepTracker`, the `zkGasErr error` slot and its comment).

- [ ] **Step 2: Hunks for Call / CallCode / DelegateCall / StaticCall / create — signatures**

For each function, adopt upstream's `GasBudget` signature and re-insert Taiko's
spawn-marking line. Example (`Call`):
```go
func (evm *EVM) Call(caller common.Address, addr common.Address, input []byte, gas GasBudget, value *uint256.Int) (ret []byte, leftOverGas GasBudget, err error) {
	// CHANGE(taiko): mark CALL-family opcodes as spawned at dispatch entry.
	evm.markPendingCallSpawn()
	...
```
`create` uses `evm.markPendingCreateSpawn()`. `StaticCall` has no `value` param.

- [ ] **Step 3: Precompile-run hunks (one per call function) — adopt upstream call + keep ZK-gas charge**

Upstream changed `RunPrecompiledContract` to
`RunPrecompiledContract(stateDB, p, addr, input, gas GasBudget, logger, rules)`
and passes `evm.StateDB` + `evm.chainRules` directly — so **drop Taiko's old
`var stateDB StateDB; if evm.chainRules.IsAmsterdam {...}` conditional** (upstream
now handles that via `rules`). Keep Taiko's zk-gas charge, feeding `.RegularGas`:
```go
		gasBeforePrecompile := gas // CHANGE(taiko): capture for zk gas accounting (GasBudget)
		ret, gas, err = RunPrecompiledContract(evm.StateDB, p, addr, input, gas, evm.Config.Tracer, evm.chainRules)
		// CHANGE(taiko): charge precompile zk gas; on over-limit set the sticky
		// error and route through the standard err-cleanup path below.
		if evm.Config.ZkGasMeter != nil {
			if zkErr := evm.Config.ZkGasMeter.ChargePrecompile(addr, precompileZkGasUsed(gasBeforePrecompile.RegularGas, gas.RegularGas, err)); zkErr != nil {
				evm.setZkGasErr()
				err = zkErr
			}
		}
```

- [ ] **Step 4: Stage**

Run: `git add core/vm/evm.go` (do NOT commit yet).

---

## Task 6: Resolve core/vm/interpreter.go (consensus-critical — GasBudget × ZK-gas OOG)

**Files:** Modify: `core/vm/interpreter.go` (2 conflict hunks + 1 compile-fix line)

- [ ] **Step 1: Hunk 1 — static-cost OOG check**

Use upstream's `contract.Gas.RegularGas < cost`; keep Taiko's zk-gas OOG block
inside it:
```go
		if contract.Gas.RegularGas < cost {
			if evm.zkGasTracker != nil && evm.zkGasErr == nil { // CHANGE(taiko)
				evm.zkGasTracker.Begin(evm.depth, byte(op), gasBefore)
				if zkErr := evm.zkGasTracker.FinishAndCharge(evm.depth, 0); zkErr != nil {
					evm.setZkGasErr()
					return nil, ErrZkGasLimitExceeded
				}
			}
			return nil, ErrOutOfGas // (keep upstream's existing OOG return)
		}
```

- [ ] **Step 2: Hunk 2 — dynamic-cost OOG check**

Use upstream's `contract.Gas.RegularGas < dynamicCost.RegularGas`; keep Taiko's
zk-gas OOG block (with `zkGasDynamicOOGGasAfter(...)`) inside it.

- [ ] **Step 3: Fix the `gasBefore` capture (line ~178)**

Taiko captures `gasBefore := contract.Gas`; `contract.Gas` is now `GasBudget`, but
`ZkGasStepTracker.Begin/FinishAndCharge` and `zkGas*GasAfter` take `uint64`. Change
the capture to the regular-gas scalar:
```go
		gasBefore := contract.Gas.RegularGas
```
This single change makes every downstream `Begin(... gasBefore)` /
`FinishAndCharge(... gasBefore ...)` call compile unchanged.

- [ ] **Step 4: Stage**

Run: `git add core/vm/interpreter.go` (do NOT commit yet).

---

## Task 7: Resolve dependencies (go.mod / go.sum)

**Files:** Modify: `go.mod`, `go.sum`

`go.mod` auto-merged (not in the conflict set) — it should already carry upstream's
`karalabe/hid`→`ethereum/hid` swap and the `x/crypto`, `x/sync`, `x/text`,
`x/tools`, otel, grpc-gateway bumps. Normalize and verify:

- [ ] **Step 1: Tidy & verify modules**

Run:
```bash
go mod tidy
go mod verify
```
Expected: `go mod verify` prints `all modules verified`. `go mod tidy` may adjust
`go.sum`; stage any changes: `git add go.mod go.sum`.

---

## Task 8: Compile-fix pass

**Files:** As surfaced by the compiler — expected: `core/vm/*.go` Taiko ZK-gas
helpers, possibly `core/state_processor.go`, `params/config.go` call sites.

- [ ] **Step 1: Build everything**

Run:
```bash
go build ./... 2>&1 | tee /tmp/build.log | tail -40
```
Expected initially: FAIL with `GasBudget` / type-mismatch errors in Taiko code that
consumes the changed APIs.

- [ ] **Step 2: Fix the expected adaptation sites**

Apply `.RegularGas` accessors / `NewGasBudget(...)` wrapping where Taiko code
bridges to upstream's `GasBudget`. Known/likely sites:
- `core/vm/interpreter.go` — already fixed in Task 6 Step 3.
- `core/vm/evm.go` precompile charges — already use `.RegularGas` (Task 5 Step 3).
- Any caller passing a bare `uint64` where a `GasBudget` is now required → wrap with
  `vm.NewGasBudget(x)`; any caller reading a `uint64` from a now-`GasBudget` value →
  read `.RegularGas`.
- `IsVerkleGenesis` → `IsUBTGenesis` rename call sites (from Task 3 Step 3).

Do NOT change Taiko ZK-gas *semantics* — only bridge the types. If a fix is not a
pure type bridge, stop and flag it for the Task 10 review.

- [ ] **Step 3: Regenerate gen_ed.go (and siblings) now that types.go is final**

Run:
```bash
go generate ./beacon/engine/
git diff --stat beacon/engine/
```
Expected: `beacon/engine/gen_ed.go` regenerated. Verify it carries the Taiko fields:
```bash
grep -c "TxHash\|WithdrawalsHash\|HeaderDifficulty\|TaikoBlock" beacon/engine/gen_ed.go
```
Expected: non-zero (Taiko fields present). Stage: `git add beacon/engine/`.

- [ ] **Step 4: Re-build until green**

Run:
```bash
go build ./... && echo BUILD_OK
```
Expected: `BUILD_OK`. Repeat Steps 1–3 until green. Stage all fixed files
(`git add -u`), but still **do not commit**.

---

## Task 9: Run targeted tests

**Files:** none (verification)

- [ ] **Step 1: Run the high-risk packages**

Run:
```bash
go test ./consensus/taiko/... ./core/vm/... ./core/ ./miner/... ./eth/catalyst/... ./internal/ethapi/... 2>&1 | tail -40
```
Expected: `ok` for each package. Pay special attention to `core/vm` (ZK-gas tests:
`taiko_zk_gas_*_test.go`) and `core` (state transition / anchor).

- [ ] **Step 2: If failures, triage**

A behavior change in a ZK-gas/anchor test is a resolution bug — fix the resolution,
not the test. A test that references a removed/renamed upstream symbol is an
upstream test-surface change — adapt minimally. Re-run until green. Capture the
final passing output for the Task 10 review and the PR body.

---

## Task 10: 🔴 HUMAN REVIEW CHECKPOINT (do not commit until approved)

**Files:** none (review gate)

- [ ] **Step 1: Present the consensus-critical diffs**

Run and share with the user:
```bash
git diff --cached -- core/state_transition.go core/vm/evm.go core/vm/interpreter.go
```
Summarize for each file: which hunks took upstream, which preserved `CHANGE(taiko)`,
and every `.RegularGas` bridge applied. Include the Task 9 passing-test output.

- [ ] **Step 2: Wait for explicit approval**

Do not proceed to Task 11 until the user approves. Apply any requested changes
(re-stage, re-run Task 8/9), then re-present.

---

## Task 11: Complete the merge commit

**Files:** none (commits the staged merge)

- [ ] **Step 1: Confirm nothing unresolved**

Run: `git diff --name-only --diff-filter=U`
Expected: empty.

- [ ] **Step 2: Commit the merge**

Run:
```bash
git commit --no-edit
```
(Uses git's prepared merge message recording v1.17.3 as the second parent.)
Expected: a merge commit. Verify two parents:
```bash
git rev-list --parents -n 1 HEAD | wc -w   # expect 3
```

---

## Task 12: Bump the geth-base version to 1.17.3

**Files:** Modify: `version/version.go`

- [ ] **Step 1: Edit the patch version**

In `version/version.go`, change `Patch = 2` → `Patch = 3` (leave `Major=1`,
`Minor=17`, `Meta="stable"`). Taiko's own 2.x release version is out of scope.

- [ ] **Step 2: Build sanity + commit**

Run:
```bash
go build ./version/... && git add version/version.go
git commit -m "version: set go-ethereum base version to v1.17.3"
```

---

## Task 13: Strip the superpowers docs (pre-PR cleanup)

**Files:** Delete: `docs/superpowers/` (working artifacts only — not shipped)

- [ ] **Step 1: Remove and commit**

Run:
```bash
git rm -r docs/superpowers
git commit -m "chore: remove working design/plan docs before PR"
```
Expected: `docs/superpowers/` gone. (Per standing preference: only code ships.)

---

## Task 14: Push & open the PR

**Files:** none

- [ ] **Step 1: Final verification before push**

Run:
```bash
go build ./... && echo BUILD_OK
git log --oneline origin/taiko..HEAD
```
Expected: `BUILD_OK`; the log shows the graft, the merge, the version bump, and the
docs-removal commits (no superpowers docs in the tree).

- [ ] **Step 2: Push the branch**

Run: `git push -u origin feat/go-ethereum-v1.17.3-upstream-merge`

- [ ] **Step 3: Open the PR**

Run `gh pr create` with title
`feat(repo): go-ethereum v1.17.3 upstream merge` and a body covering:
- The delta (138 commits, 312 files) and the graft-base technique (one-line
  reviewer note explaining the synthetic `-s ours` commit).
- The 7 conflicts and how each was resolved.
- The **consensus-impact notes** from spec §5 — headline the `GasBudget` gas-vector
  refactor and confirm `StateGas` is upstream-only / not wired into Taiko ZK-gas.
- Targeted-test results.
- **Landing note:** prefer a real merge commit (keeps v1.17.3 a permanent ancestor
  for the next bump); if squash is the convention, the graft is re-applied next time.

---

## Self-review checklist (run before execution)

- **Spec coverage:** graft (§2)→T1; merge (§2)→T2; conflict resolution (§3)→T3–T6;
  deps (§4)→T7; consensus review (§5)→T4–T6,T10; version (§6)→T12; verification
  (§7)→T8–T9; PR & strip (§8)→T13–T14; risks (§9)→T1 Step 4, T10. ✅ all covered.
- **No silent caps:** the 7-conflict set is asserted in T2 Step 2; divergence halts
  execution rather than proceeding blindly.
- **Type consistency:** `GasBudget.RegularGas` (uint64) is the single bridge type
  used in T4–T6 and T8; `precompileZkGasUsed`/`ZkGasStepTracker` keep their uint64
  signatures; `NewGasBudget` is the only uint64→GasBudget constructor referenced.
