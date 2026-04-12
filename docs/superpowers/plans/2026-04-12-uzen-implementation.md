# Uzen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `Uzen` Taiko timestamp fork to taiko-geth, matching Alethia runtime behavior for fork activation, payload normalization, blob-tx rejection, and zk-gas enforcement.

**Architecture:** Treat `Uzen` as a first-class Taiko time fork alongside `Shasta`, then layer Uzen-specific runtime behavior at the existing seams: chain config/genesis, payload conversion/validation, tx ingress, and EVM/state processing. Keep shared Uzen helpers small and explicit, and implement zk-gas as a dedicated meter wired into `core/vm` and consumed by `core/state_processor`.

**Tech Stack:** Go, taiko-geth core packages (`params`, `core`, `beacon/engine`, `eth/catalyst`, `core/vm`, `core/txpool`), Go test

---

## File Map

### Existing files to modify

- `params/config.go`
  Adds `UzenTime`, `IsUzen`, config banner/compatibility wiring, and `Rules.IsUzen`.
- `core/taiko_genesis.go`
  Wires Alethia/other Taiko network defaults for `UzenTime`.
- `params/taiko_config_test.go`
  Covers `IsUzen` activation and `Rules.IsUzen`.
- `eth/taiko_fork_upgrade_test.go`
  Verifies nodes upgrading from a config without `UzenTime` receive the new fork config.
- `beacon/engine/types.go`
  Applies Uzen-aware `parentBeaconBlockRoot` and `requestsHash` normalization during payload-to-block conversion.
- `beacon/engine/gen_ed.go`
  Regenerate after adding a Uzen-specific payload flag to `ExecutableData`.
- `core/block_validator.go`
  Uses Uzen-aware requests-hash validation instead of always re-deriving the Prague-style hash.
- `eth/catalyst/api.go`
  Normalizes beacon root/requests on Uzen payload ingress and rejects blob tx payloads after `Uzen`.
- `miner/worker.go`
  Forces empty requests hash for locally built Uzen payloads.
- `core/error.go`
  Adds a dedicated blob-rejection error for Uzen.
- `core/txpool/validation.go`
  Rejects blob txs when `config.IsUzen(head.Time)` is true.
- `core/txpool/validation_test.go`
  Covers post-Uzen txpool rejection.
- `eth/handler_eth.go`
  Rejects inbound blob txs from peers once Uzen is active.
- `eth/handler_eth_test.go`
  Covers peer-path blob rejection.
- `miner/taiko_worker.go`
  Keeps local Taiko block building blob-free under Uzen.
- `miner/taiko_worker_test.go`
  Covers Uzen-era local builder behavior.
- `core/vm/errors.go`
  Adds `ErrZKGasLimitReached`.
- `core/vm/interpreter.go`
  Feeds per-opcode gas into the zk-gas meter.
- `core/vm/evm.go`
  Marks spawn/precompile paths and feeds precompile gas into the meter.
- `core/state_processor.go`
  Creates the Uzen meter, snapshots state/gaspool per tx, discards the offending tx, and truncates the block.
- `core/state_processor_test.go`
  Covers block-truncation semantics under zk-gas overflow.

### New files to create

- `core/taiko_uzen.go`
  Shared helper functions:
  - `NormalizeUzenParentBeaconRoot`
  - `UzenRequestsHash`
  - `RejectUzenBlobTransactions`
- `core/taiko_uzen_test.go`
  Direct tests for helper behavior without engine setup noise.
- `core/vm/taiko_zk_gas.go`
  `ZKGasMeter` implementation, per-tx/per-block accounting, overflow handling.
- `core/vm/taiko_zk_gas_tables.go`
  Opcode multipliers, precompile multipliers, and spawn estimates.
- `core/vm/taiko_zk_gas_test.go`
  Unit tests for opcode charging, spawn charging, precompile charging, and overflow.

### No new package boundaries

- Keep helpers inside existing packages instead of introducing a new top-level Taiko/Uzen package tree.
- Keep zk-gas inside `core/vm` so it can observe interpreter/EVM internals without layering hacks.

## Task 1: Add Uzen Fork Activation To Chain Config And Genesis

