# Taiko-geth go-ethereum v1.17.2 Upstream Merge Design

## Summary

Merge upstream go-ethereum `v1.15.5` → `v1.17.2` into `taiko-geth` as one big-bang
merge on an integration branch, preserving all Taiko features and the existing
Shanghai-capped Taiko fork configuration. Land via squash-merge pull request,
matching the `364acd00d` upstream-merge pattern used for the prior v1.15.5 sync.

Upstream target commit is `be4dc0c4b` (full:
`be4dc0c4be2fe316dbdd0a73e48421f64978232f`) — the go-ethereum release commit
for `v1.17.2`. This must be pinned by SHA because the repository has a local
tag named `v1.17.2` that points to a Taiko commit, not the upstream release.

## Goals

1. `taiko` branch tracks upstream go-ethereum at release tag `v1.17.2`.
2. All Taiko features preserved: consensus engine, anchor transactions, L1
   origin tracking, payload building, `taiko_*` and `taikoAuth_*` RPC
   namespaces, genesis data, persistence, golden touch handling, base fee
   sharing, Ontake/Pacaya/Shasta forks.
3. `TaikoChainConfig` remains Shanghai-capped. `CancunTime`, `PragueTime`,
   `OsakaTime`, and any new post-Shanghai fork times remain `nil` for Taiko
   networks. Ontake, Pacaya, and Shasta forks remain intact at current values.
4. `make test` passes on the integration branch across the entire repository.
   Upstream tests run against `TestChainConfig` / `MergedTestChainConfig`
   (which have no `Taiko: true` flag) and exercise full Cancun/Prague/Osaka
   paths normally. Taiko tests run against `TaikoChainConfig` and exercise
   `if config.Taiko { ... }` gated branches.
5. Final history: stacked subsystem commits on the integration branch during
   development, one squash-merge commit on land via GitHub PR.

## Non-goals

- No fork activation changes on Taiko networks. Cancun/Prague/Osaka adoption
  is a separate product decision.
- No selective backport — one full merge of the entire v1.15.5 → v1.17.2 range.
- No live-network runtime validation in this phase. Flagged as follow-up.
- No rewrite of existing Taiko commit history.
- No merge beyond v1.17.2 (not v1.17.3 or v1.17.4).
- No adoption of prior aborted merge attempts (`codex/upstream-v1.17.2-merge`,
  `origin/fix-v1.17.2`, `origin/hotfix-v1.17.1`, etc.) as starting points.
  These are ignored entirely; the merge starts clean from the current `taiko`
  branch.

## Target refs

- Current upstream base: go-ethereum `v1.15.5` (integrated via squash commit
  `364acd00d`).
- Upstream merge target: go-ethereum `v1.17.2`.
- Exact upstream release commit: `be4dc0c4be2fe316dbdd0a73e48421f64978232f`
  (short: `be4dc0c4b`).
- Prior upstream-merge pattern reference: `364acd00d`.
- Integration branch: `upstream-v1.17.2-merge` (created fresh off current
  `taiko`).
- PR target branch: `taiko`.
- Land method: GitHub squash-merge.

## Ancestry and graft