**Files:**
- Modify: `params/config.go`
- Modify: `core/taiko_genesis.go`
- Modify: `params/taiko_config_test.go`
- Modify: `eth/taiko_fork_upgrade_test.go`

- [ ] **Step 1: Write the failing fork-activation tests**

Add these tests first.

```go
func TestIsUzenTimestampFork(t *testing.T) {
	cfg := *params.TaikoChainConfig
	uzenTime := uint64(1_780_000_000)
	cfg.UzenTime = &uzenTime

	if cfg.IsUzen(uzenTime - 1) {
		t.Fatal("expected Uzen to be inactive before UzenTime")
	}
	if !cfg.IsUzen(uzenTime) {
		t.Fatal("expected Uzen to activate exactly at UzenTime")
	}
	if !cfg.Rules(common.Big1, true, uzenTime).IsUzen {
		t.Fatal("expected Rules.IsUzen to mirror ChainConfig.IsUzen")
	}
}
```

```go
func TestUzenUpgradeUsesUpdatedChainConfigInConsensusEngine(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	oldGenesis := core.TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())
	oldCfg := *oldGenesis.Config
	oldCfg.UzenTime = nil
	oldGenesis.Config = &oldCfg
	oldGenesis.MustCommit(db, triedb.NewDatabase(db, triedb.HashDefaults))

	newGenesis := core.TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())
	loadedCfg, _, err := core.LoadChainConfig(db, newGenesis)
	if err != nil {
		t.Fatalf("failed to load chain config: %v", err)
	}
	if loadedCfg.UzenTime != nil {
		t.Fatalf("expected stored config to be missing UzenTime, got %d", *loadedCfg.UzenTime)
	}

	engine, err := ethconfig.CreateConsensusEngine(loadedCfg, db)
	if err != nil {
		t.Fatalf("failed to create consensus engine: %v", err)
	}
	chain, err := core.NewBlockChain(db, newGenesis, engine, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	if chain.Config().UzenTime == nil {
		t.Fatal("expected upgraded blockchain config to contain UzenTime")
	}
}
```

- [ ] **Step 2: Run the targeted tests and confirm they fail**

Run:

```bash
go test ./params ./eth -run 'Test(IsUzenTimestampFork|UzenUpgradeUsesUpdatedChainConfigInConsensusEngine)' -count=1
```

Expected:

- `params/config.go` compile failure because `UzenTime`, `IsUzen`, or `Rules.IsUzen` do not exist yet
- `eth/taiko_fork_upgrade_test.go` compile failure because `UzenTime` is missing

- [ ] **Step 3: Implement Uzen config/genesis plumbing**

Add the new chain-config field and helper methods in `params/config.go`.

```go
type ChainConfig struct {
	// CHANGE(taiko): Taiko network flag.
	Taiko       bool     `json:"taiko"`
	OntakeBlock *big.Int `json:"ontakeBlock,omitempty"`
	PacayaBlock *big.Int `json:"pacayaBlock,omitempty"`
	ShastaTime  *uint64  `json:"shastaTime,omitempty"`
	UzenTime    *uint64  `json:"uzenTime,omitempty"`
}
```

```go
func (c *ChainConfig) IsUzen(time uint64) bool {
	return isTimestampForked(c.UzenTime, time)
}
```

Update `Rules` and `Rules(...)`:

```go
type Rules struct {
	IsMerge, IsShanghai, IsCancun, IsPrague, IsOsaka bool
	IsAmsterdam, IsVerkle, IsUzen                    bool
}
```

```go
return Rules{
	IsMerge:     isMerge,
	IsShanghai:  isMerge && c.IsShanghai(num, timestamp),
	IsCancun:    isMerge && c.IsCancun(num, timestamp),
	IsPrague:    isMerge && c.IsPrague(num, timestamp),
	IsOsaka:     isMerge && c.IsOsaka(num, timestamp),
	IsAmsterdam: isMerge && c.IsAmsterdam(num, timestamp),
	IsVerkle:    isVerkle,
	IsEIP4762:   isVerkle,
	IsUzen:      c.Taiko && c.IsUzen(timestamp),
}
```

Update banner/compat handling in `params/config.go`:

```go
if c.UzenTime != nil {
	banner += fmt.Sprintf(" - Uzen:                       @%-10v\n", *c.UzenTime)
}
```

```go
if isTimestampForkIncompatible(c.UzenTime, newcfg.UzenTime, btime) {
	return newTimestampCompatError("Uzen fork timestamp", c.UzenTime, newcfg.UzenTime)
}
```

Wire network defaults in `core/taiko_genesis.go`:

```go
var (
	InternalUzenTime uint64 = 0
	MasayaUzenTime   uint64 = 0
	MainnetUzenTime  uint64 = 1_780_000_000
	HoodiUzenTime    uint64 = 0
)
```

```go
case params.TaikoMainnetNetworkID.Uint64():
	chainConfig.ChainID = params.TaikoMainnetNetworkID
	chainConfig.OntakeBlock = MainnetOntakeBlock
	chainConfig.PacayaBlock = MainnetPacayaBlock
	chainConfig.ShastaTime = &MainnetShastaTime
	chainConfig.UzenTime = &MainnetUzenTime
```

- [ ] **Step 4: Run the targeted tests and confirm they pass**

Run:

```bash
go test ./params ./eth -run 'Test(IsUzenTimestampFork|UzenUpgradeUsesUpdatedChainConfigInConsensusEngine|TestNetworkIDToChainConfigOrDefault)' -count=1
```

Expected:

- PASS for the new Uzen activation tests
- existing `TestNetworkIDToChainConfigOrDefault` remains green

- [ ] **Step 5: Commit**

```bash
git add params/config.go core/taiko_genesis.go params/taiko_config_test.go eth/taiko_fork_upgrade_test.go
git commit -m "feat(params): add Uzen time fork activation"
```

## Task 2: Normalize Uzen Payload Fields In Shared Helpers And Engine Paths

**Files:**
- Create: `core/taiko_uzen.go`
- Create: `core/taiko_uzen_test.go`
- Modify: `beacon/engine/types.go`
- Modify: `core/block_validator.go`
- Modify: `eth/catalyst/api.go`
- Modify: `miner/worker.go`
- Modify: `beacon/engine/types_test.go`
- Modify: `eth/catalyst/api_test.go`

- [ ] **Step 1: Write the failing helper and engine tests**

Add a direct helper test in `core/taiko_uzen_test.go`.

```go
func TestUzenRequestsHashUsesEmptyHash(t *testing.T) {
	got := UzenRequestsHash(true, [][]byte{[]byte("not-empty")})
	if got == nil {
		t.Fatal("expected non-nil requests hash for Uzen")
	}
	if *got != types.EmptyRequestsHash {
		t.Fatalf("unexpected requests hash: got %x want %x", *got, types.EmptyRequestsHash)
	}
}
```

Add an engine-level test in `beacon/engine/types_test.go`.

```go
func TestExecutableDataToBlockNormalizesUzenBeaconRootAndRequestsHash(t *testing.T) {
	timestamp := uint64(1_780_000_001)
	payload := ExecutableData{
		Number:        1,
		Timestamp:     timestamp,
		GasLimit:      30_000_000,
		GasUsed:       0,
		BaseFeePerGas: big.NewInt(params.InitialBaseFee),
		Transactions:  nil,
		ExtraData:     nil,
		LogsBloom:     make([]byte, 256),
		TaikoBlock:    true,
		UzenBlock:     true,
	}

	block, err := ExecutableDataToBlockNoHash(payload, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if block.BeaconRoot() == nil || *block.BeaconRoot() != (common.Hash{}) {
		t.Fatalf("expected zero beacon root under Uzen, got %v", block.BeaconRoot())
	}
	if block.RequestsHash() == nil || *block.RequestsHash() != types.EmptyRequestsHash {
		t.Fatalf("expected empty requests hash under Uzen, got %v", block.RequestsHash())
	}
}
```

- [ ] **Step 2: Run the targeted tests and confirm they fail**

Run:

```bash
go test ./core ./beacon/engine -run 'Test(UzenRequestsHashUsesEmptyHash|ExecutableDataToBlockNormalizesUzenBeaconRootAndRequestsHash)' -count=1
```