The `364acd00d` v1.15.5 merge was a GitHub squash commit (PR #395), which
collapsed upstream history. As a result, `git merge-base taiko be4dc0c4b`
currently resolves to `9038ba694` (v1.13.14), not v1.15.5. A naive
`git merge be4dc0c4b` would therefore 3-way merge from v1.13.14, re-resolving
four years of upstream changes already integrated by `364acd00d`.

The fix is a local `git replace --graft` that tells git v1.15.5 is a parent
of `364acd00d`. This is a local git concept only; it does not modify commits,
rewrite history, or change SHAs. It only affects how git computes merge bases
in this working copy.

Concrete SHAs for the graft:

- Squash commit to graft: `364acd00d` (Taiko v1.15.5 upstream merge).
- Upstream v1.15.5 release: `4263936a0` — resolved via the local `v1.15.5`
  tag, which is safe because this tag is the real upstream release commit
  (`version: release v1.15.5 stable`), not a Taiko commit.
- Original parent of `364acd00d`: `03f614fb2` (`chore(core): revert
  []*ethapi.RPCTransaction changes in miner (#394)`).

The Phase 0 graft command is therefore:

```
git replace --graft 364acd00d 4263936a0 03f614fb2
```

After this graft, `git merge-base upstream-v1.17.2-merge be4dc0c4b` must
resolve to `4263936a0` (v1.15.5), not `9038ba694` (v1.13.14).

### Tag-safety summary

- `v1.15.5` tag → `4263936a0` → **safe to use**. Resolves to the real
  upstream release commit.
- `v1.17.2` tag → `3ac2f7be8` → **UNSAFE**. Resolves to a Taiko commit
  (`remove golden touch from mempool (#484)`), not the upstream release.
  Always pin `be4dc0c4b` explicitly when referring to upstream v1.17.2.

## Approach: big-bang merge

One `git merge be4dc0c4b` into an integration branch, with conflicts resolved
in a subsystem-ordered sequence that produces one stacked commit per tier.
Stacked commits during development make the branch checkpointable and allow
partial review. On land, GitHub squash-merges the branch into `taiko`,
producing a single commit matching the `364acd00d` style.

Alternative approaches considered and rejected:

- **Incremental minor-by-minor merges** (v1.16.0, v1.16.1, …, v1.17.2): 5–10×
  the wall-clock cost, re-resolves the same conflicts across intermediate
  steps, and the bisectability benefit is theoretical. The prior big-bang
  merge (`364acd00d`) landed successfully and this one has the same shape.
- **Rebase-based replay** of Taiko commits on top of upstream v1.17.2:
  requires force-push, rewrites every Taiko commit SHA, breaks external
  references (PR links, tags), and contradicts the `364acd00d` history
  pattern.

## Conflict domains

Two file populations matter:

- **28 Taiko-exclusive files** (`taiko_*.go`, `core/taiko_genesis/*.json`,
  `consensus/taiko/*`, `scripts/taiko_generate_jsonrpc.go`). These do not
  textually conflict, but they break the build when upstream renames a type,
  moves a function, or changes a signature they depend on. Low conflict risk,
  medium adaptation risk.
- **30 shared files with `CHANGE(taiko)` markers**. These will textually
  conflict. This is the real work.

Organized by resolution tier (the order drives the execution plan).

### Tier 1 — Shared infrastructure and CLI

Files: `build/ci.go`, `cmd/geth/main.go`, `cmd/utils/flags.go`,
`cmd/utils/taiko_flags.go`, `node/defaults.go`, `node/endpoints.go`,
`eth/ethconfig/config.go`, `README.md`, `AGENTS.md`, `CLAUDE.md`.

Resolve first. Upstream wins on structure; Taiko flags and defaults
preserved. Resolving these first gets the compiler back on its feet so
subsequent tiers get type-checker feedback.

### Tier 2 — Params and chain config

Files: `params/config.go`, `params/protocol_params.go`,
`params/taiko_config.go`, `core/taiko_genesis.go`, `core/taiko_genesis/*.json`.

Foundation for everything else. Invariants:

- `TaikoChainConfig` stays Shanghai-capped. All new post-Shanghai fork fields
  introduced by upstream remain `nil` on `TaikoChainConfig`.
- `TestChainConfig` and `MergedTestChainConfig` pick up whatever upstream
  sets. No `Taiko: true` flag.
- Ontake, Pacaya, and Shasta fork fields preserved. `MainnetShastaTime =
  1_775_135_700`, `HoodiShastaTime = 1_770_296_400`, `MainnetOntakeBlock =
  538_304`, `MainnetPacayaBlock = 1_166_000`.
- New upstream fork config fields (if any) added to the struct and set per
  upstream on `MainnetChainConfig` and `MergedTestChainConfig`. Never activated
  on Taiko networks.

Correctness constraint: after this tier, `TaikoChainConfig.IsCancun(t)`,
`IsPrague(t)`, `IsOsaka(t)`, and any new post-Shanghai predicate must return
`false` at every timestamp.

### Tier 3 — Core execution and transaction types

Files: `core/blockchain.go`, `core/state_processor.go`,
`core/state_transition.go`, `core/types/transaction.go`,
`core/types/tx_dynamic_fee.go`, `core/types/taiko_transaction.go`,
`core/txpool/validation.go`, `core/tracing/hooks.go`.

Upstream reworked state transition, added EIP-7702 authorization lists,
EIP-7623 calldata floor, EIP-7708 transfer logs, and changed tracing hook
signatures. Taiko modifications in these files are small branches gated on
`config.Taiko`. Preserve those branches verbatim and let upstream win
everywhere else. `core/types/taiko_transaction.go` adapts to whatever upstream
did to the `Transaction` type's internal interface.

### Tier 4 — Consensus engine

Files: `consensus/taiko/consensus.go`, `consensus/taiko/consensus_test.go`,
`consensus/misc/taiko_eip4396.go`, `consensus/misc/taiko_eip4396_test.go`.

If upstream changed the `consensus.Engine` interface, adapt
`consensus/taiko/consensus.go`. The EIP-4396 min basefee clamp (0.01 Gwei on
mainnet, 0.005 Gwei elsewhere), Shasta fork behavior, and
anchor-v1/v2/v3/v4 selectors stay unchanged.

### Tier 5 — Engine API, miner, payload building (hardest tier)

Files: `eth/catalyst/api.go`, `eth/catalyst/queue.go`, `miner/worker.go`,
`miner/payload_building.go`, `miner/taiko_miner.go`, `miner/taiko_worker.go`,
`miner/taiko_payload_building.go`, `beacon/engine/types.go`.

Strategy: adopt upstream's new structure first, then layer Taiko semantics
back in against the new interfaces. Do not force the old structure back into
the file.

Upstream changed: engine API v4, payload attributes, blob/sidecar handling,
worker scheduling, witness stats relocation, slot number tracking, sender
handling refactor.

Taiko must preserve:

- L2 block production from ordered transaction lists.
- Taiko payload attributes including the `TaikoBlock` flag.
- Anchor transaction placement at index 0.
- Base-fee-per-gas override for legacy Taiko blocks.
- Reorg-allowed semantics in `eth/catalyst/api.go`.
- Taiko withdrawals-hash handling.
- Golden-touch exclusion from the mempool.
- `HeadL1Origin` write path even when the payload is cached.
- Preconfirmation simulator state (Gattaca preconf simulator API was reverted
  in commit `2c45f2787` and must stay reverted).

Expect this tier to take the most time. Allow breaking into multiple commits
by file group.

### Tier 6 — RPC surface

Files: `eth/api_backend.go`, `eth/api_debug.go`, `eth/state_accessor.go`,
`eth/tracers/api.go`, `eth/taiko_api_backend.go`,
`eth/taiko_api_backend_test.go`, `ethclient/taiko_api.go`,
`ethclient/taiko_api_test.go`, `rpc/server.go`, `rpc/json.go`.

All 13 Taiko RPC methods (see Compatibility surface below) preserved with
identical method names, parameter types, and return shapes. `L1Origin` JSON
marshaling including the `Signature hexutil.Bytes` override must survive.
Upstream RPC fixes adopted unless they alter the Taiko-only surfaces.

### Tier 7 — Persistence and genesis

Files: `core/rawdb/taiko_l1_origin.go`, `core/rawdb/gen_taiko_l1_origin.go`,
`core/rawdb/taiko_l1_origin_test.go`, `core/taiko_genesis/*.json`.

L1Origin persistence format must remain byte-compatible with existing Taiko
databases. Genesis JSON files untouched unless upstream forces a schema
migration. Regenerate `gen_taiko_l1_origin.go` only if the `L1Origin` struct
required a field change.

### Tier 8 — Tests

All `*_test.go` files in the touched packages, plus any new tests brought in
by upstream. Upstream tests use `TestChainConfig` or `MergedTestChainConfig`
(no `Taiko: true`) and should exercise the non-Taiko code path exactly as in
vanilla go-ethereum v1.17.2. Taiko tests exercise `TaikoChainConfig` and
`config.Taiko`-gated branches.

## Compatibility surface (must not regress)

### `taiko_` public RPC namespace

- `taiko_headL1Origin()` → `*L1Origin`
- `taiko_l1OriginByID(blockID)` → `*L1Origin`
- `taiko_getSyncMode()` → `string`

### `taikoAuth_` auth RPC namespace

- `taikoAuth_lastL1OriginByBatchID(batchID)` → `*L1Origin`
- `taikoAuth_lastBlockIDByBatchID(batchID)` → `*hexutil.Big`
- `taikoAuth_lastCertainBlockIDByBatchID(batchID)` → `*hexutil.Big`
- `taikoAuth_lastCertainL1OriginByBatchID(batchID)` → `*L1Origin`
- `taikoAuth_setHeadL1Origin(blockID)` → `*hexutil.Big`
- `taikoAuth_setBatchToLastBlock(batchID, blockID)` → `*hexutil.Big`
- `taikoAuth_updateL1Origin(l1Origin)` → `*L1Origin`
- `taikoAuth_setL1OriginSignature(blockID, signature hexutil.Bytes)` →
  `*L1Origin`
- `taikoAuth_txPoolContent(baseFee, beneficiary, blockMaxGasLimit, locals,`
  `maxBytesPerTxList, maxTransactionsLists)` → `[]preBuiltTxList`
- `taikoAuth_txPoolContentWithMinTip(...same + minTip)` → `[]preBuiltTxList`

### `L1Origin` JSON shape

```
{
  blockID,            // *big.Int → *math.HexOrDecimal256 in JSON
  l2BlockHash,        // common.Hash
  l1BlockHeight,      // *big.Int → *math.HexOrDecimal256 in JSON
  l1BlockHash,        // common.Hash
  buildPayloadArgsID, // [8]byte
  isForcedInclusion,  // bool
  signature,          // [65]byte on the struct; marshaled as hexutil.Bytes
                      // via the gencodec override (commit 01dfc264a)
}
```

Struct definition lives in `core/rawdb/taiko_l1_origin.go` as
`type L1Origin struct`. The JSON marshaling override in
`core/rawdb/gen_taiko_l1_origin.go` transforms the fixed-size `[65]byte`
signature into a `hexutil.Bytes` string on the wire.

### Taiko protocol concepts preserved

- Anchor transaction function selectors: `AnchorSelector`, `AnchorV2Selector`,
  `AnchorV3Selector`, `AnchorV4Selector`.
- Golden Touch account: `0x0000777735367b36bC9B61C50022d9D0700dB4Ec`.
- Base fee sharing via `BasefeeSharingPctg`.
- Preconfirmation blocks distinguished via `L1BlockHeight == nil` or `0`.
- Taiko payload attributes plumbed through `eth/catalyst/api.go`.

## Execution plan

Each phase produces one commit on `upstream-v1.17.2-merge`. Every phase has
an explicit exit gate. If a gate fails, stop and debug — do not advance.

### Phase 0 — Setup

1. `git fetch upstream` and verify `upstream/master` contains `be4dc0c4b`.
2. Record the existing local `v1.17.2` tag SHA for reference, but never use
   the bare name in merge commands.
3. Create a worktree:
   `git worktree add .worktrees/upstream-v1.17.2-merge -b upstream-v1.17.2-merge taiko`.
4. `git replace --graft 364acd00d 4263936a0 03f614fb2` to restore ancestry
   (squash commit, upstream v1.15.5, original parent).
5. Verify: `git merge-base upstream-v1.17.2-merge be4dc0c4b` resolves to
   `4263936a0` (v1.15.5).
6. `git status` clean in the worktree.

Exit gate: merge base resolves to `4263936a0` (v1.15.5), not `9038ba694`
(v1.13.14).

### Phase 1 — Raw merge and conflict triage

1. `git merge --no-commit --no-ff be4dc0c4b` in the worktree.
2. Do not fix anything yet.
3. `git diff --name-only --diff-filter=U` to get the conflict list.
4. Classify every conflicted file into the tiers above and write the
   classification to
   `docs/superpowers/specs/2026-04-11-upstream-v1-17-2-conflict-triage.md`.
5. Capture auto-merged files and new upstream additions for reference.

Exit gate: triage document exists and every conflicted file is classified.

### Phase 2 — Tier 1 reconcile: shared infrastructure

Resolve `build/ci.go`, `cmd/geth/main.go`, `cmd/utils/flags.go`,
`cmd/utils/taiko_flags.go`, `node/defaults.go`, `node/endpoints.go`,
`eth/ethconfig/config.go`, docs.

Upstream wins on structure. Taiko flags preserved.

Exit gate: `go build ./cmd/... ./node/... ./eth/ethconfig/... ./build/...`
compiles. Commit:
`chore(upstream-merge): reconcile shared infrastructure`.

### Phase 3 — Tier 2 reconcile: params and chain config

Resolve `params/config.go`, `params/protocol_params.go`,
`params/taiko_config.go`, `core/taiko_genesis.go`, `core/taiko_genesis/*.json`.

`TaikoChainConfig` stays Shanghai-capped. New upstream fork fields stay nil
on Taiko. Ontake/Pacaya/Shasta preserved. Helper predicates return `false`
on Taiko for every post-Shanghai fork.

Exit gate: `go build ./params/... ./core/...` succeeds, `go test ./params/...`
passes. Commit:
`chore(upstream-merge): reconcile chain config preserving Shanghai-capped Taiko`.

### Phase 4 — Tier 3 reconcile: core execution

Resolve `core/blockchain.go`, `core/state_processor.go`,
`core/state_transition.go`, `core/types/transaction.go`,
`core/types/tx_dynamic_fee.go`, `core/types/taiko_transaction.go`,
`core/txpool/validation.go`, `core/tracing/hooks.go`.

Upstream wins on EIP-7702/7623/7708 plumbing. Every `if config.Taiko { ... }`
branch preserved.

Exit gate: `go build ./core/...` succeeds. Commit:
`chore(upstream-merge): reconcile core execution preserving Taiko branches`.

### Phase 5 — Tier 4 reconcile: consensus engine

Resolve `consensus/taiko/consensus.go`, `consensus/misc/taiko_eip4396.go`
and their tests. Adapt to any upstream `consensus.Engine` interface changes.
EIP-4396 and Shasta behavior unchanged.

Exit gate: `go build ./consensus/...` and
`go test ./consensus/taiko/... ./consensus/misc/...` pass. Commit:
`chore(upstream-merge): reconcile Taiko consensus engine against upstream interface changes`.

### Phase 6 — Tier 5 reconcile: engine API, miner, payload building

Resolve `eth/catalyst/api.go`, `eth/catalyst/queue.go`, `miner/worker.go`,
`miner/payload_building.go`, `miner/taiko_miner.go`, `miner/taiko_worker.go`,
`miner/taiko_payload_building.go`, `beacon/engine/types.go`.

Adopt upstream structure first, then layer Taiko semantics back in. Preserve
every item in the Tier 5 "Taiko must preserve" list above. This is the
hardest phase — allow splitting into multiple commits by file group.

Exit gate: `go build ./miner/... ./eth/catalyst/... ./beacon/...` and
`go test ./miner/...` pass including Taiko tests. Commits:
`chore(upstream-merge): reconcile engine/miner/payload-building`
(split if useful).

### Phase 7 — Tier 6 reconcile: RPC surface

Resolve `eth/api_backend.go`, `eth/api_debug.go`, `eth/state_accessor.go`,
`eth/tracers/api.go`, `eth/taiko_api_backend.go`, `ethclient/taiko_api.go`,
`rpc/server.go`, `rpc/json.go`.

All 13 Taiko RPC methods preserved. `L1Origin.Signature` marshals as
`hexutil.Bytes`.

Exit gate: `go test ./eth/... ./ethclient/... ./rpc/...` passes. Commit:
`chore(upstream-merge): reconcile RPC surface preserving taikoAuth_ namespace`.

### Phase 8 — Tier 7 reconcile: persistence and genesis

Resolve `core/rawdb/taiko_l1_origin.go`, `core/rawdb/gen_taiko_l1_origin.go`,
`core/taiko_genesis/*`. L1Origin format byte-compatible. Regenerate
`gen_taiko_l1_origin.go` only if the struct changed.

Exit gate: `go test ./core/rawdb/...` passes. Commit:
`chore(upstream-merge): reconcile persistence preserving L1Origin format`.

### Phase 9 — Test restoration and full-repo build

1. Resolve any remaining conflicted tests.
2. `go build ./...` across the whole tree.
3. `make test` across the whole tree.
4. Any failing test gets a root-cause fix in source, not a skip.
5. Run `go generate ./...` and commit any legitimate regenerated files.

Exit gate: `make test` exits 0 across the whole repository. Commit:
`test(upstream-merge): restore Taiko-specific tests after refactor`.

### Phase 10 — Finalization and PR prep

1. Update upstream version references in `README.md`.
2. Update any Taiko-specific version metadata if applicable.
3. Regenerate committed `go generate` artifacts (JSON-RPC docs, bindings).
4. Review the branch's commit log for narrative clarity across phases.
5. `make lint` clean.
6. Final `make test` and `make geth`.
7. Push the branch, open a GitHub PR targeting `taiko`. PR body lists the
   upstream SHA range, conflict tiers touched, and verification gates passed.

Exit gate: PR open with all CI green, ready for review.

## Verification gates (all must pass before land)

1. **Build gate**: `make geth` and `go build ./...` succeed.
2. **Full test gate**: `make test` exits 0. No new skips, no new quarantines
   versus the current `taiko` branch.
3. **Taiko regression gate**: focused packages all green:
   - `go test ./consensus/taiko/... ./consensus/misc/...`
   - `go test ./eth/... -run Taiko`
   - `go test ./ethclient/...`
   - `go test ./miner/... -run Taiko`
   - `go test ./core/rawdb/... -run L1Origin`
   - `go test ./params/...`
4. **Shanghai-cap invariant gate**: a test in `params/taiko_config_test.go`
   asserts `TaikoChainConfig.IsCancun(t) == false` and the same for
   `IsPrague`, `IsOsaka`, and every post-Shanghai predicate upstream
   introduced. Must hold at timestamps `0` and `math.MaxUint64`.
5. **RPC compatibility gate**: a test in `eth/taiko_api_backend_test.go`
   asserts all 13 `taiko_*` and `taikoAuth_*` methods are registered,
   callable, and return the expected JSON field names. `L1Origin.Signature`
   marshals as `hexutil.Bytes`.
6. **Fork activation audit**: manual inspection of `core/taiko_genesis.go`
   confirms no upstream fork field leaked into Taiko genesis unintentionally.
7. **Lint gate**: `make lint` and `golangci-lint run` clean.
8. **Merge base gate**: `git merge-base upstream-v1.17.2-merge be4dc0c4b`
   returns `4263936a0` (v1.15.5). Proves the graft worked.

## Key risks and mitigations

- **Ancestry graft fails silently**: if `git replace --graft` is skipped or
  wrong, conflict volume explodes. Mitigation: Phase 0 exit gate verifies
  the merge base explicitly.
- **Local `v1.17.2` tag used by accident**: merge resolves to the wrong ref.
  Mitigation: plan pins `be4dc0c4b` everywhere; never use `v1.17.2` bare.
- **Silent fork activation on Taiko**: a new upstream fork field inherits a
  non-nil default on `TaikoChainConfig`. Mitigation: verification gate 4
  (Shanghai-cap invariant test) catches this.
- **RPC field rename via struct embedding**: upstream refactors `L1Origin` or
  `preBuiltTxList` JSON tags. Mitigation: verification gate 5 (RPC
  compatibility gate).
- **L1Origin database schema drift**: upstream rawdb changes break existing
  Taiko databases. Mitigation: Phase 8 exit gate; treat any schema change as
  a blocker.
- **Payload building semantic drift**: upstream changes to shared miner code
  subtly change block construction while tests still pass because Taiko uses
  its own worker. Mitigation: Phase 6 reviewer explicitly traces each Taiko
  payload code path. Hardest risk to catch automatically.
- **Generated-file drift**: `gen_taiko_l1_origin.go`, ABI bindings, RPC docs
  go stale. Mitigation: Phase 10 runs `go generate ./...` and commits any
  diff.
- **Runtime-only regressions**: subtle gas accounting, fee distribution, or
  reorg-handling changes that only surface on a live node. Explicitly
  out of scope for this PR; flagged in the PR body as "post-merge runtime
  validation required" for follow-up devnet validation.

## Rollback plan

- **In-phase**: `git reset --hard <prior-phase-sha>` undoes a single phase's
  work without touching other phases.
- **Whole merge**: delete the integration branch and start over from `taiko`.
  No history damage — the PR has not landed.
- **Post-land**: if a regression is found after squash-merge lands on
  `taiko`, revert the squash commit with `git revert <squash-sha>`. Because
  it is one commit, revert is one command. All Taiko work landed after the
  merge is preserved.
- **Database rollback**: no migration involved. Chain data files are
  unchanged. A node running the merged build can downgrade to the prior
  `taiko` build without data loss, provided the Shanghai-cap invariant held
  (verification gate 4).

## Explicit decisions

- Big-bang merge, not incremental minor-by-minor. Not rebase-replay.
- One integration branch (`upstream-v1.17.2-merge`), stacked subsystem
  commits during work, squash-merge via GitHub PR at land.
- Target is `v1.17.2` exactly; not v1.17.3 or v1.17.4.
- Prior aborted merge attempts ignored. Fresh start from current `taiko`.
- `TaikoChainConfig` stays Shanghai-capped. No Cancun, Prague, or Osaka
  activation on Taiko networks as part of this merge.
- Upstream tests run with `TestChainConfig` or `MergedTestChainConfig`
  (no `Taiko: true`) and exercise full upstream behavior including new
  forks. All tests must pass; no skips, no quarantines.
- Taiko-specific tests run with `TaikoChainConfig` and exercise
  `config.Taiko`-gated branches.
- Live-network runtime validation is out of scope for this merge. Follow-up
  devnet validation happens separately.
- `git replace --graft` restores v1.15.5 as the effective merge base.

## Exit criteria

The merge is complete when:

- The integration branch builds (`make geth`, `go build ./...`).
- `make test` exits 0 across the whole repository.
- All verification gates pass.
- A GitHub PR targeting `taiko` is open with green CI.
- The PR body lists the upstream SHA range, conflict tiers touched, and
  verification gate results.

After land, the `taiko` branch head is a single squash-merge commit
containing the entire v1.15.5 → v1.17.2 upstream integration plus the
preserved Taiko feature set, matching the `364acd00d` pattern.