Expected:

- compile failure because `core/taiko_uzen.go` and helper functions do not exist yet
- engine test failure because `ExecutableDataToBlockNoHash` still uses raw `beaconRoot`/`CalcRequestsHash`

- [ ] **Step 3: Implement shared Uzen normalization helpers and wire them in**

Create `core/taiko_uzen.go`:

```go
package core

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func NormalizeUzenParentBeaconRoot(uzenActive bool, beaconRoot *common.Hash) *common.Hash {
	if uzenActive && beaconRoot == nil {
		zero := common.Hash{}
		return &zero
	}
	return beaconRoot
}

func UzenRequestsHash(uzenActive bool, requests [][]byte) *common.Hash {
	if uzenActive {
		hash := types.EmptyRequestsHash
		return &hash
	}
	if requests == nil {
		return nil
	}
	hash := types.CalcRequestsHash(requests)
	return &hash
}

func RejectUzenBlobTransactions(uzenActive bool, txs []*types.Transaction) error {
	if !uzenActive {
		return nil
	}
	for _, tx := range txs {
		if tx.Type() == types.BlobTxType {
			return ErrBlobTransactionsUnsupported
		}
	}
	return nil
}
```

Add a Uzen-specific payload flag to `ExecutableData` and regenerate `gen_ed.go`:

```go
type ExecutableData struct {
	// ...
	TaikoBlock bool // CHANGE(taiko)
	UzenBlock  bool // CHANGE(taiko): whether Uzen payload normalization applies
}
```

```bash
go generate ./beacon/engine
```

Use the helper in `beacon/engine/types.go`:

```go
if data.UzenBlock {
	beaconRoot = core.NormalizeUzenParentBeaconRoot(true, beaconRoot)
	requestsHash = core.UzenRequestsHash(true, requests)
} else {
	if requests != nil {
		h := types.CalcRequestsHash(requests)
		requestsHash = &h
	}
}
```

Use the helper in `eth/catalyst/api.go` before converting payloads and set the new flag:

```go
if api.eth.BlockChain().Config().Taiko {
	params.UzenBlock = api.eth.BlockChain().Config().IsUzen(params.Timestamp)
	beaconRoot = core.NormalizeUzenParentBeaconRoot(params.UzenBlock, beaconRoot)
}
block, err := engine.ExecutableDataToBlock(params, versionedHashes, beaconRoot, requests)
if err != nil {
	return api.invalid(err, nil), nil
}
if err := core.RejectUzenBlobTransactions(params.UzenBlock, block.Transactions()); err != nil {
	return api.invalid(err, parent.Header()), nil
}
```

Use the helper in `miner/worker.go`:

```go
if miner.chainConfig.IsUzen(work.header.Time) {
	work.header.RequestsHash = &types.EmptyRequestsHash
} else {
	work.header.RequestsHash = core.UzenRequestsHash(false, requests)
}
```

Use the helper in `core/block_validator.go`:

```go
if header.RequestsHash != nil {
	want := core.UzenRequestsHash(v.config.Taiko && v.config.IsUzen(header.Time), res.Requests)
	if want == nil || *want != *header.RequestsHash {
		return fmt.Errorf("invalid requests hash (remote: %x local: %x)", *header.RequestsHash, *want)
	}
}
```

- [ ] **Step 4: Run the targeted tests and confirm they pass**

Run:

```bash
go test ./core ./beacon/engine ./eth/catalyst -run 'Test(UzenRequestsHashUsesEmptyHash|ExecutableDataToBlockNormalizesUzenBeaconRootAndRequestsHash|ParentBeaconBlockRoot)' -count=1
```

Expected:

- Uzen helper and engine tests PASS
- existing `TestParentBeaconBlockRoot` stays green

- [ ] **Step 5: Commit**

```bash
git add core/taiko_uzen.go core/taiko_uzen_test.go beacon/engine/types.go beacon/engine/gen_ed.go core/block_validator.go eth/catalyst/api.go miner/worker.go beacon/engine/types_test.go eth/catalyst/api_test.go
git commit -m "feat(engine): normalize Uzen payload fields"
```

## Task 3: Reject Blob Transactions At Ingress, Validation, And Local Building

**Files:**
- Modify: `core/error.go`
- Modify: `core/txpool/validation.go`
- Modify: `core/txpool/validation_test.go`
- Modify: `eth/handler_eth.go`
- Modify: `eth/handler_eth_test.go`
- Modify: `miner/taiko_worker.go`
- Modify: `miner/taiko_worker_test.go`

- [ ] **Step 1: Write the failing blob-rejection tests**

Add a txpool validation test:

```go
func TestValidateTransactionRejectsBlobTxAtUzen(t *testing.T) {
	cfg := *params.TaikoChainConfig
	uzenTime := uint64(100)
	cfg.UzenTime = &uzenTime

	head := &types.Header{
		Number:     common.Big1,
		Time:       uzenTime,
		GasLimit:   30_000_000,
		Difficulty: common.Big0,
	}
	opts := &ValidationOptions{
		Config:       &cfg,
		Accept:       0xFF,
		MaxSize:      128 * 1024,
		MaxBlobCount: params.BlobTxMaxBlobs,
		MinTip:       common.Big0,
	}

	blob := new(kzg4844.Blob)
	commitment, _ := kzg4844.BlobToCommitment(blob)
	proof, _ := kzg4844.ComputeBlobProof(blob, commitment)
	tx := types.NewTx(&types.BlobTx{
		Nonce:      0,
		To:         common.Address{1},
		Gas:        params.TxGas,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.NewInt(params.InitialBaseFee),
		BlobFeeCap: uint256.NewInt(params.BlobTxMinBlobGasprice),
		BlobHashes: []common.Hash{kzg4844.CalcBlobHashV1(sha256.New(), &commitment)},
		Sidecar:    types.NewBlobTxSidecar(types.BlobSidecarVersion0, []kzg4844.Blob{*blob}, []kzg4844.Commitment{commitment}, []kzg4844.Proof{proof}),
	})
	err := ValidateTransaction(tx, head, types.LatestSigner(&cfg), opts)
	if !errors.Is(err, core.ErrBlobTransactionsUnsupported) {
		t.Fatalf("expected ErrBlobTransactionsUnsupported, got %v", err)
	}
}
```

Add a peer-ingress test in `eth/handler_eth_test.go`:

```go
func TestHandleTransactionsRejectsBlobTxAtUzen(t *testing.T) {
	peer := eth.NewPeer(eth.ETH69, p2p.NewPeer(enode.ID{1}, "", nil), nil, nil)
	tx := types.NewTx(&types.BlobTx{To: common.Address{1}})
	if err := handleTransactions(peer, []*types.Transaction{tx}, false); !errors.Is(err, core.ErrBlobTransactionsUnsupported) {
		t.Fatalf("expected ErrBlobTransactionsUnsupported, got %v", err)
	}
}
```

- [ ] **Step 2: Run the targeted tests and confirm they fail**

Run:

```bash
go test ./core/txpool ./eth ./miner -run 'Test(ValidateTransactionRejectsBlobTxAtUzen|HandleTransactionsRejectsBlobTxAtUzen|TestBuildTransactionsLists)' -count=1
```

Expected:

- compile failure because `ErrBlobTransactionsUnsupported` does not exist
- txpool/peer tests fail because Uzen-era blob txs are still accepted

- [ ] **Step 3: Implement explicit Uzen blob rejection**

Add the dedicated error in `core/error.go`:

```go
var (
	ErrBlobTransactionsUnsupported = errors.New("blob transactions unsupported after Uzen")
)
```

Reject in txpool validation:

```go
if opts.Config.Taiko && opts.Config.IsUzen(head.Time) && tx.Type() == types.BlobTxType {
	return ErrBlobTransactionsUnsupported
}
```

Reject in peer ingress:

```go
if tx.Type() == types.BlobTxType {
	return core.ErrBlobTransactionsUnsupported
}
```

Keep local Taiko builder blob-free in `miner/taiko_worker.go`:

```go
if miner.chainConfig.IsUzen(env.header.Time) && tx.Type() == types.BlobTxType {
	log.Debug("Skip a Uzen-disallowed blob transaction", "hash", tx.Hash())
	continue
}
```

- [ ] **Step 4: Run the targeted tests and confirm they pass**

Run:

```bash
go test ./core/txpool ./eth ./miner -run 'Test(ValidateTransactionRejectsBlobTxAtUzen|HandleTransactionsRejectsBlobTxAtUzen|BuildTransactionsLists|RemoveGoldenTouchPendingTxs)' -count=1
```

Expected:

- new blob-rejection tests PASS
- existing miner tests remain green

- [ ] **Step 5: Commit**

```bash
git add core/error.go core/txpool/validation.go core/txpool/validation_test.go eth/handler_eth.go eth/handler_eth_test.go miner/taiko_worker.go miner/taiko_worker_test.go
git commit -m "feat(txpool): reject blob transactions after Uzen"
```

## Task 4: Add Uzen zk-gas Metering To The EVM

**Files:**
- Create: `core/vm/taiko_zk_gas.go`
- Create: `core/vm/taiko_zk_gas_tables.go`
- Create: `core/vm/taiko_zk_gas_test.go`
- Modify: `core/vm/errors.go`
- Modify: `core/vm/interpreter.go`
- Modify: `core/vm/evm.go`

- [ ] **Step 1: Write the failing zk-gas unit tests**

Add these table-driven tests in `core/vm/taiko_zk_gas_test.go`.

```go
func TestZKGasMeterChargesOpcodeMultiplier(t *testing.T) {
	meter := NewZKGasMeter(100_000_000)
	meter.StartTx()

	if err := meter.OnOpcode(ADD, 3, false, false); err != nil {
		t.Fatal(err)
	}
	if got, want := meter.CurrentTxUsed(), uint64(36); got != want { // ADD multiplier 12 * gas 3
		t.Fatalf("unexpected tx zk gas: got %d want %d", got, want)
	}
}
```

```go
func TestZKGasMeterUsesSpawnEstimateForPrecompileCall(t *testing.T) {
	meter := NewZKGasMeter(100_000_000)
	meter.StartTx()

	if err := meter.OnOpcode(CALL, 10_700, false, true); err != nil {
		t.Fatal(err)
	}
	if got, want := meter.CurrentTxUsed(), uint64(12_500*25); got != want {
		t.Fatalf("unexpected CALL zk gas: got %d want %d", got, want)
	}
}
```

```go
func TestZKGasMeterTreatsOverflowAsLimitReached(t *testing.T) {
	meter := NewZKGasMeter(math.MaxUint64)
	meter.StartTx()

	err := meter.OnPrecompile(common.HexToAddress("0x05"), math.MaxUint64)
	if !errors.Is(err, ErrZKGasLimitReached) {
		t.Fatalf("expected ErrZKGasLimitReached, got %v", err)
	}
}
```

- [ ] **Step 2: Run the targeted tests and confirm they fail**

Run:

```bash
go test ./core/vm -run 'TestZKGasMeter' -count=1
```

Expected:

- compile failure because `NewZKGasMeter`, `OnOpcode`, `OnPrecompile`, and `ErrZKGasLimitReached` do not exist yet

- [ ] **Step 3: Implement the meter and wire it into interpreter/EVM**

Add the new VM error in `core/vm/errors.go`:

```go
var (
	ErrZKGasLimitReached = errors.New("zk gas limit reached")
)
```

Add the meter type in `core/vm/taiko_zk_gas.go`:

```go
type ZKGasMeter struct {
	blockLimit uint64
	blockUsed  uint64
	txUsed     uint64
}

func NewZKGasMeter(limit uint64) *ZKGasMeter {
	return &ZKGasMeter{blockLimit: limit}
}

func (m *ZKGasMeter) StartTx() { m.txUsed = 0 }
func (m *ZKGasMeter) AbortTx() { m.txUsed = 0 }
func (m *ZKGasMeter) CommitTx() { m.blockUsed += m.txUsed; m.txUsed = 0 }
```

```go
func (m *ZKGasMeter) OnOpcode(op OpCode, stepGas uint64, childFrameCreated bool, precompileInvoked bool) error {
	rawGas := stepGas
	if spawnEstimate, ok := zkGasSpawnEstimate[op]; ok && (childFrameCreated || precompileInvoked) {
		rawGas = spawnEstimate
	}
	return m.add(rawGas, zkGasOpcodeMultiplier[op])
}

func (m *ZKGasMeter) OnPrecompile(addr common.Address, gasUsed uint64) error {
	return m.add(gasUsed, zkGasPrecompileMultiplier[byte(addr.Bytes()[19])])
}
```

Extend `vm.Config` in `core/vm/interpreter.go`:

```go
type Config struct {
	Tracer    *tracing.Hooks
	ZKGasMeter *ZKGasMeter
	NoBaseFee bool
	// ...
}
```

Charge opcode gas after execution in `core/vm/interpreter.go`:

```go
gasBefore := gasCopy
res, err = operation.execute(&pc, evm, callContext)
stepGas := gasBefore - contract.Gas

if meter := evm.Config.ZKGasMeter; meter != nil {
	if err2 := meter.OnOpcode(op, stepGas, evm.lastSpawnCreatedChild, evm.lastSpawnPrecompile); err2 != nil {
		return nil, err2
	}
}
evm.lastSpawnCreatedChild = false
evm.lastSpawnPrecompile = false
```

Mark spawn/precompile paths in `core/vm/evm.go`:

```go
if isPrecompile {
	evm.lastSpawnPrecompile = true
	beforeGas := gas
	ret, gas, err = RunPrecompiledContract(stateDB, p, addr, input, gas, evm.Config.Tracer)
	if meter := evm.Config.ZKGasMeter; meter != nil {
		if err2 := meter.OnPrecompile(addr, beforeGas-gas); err2 != nil {
			return nil, gas, err2
		}
	}
} else {
	evm.lastSpawnCreatedChild = true
}
```

- [ ] **Step 4: Run the targeted tests and confirm they pass**

Run:

```bash
go test ./core/vm -run 'TestZKGasMeter' -count=1
```

Expected:

- all new meter unit tests PASS
- no unrelated `core/vm` compile regressions

- [ ] **Step 5: Commit**

```bash
git add core/vm/errors.go core/vm/interpreter.go core/vm/evm.go core/vm/taiko_zk_gas.go core/vm/taiko_zk_gas_tables.go core/vm/taiko_zk_gas_test.go
git commit -m "feat(vm): add Uzen zk gas metering"
```

## Task 5: Integrate zk-gas Metering Into Block Processing And Truncate On Overflow

**Files:**
- Modify: `core/state_processor.go`
- Modify: `core/state_processor_test.go`

- [ ] **Step 1: Write the failing block-processing test**

Add a focused state-processor test that proves the offending tx is dropped and later txs are skipped.

```go
func newSimpleDynamicTx(t *testing.T, cfg *params.ChainConfig, nonce uint64) *types.Transaction {
	t.Helper()
	key, err := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID:   cfg.ChainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(params.InitialBaseFee),
		Gas:       params.TxGas,
		To:        &to,
	}), types.LatestSigner(cfg), key)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func newHeavyDynamicTx(t *testing.T, cfg *params.ChainConfig, nonce uint64) *types.Transaction {
	t.Helper()
	key, err := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress("0x0000000000000000000000000000000000000001")
	data := common.FromHex("0x6001600055600160005560016000556001600055")
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID:   cfg.ChainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(params.InitialBaseFee),
		Gas:       5_000_000,
		To:        &to,
		Data:      data,
	}), types.LatestSigner(cfg), key)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestStateProcessorUzenZKGasTruncatesBlock(t *testing.T) {
	cfg := *params.MergedTestChainConfig
	cfg.Taiko = true
	uzenTime := uint64(100)
	cfg.UzenTime = &uzenTime

	genesis := &Genesis{Config: &cfg, BaseFee: big.NewInt(params.InitialBaseFee)}
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, genesis, beacon.New(ethash.NewFaker()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()

	block := types.NewBlock(
		&types.Header{
			Number:   common.Big1,
			Time:     uzenTime,
			GasLimit: 30_000_000,
			BaseFee:  big.NewInt(params.InitialBaseFee),
		},
		&types.Body{Transactions: []*types.Transaction{
			newSimpleDynamicTx(t, &cfg, 0),
			newHeavyDynamicTx(t, &cfg, 1),
			newSimpleDynamicTx(t, &cfg, 2),
		}},
		nil,
		trie.NewStackTrie(nil),
	)
	stateDB, _ := state.New(block.Root(), state.NewDatabase(chain.TrieDB(), nil))

	res, err := NewStateProcessor(chain).Process(context.Background(), block, stateDB, vm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.Receipts); got != 1 {
		t.Fatalf("expected only the first tx receipt to survive, got %d", got)
	}
}
```

- [ ] **Step 2: Run the targeted test and confirm it fails**

Run:

```bash
go test ./core -run 'TestStateProcessorUzenZKGasTruncatesBlock' -count=1
```

Expected:

- failure because `StateProcessor.Process` still applies every tx and has no Uzen zk-gas truncation path

- [ ] **Step 3: Integrate the meter into state processing**

Create the block-scoped meter when `Uzen` is active in `core/state_processor.go`:

```go
var zkMeter *vm.ZKGasMeter
if config.Taiko && config.IsUzen(header.Time) {
	zkMeter = vm.NewZKGasMeter(100_000_000)
	cfg.ZKGasMeter = zkMeter
}
evm := vm.NewEVM(context, tracingStateDB, config, cfg)
```

Snapshot state and gaspool per tx and discard the offending tx on `vm.ErrZKGasLimitReached`:

```go
for i, tx := range block.Transactions() {
	stateSnapshot := statedb.Snapshot()
	gasSnapshot := gp.Snapshot()
	if zkMeter != nil {
		zkMeter.StartTx()
	}

	receipt, err := ApplyTransactionWithEVM(msg, gp, statedb, blockNumber, blockHash, context.Time, tx, evm)
	if errors.Is(err, vm.ErrZKGasLimitReached) {
		statedb.RevertToSnapshot(stateSnapshot)
		gp.Set(gasSnapshot)
		zkMeter.AbortTx()
		break
	}
	if err != nil {
		return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
	}
	if zkMeter != nil {
		zkMeter.CommitTx()
	}
	receipts = append(receipts, receipt)
	allLogs = append(allLogs, receipt.Logs...)
}
```

Teach `ApplyTransactionWithEVM` to surface the sentinel before receipt creation:

```go
result, err := ApplyMessage(evm, msg, gp)
if err != nil {
	return nil, err
}
if errors.Is(result.Err, vm.ErrZKGasLimitReached) {
	return nil, vm.ErrZKGasLimitReached
}
```

- [ ] **Step 4: Run the targeted test and then the broader core suite**

Run:

```bash
go test ./core -run 'TestStateProcessorUzenZKGasTruncatesBlock|TestStateProcessorErrors' -count=1
```

Then run:

```bash
go test ./params ./core ./core/vm ./core/txpool ./beacon/engine ./eth ./eth/catalyst ./miner -count=1
```

Expected:

- the new truncation test PASSes
- existing state processor error tests remain green
- the touched package suites PASS together

- [ ] **Step 5: Commit**

```bash
git add core/state_processor.go core/state_processor_test.go
git commit -m "feat(core): truncate Uzen blocks on zk gas overflow"
```

## Self-Review Checklist

- [ ] Confirm every requirement in `docs/superpowers/specs/2026-04-12-uzen-design.md` maps to one of the five tasks above.
- [ ] Search the plan for placeholder words and remove them before implementation begins:

```bash
rg -n 'TODO|TBD|placeholder|implement later|similar to Task' docs/superpowers/plans/2026-04-12-uzen-implementation.md
```

- [ ] Verify the planned names stay consistent across tasks:
  - `UzenTime`
  - `IsUzen`
  - `ErrBlobTransactionsUnsupported`
  - `ErrZKGasLimitReached`
  - `ZKGasMeter`
  - `NormalizeUzenParentBeaconRoot`
  - `UzenRequestsHash`
