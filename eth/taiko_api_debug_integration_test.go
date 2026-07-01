package eth

import (
	"context"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

// txListWitnessTestChain builds an in-memory chain of `n` blocks. Block bodies
// have a DynamicFeeTx first (so the Taiko anchor-marking path is exercised),
// followed by a simple value transfer. Returns the blockchain and the blocks.
func txListWitnessTestChain(t *testing.T, n int) (*core.BlockChain, []*types.Block) {
	t.Helper()
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")

	cfg := *params.MergedTestChainConfig
	cfg.Taiko = true
	cfg.ChainID = big.NewInt(167000)

	gspec := &core.Genesis{
		Config: &cfg,
		Alloc:  types.GenesisAlloc{addr: {Balance: big.NewInt(1e18)}},
	}
	engine := beacon.New(ethash.NewFaker())
	db := rawdb.NewMemoryDatabase()

	_, blocks, _ := core.GenerateChainWithGenesis(gspec, engine, n, func(i int, gen *core.BlockGen) {
		signer := types.LatestSigner(&cfg)
		tx0, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
		}), signer, key)
		// CHANGE(taiko): mark the block's first tx as the anchor during generation
		// so GenerateChain's fee accounting matches the Taiko block-import path
		// (which marks index 0 as anchor and skips base-fee redirection); otherwise
		// InsertChain rejects the block with an invalid merkle root.
		if err := tx0.MarkAsAnchor(); err != nil {
			t.Fatalf("mark anchor: %v", err)
		}
		gen.AddTx(tx0)
		tx1, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(2),
		}), signer, key)
		gen.AddTx(tx1)
	})

	bc, err := core.NewBlockChain(db, gspec, engine, nil)
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("insert chain: %v", err)
	}
	return bc, blocks
}

func TestBuildTxListWitnessReproducesStateRoot(t *testing.T) {
	bc, blocks := txListWitnessTestChain(t, 2)
	defer bc.Stop()
	block := blocks[len(blocks)-1]

	witness, committed, err := buildTxListWitness(bc, block, block.Transactions(), txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != len(block.Transactions()) {
		t.Fatalf("expected all %d txs committed, got %d", len(block.Transactions()), len(committed))
	}
	if len(witness.Keys) == 0 {
		t.Fatalf("expected populated keys")
	}

	// Re-execute statelessly using only the witness; the state root must match.
	hdr := block.Header()
	hdr.Root = common.Hash{}
	hdr.ReceiptHash = common.Hash{}
	stateless := types.NewBlockWithHeader(hdr).WithBody(*block.Body())
	got, _, err := core.ExecuteStateless(context.Background(), bc.Config(), vm.Config{}, stateless, witness)
	if err != nil {
		t.Fatalf("ExecuteStateless: %v", err)
	}
	if got != block.Root() {
		t.Fatalf("state root mismatch: got %s want %s", got, block.Root())
	}
}

func TestBuildTxListWitnessSkipsInvalidNonAnchorTx(t *testing.T) {
	bc, blocks := txListWitnessTestChain(t, 1)
	defer bc.Stop()
	block := blocks[0]

	key, _ := crypto.GenerateKey() // unknown, unfunded sender
	signer := types.LatestSigner(bc.Config())
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")
	bad, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 5, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
	}), signer, key)

	anchor := block.Transactions()[0]
	valid := block.Transactions()[1]
	_, committed, err := buildTxListWitness(bc, block, types.Transactions{anchor, bad, valid}, txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("expected 2 committed (anchor + valid), got %d", len(committed))
	}
}

func TestBuildTxListWitnessAnchorFailureIsFatal(t *testing.T) {
	bc, blocks := txListWitnessTestChain(t, 1)
	defer bc.Stop()
	block := blocks[0]

	key, _ := crypto.GenerateKey() // unfunded
	signer := types.LatestSigner(bc.Config())
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")
	badAnchor, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 0, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
	}), signer, key)

	if _, _, err := buildTxListWitness(bc, block, types.Transactions{badAnchor}, txListWitnessOptions{}); err == nil {
		t.Fatalf("expected fatal error for failed anchor transaction")
	}
}

func TestExecutionWitnessForTxListEndToEnd(t *testing.T) {
	bc, blocks := txListWitnessTestChain(t, 2)
	defer bc.Stop()
	block := blocks[len(blocks)-1]

	rlpTxs, err := rlp.EncodeToBytes(block.Transactions())
	if err != nil {
		t.Fatalf("encode txs: %v", err)
	}
	bn := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(block.NumberU64()))

	out, err := executionWitnessForTxList(bc, bn, rlpTxs, nil, nil)
	if err != nil {
		t.Fatalf("executionWitnessForTxList: %v", err)
	}
	if len(out.State) == 0 || len(out.Headers) == 0 || len(out.Keys) == 0 {
		t.Fatalf("empty witness fields: %+v", out)
	}
	// headers must RLP-decode to a header.
	var h types.Header
	if err := rlp.DecodeBytes(out.Headers[0], &h); err != nil {
		t.Fatalf("headers must be RLP: %v", err)
	}
}

func TestExecutionWitnessForTxListRejectsCanonicalMode(t *testing.T) {
	bc, blocks := txListWitnessTestChain(t, 1)
	defer bc.Stop()
	bn := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blocks[0].NumberU64()))
	mode := "canonical"
	if _, err := executionWitnessForTxList(bc, bn, []byte{0xc0}, &mode, nil); err == nil {
		t.Fatalf("expected error for unsupported mode")
	}
}

// txListWitnessBeaconChain builds an in-memory Taiko chain on a Cancun/Prague
// -active config whose blocks carry an explicit non-zero ParentBeaconRoot, with
// the EIP-4788 beacon-roots and EIP-2935 history-storage system contracts
// deployed in genesis. This exercises the pre-execution system calls that
// buildTxListWitness must replay so the witness captures their state accesses.
func txListWitnessBeaconChain(t *testing.T, n int) (*core.BlockChain, []*types.Block) {
	t.Helper()
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")

	cfg := *params.MergedTestChainConfig
	cfg.Taiko = true
	cfg.ChainID = big.NewInt(167000)

	gspec := &core.Genesis{
		Config: &cfg,
		Alloc: types.GenesisAlloc{
			addr:                         {Balance: big.NewInt(1e18)},
			params.BeaconRootsAddress:    {Nonce: 1, Code: params.BeaconRootsCode, Balance: common.Big0},
			params.HistoryStorageAddress: {Nonce: 1, Code: params.HistoryStorageCode, Balance: common.Big0},
		},
	}
	engine := beacon.New(ethash.NewFaker())
	db := rawdb.NewMemoryDatabase()

	_, blocks, _ := core.GenerateChainWithGenesis(gspec, engine, n, func(i int, gen *core.BlockGen) {
		// CHANGE(taiko): set an explicit non-zero beacon root so the EIP-4788
		// system call writes storage and the block header carries a canonical,
		// non-zero ParentBeaconRoot for the witness replay to reproduce.
		gen.SetParentBeaconRoot(common.HexToHash("0x00000000000000000000000000000000000000000000000000000000cafebabe"))

		signer := types.LatestSigner(&cfg)
		tx0, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
		}), signer, key)
		if err := tx0.MarkAsAnchor(); err != nil {
			t.Fatalf("mark anchor: %v", err)
		}
		gen.AddTx(tx0)
		tx1, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(2),
		}), signer, key)
		gen.AddTx(tx1)
	})

	bc, err := core.NewBlockChain(db, gspec, engine, nil)
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("insert chain: %v", err)
	}
	return bc, blocks
}

// txListWitnessPragueChain builds an in-memory Taiko chain on a Prague-active
// config with the EIP-4788 beacon-roots, EIP-2935 history-storage, EIP-7002
// withdrawal-queue and EIP-7251 consolidation-queue system contracts deployed in
// genesis. This exercises both the pre-execution system calls and the Prague
// post-execution system calls (withdrawal/consolidation queues) that
// buildTxListWitness must replay so the witness captures their state accesses.
func txListWitnessPragueChain(t *testing.T, n int) (*core.BlockChain, []*types.Block) {
	t.Helper()
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")

	cfg := *params.MergedTestChainConfig // Prague-active (PragueTime == 0)
	cfg.Taiko = true
	cfg.ChainID = big.NewInt(167000)

	gspec := &core.Genesis{
		Config: &cfg,
		Alloc: types.GenesisAlloc{
			addr:                             {Balance: big.NewInt(1e18)},
			params.BeaconRootsAddress:        {Nonce: 1, Code: params.BeaconRootsCode, Balance: common.Big0},
			params.HistoryStorageAddress:     {Nonce: 1, Code: params.HistoryStorageCode, Balance: common.Big0},
			params.WithdrawalQueueAddress:    {Nonce: 1, Code: params.WithdrawalQueueCode, Balance: common.Big0},
			params.ConsolidationQueueAddress: {Nonce: 1, Code: params.ConsolidationQueueCode, Balance: common.Big0},
		},
	}
	engine := beacon.New(ethash.NewFaker())
	db := rawdb.NewMemoryDatabase()

	_, blocks, _ := core.GenerateChainWithGenesis(gspec, engine, n, func(i int, gen *core.BlockGen) {
		gen.SetParentBeaconRoot(common.HexToHash("0x00000000000000000000000000000000000000000000000000000000cafebabe"))

		signer := types.LatestSigner(&cfg)
		tx0, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
		}), signer, key)
		if err := tx0.MarkAsAnchor(); err != nil {
			t.Fatalf("mark anchor: %v", err)
		}
		gen.AddTx(tx0)
		tx1, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(2),
		}), signer, key)
		gen.AddTx(tx1)
	})

	bc, err := core.NewBlockChain(db, gspec, engine, nil)
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("insert chain: %v", err)
	}
	return bc, blocks
}

// TestBuildTxListWitnessAppliesPostExecutionSystemCalls verifies buildTxListWitness
// runs the Prague post-execution system calls (EIP-7002 withdrawal queue and
// EIP-7251 consolidation queue) after replaying transactions, so the witness
// records both queue system-contract accounts. Without the post-execution system
// calls neither account is touched and these keys are absent.
func TestBuildTxListWitnessAppliesPostExecutionSystemCalls(t *testing.T) {
	bc, blocks := txListWitnessPragueChain(t, 2)
	defer bc.Stop()
	block := blocks[len(blocks)-1]

	// Sanity: the target block must be Prague-active, otherwise postExecution
	// (and this test) would not exercise the EIP-7002/7251 system calls.
	if !bc.Config().IsPrague(block.Number(), block.Time()) {
		t.Fatalf("test block is not Prague-active; cannot exercise post-execution system calls")
	}

	witness, committed, err := buildTxListWitness(bc, block, block.Transactions(), txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != len(block.Transactions()) {
		t.Fatalf("expected all %d txs committed, got %d", len(block.Transactions()), len(committed))
	}

	// Required regression guard: both Prague post-execution queue system contracts
	// must appear in the witness key preimages. They are only recorded if the
	// post-execution system calls ran during witness building.
	if _, ok := witness.Keys[string(params.WithdrawalQueueAddress.Bytes())]; !ok {
		t.Fatalf("withdrawal-queue system-contract address missing from witness keys; " +
			"post-execution EIP-7002 system call was not applied")
	}
	if _, ok := witness.Keys[string(params.ConsolidationQueueAddress.Bytes())]; !ok {
		t.Fatalf("consolidation-queue system-contract address missing from witness keys; " +
			"post-execution EIP-7251 system call was not applied")
	}

	// Self-consistency: re-executing statelessly from the witness alone must
	// reproduce the canonical post-state root. This exercises the full
	// pre-execution + transaction replay + post-execution path and fails if the
	// witness omits state the canonical execution touched.
	hdr := block.Header()
	hdr.Root = common.Hash{}
	hdr.ReceiptHash = common.Hash{}
	stateless := types.NewBlockWithHeader(hdr).WithBody(*block.Body())
	got, _, err := core.ExecuteStateless(context.Background(), bc.Config(), vm.Config{}, stateless, witness)
	if err != nil {
		t.Fatalf("ExecuteStateless: %v", err)
	}
	if got != block.Root() {
		t.Fatalf("state root mismatch: got %s want %s", got, block.Root())
	}
}

// txListWitnessDeepPragueChain builds a Prague-active Taiko chain whose genesis
// funds many extra accounts, so the account trie is deep enough that the
// SystemAddress exclusion proof spans multiple trie nodes (not just the root).
// This is what makes the "missing system-caller node" regression observable: a
// shallow trie hides it because the root node alone proves the exclusion.
func txListWitnessDeepPragueChain(t *testing.T, extraAccounts int) (*core.BlockChain, []*types.Block) {
	t.Helper()
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")

	cfg := *params.MergedTestChainConfig // Prague-active (PragueTime == 0)
	cfg.Taiko = true
	cfg.ChainID = big.NewInt(167000)

	alloc := types.GenesisAlloc{
		addr:                             {Balance: big.NewInt(1e18)},
		params.BeaconRootsAddress:        {Nonce: 1, Code: params.BeaconRootsCode, Balance: common.Big0},
		params.HistoryStorageAddress:     {Nonce: 1, Code: params.HistoryStorageCode, Balance: common.Big0},
		params.WithdrawalQueueAddress:    {Nonce: 1, Code: params.WithdrawalQueueCode, Balance: common.Big0},
		params.ConsolidationQueueAddress: {Nonce: 1, Code: params.ConsolidationQueueCode, Balance: common.Big0},
	}
	for i := range extraAccounts {
		var a common.Address
		binary.BigEndian.PutUint64(a[:8], uint64(i+1))
		a[19] = 0x11
		alloc[a] = types.Account{Balance: big.NewInt(1)}
	}
	gspec := &core.Genesis{Config: &cfg, Alloc: alloc}
	engine := beacon.New(ethash.NewFaker())
	db := rawdb.NewMemoryDatabase()

	_, blocks, _ := core.GenerateChainWithGenesis(gspec, engine, 2, func(i int, gen *core.BlockGen) {
		gen.SetParentBeaconRoot(common.HexToHash("0x00000000000000000000000000000000000000000000000000000000cafebabe"))
		signer := types.LatestSigner(&cfg)
		tx0, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
		}), signer, key)
		if err := tx0.MarkAsAnchor(); err != nil {
			t.Fatalf("mark anchor: %v", err)
		}
		gen.AddTx(tx0)
	})

	bc, err := core.NewBlockChain(db, gspec, engine, nil)
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("insert chain: %v", err)
	}
	return bc, blocks
}

// accountProofNodes returns the account-trie proof (list of RLP-encoded nodes)
// for addr against the state trie rooted at root.
func accountProofNodes(t *testing.T, bc *core.BlockChain, root common.Hash, addr common.Address) [][]byte {
	t.Helper()
	accTrie, err := trie.NewStateTrie(trie.StateTrieID(root), bc.TrieDB())
	if err != nil {
		t.Fatalf("open state trie at %s: %v", root, err)
	}
	var nodes proofNodeList
	if err := accTrie.Prove(crypto.Keccak256(addr.Bytes()), &nodes); err != nil {
		t.Fatalf("prove %s: %v", addr, err)
	}
	return nodes
}

type proofNodeList [][]byte

func (p *proofNodeList) Put(key, value []byte) error { *p = append(*p, value); return nil }
func (p *proofNodeList) Delete(key []byte) error     { return nil }

// TestBuildTxListWitnessIncludesSystemCallCaller is a regression guard: the
// system-call caller account (params.SystemAddress, 0xff..fe) must appear in the
// witness — both its key preimage and every node of its account-trie proof.
//
// go-geth skips the value transfer for system calls, so it never loads the system
// caller and its account-trie path is absent from the witness. go-geth's own
// stateless re-execution skips it too, so the state-root self-consistency check
// cannot catch the gap; a cross-client stateless executor that loads the caller
// during its EIP-4788/2935/7002/7251 system calls fails to resolve the account
// without those nodes. The deep trie makes the missing nodes observable.
func TestBuildTxListWitnessIncludesSystemCallCaller(t *testing.T) {
	bc, blocks := txListWitnessDeepPragueChain(t, 2000)
	defer bc.Stop()
	block := blocks[len(blocks)-1]
	parent := bc.GetHeaderByHash(block.ParentHash())

	// Sanity: the trie must be deep enough that the exclusion proof is more than
	// the root node, otherwise this test would pass trivially and guard nothing.
	sysProof := accountProofNodes(t, bc, parent.Root, params.SystemAddress)
	if len(sysProof) < 2 {
		t.Fatalf("SystemAddress exclusion proof has %d node(s); need a deeper trie to guard the regression", len(sysProof))
	}

	witness, _, err := buildTxListWitness(bc, block, block.Transactions(), txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}

	if _, ok := witness.Keys[string(params.SystemAddress.Bytes())]; !ok {
		t.Fatalf("system-call caller %s missing from witness keys", params.SystemAddress)
	}
	for i, node := range sysProof {
		if _, ok := witness.State[string(node)]; !ok {
			t.Fatalf("system-call caller account-trie proof node %d/%d missing from witness state", i+1, len(sysProof))
		}
	}

	// The witness must still re-execute statelessly to the canonical root: the
	// extra system-caller nodes must not have perturbed the post-state.
	hdr := block.Header()
	hdr.Root = common.Hash{}
	hdr.ReceiptHash = common.Hash{}
	stateless := types.NewBlockWithHeader(hdr).WithBody(*block.Body())
	got, _, err := core.ExecuteStateless(context.Background(), bc.Config(), vm.Config{}, stateless, witness)
	if err != nil {
		t.Fatalf("ExecuteStateless: %v", err)
	}
	if got != block.Root() {
		t.Fatalf("state root mismatch: got %s want %s", got, block.Root())
	}
}

// TestBuildTxListWitnessAppliesPreExecutionSystemCalls verifies buildTxListWitness
// runs the EIP-4788 beacon-block-root system call before replaying transactions,
// so the witness records the beacon-roots system-contract account. Without the
// pre-execution system calls the account is never touched and this key is absent.
func TestBuildTxListWitnessAppliesPreExecutionSystemCalls(t *testing.T) {
	bc, blocks := txListWitnessBeaconChain(t, 2)
	defer bc.Stop()
	block := blocks[len(blocks)-1]

	// Sanity: the block must actually carry a non-nil, non-zero beacon root,
	// otherwise this test would not exercise the EIP-4788 pre-execution call.
	beaconRoot := block.BeaconRoot()
	if beaconRoot == nil {
		t.Fatalf("test block has no ParentBeaconRoot; cannot exercise EIP-4788")
	}
	if *beaconRoot == (common.Hash{}) {
		t.Fatalf("test block has zero ParentBeaconRoot; expected an explicit non-zero root")
	}

	witness, committed, err := buildTxListWitness(bc, block, block.Transactions(), txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != len(block.Transactions()) {
		t.Fatalf("expected all %d txs committed, got %d", len(block.Transactions()), len(committed))
	}

	// Required regression guard: the EIP-4788 beacon-roots system contract must
	// appear in the witness key preimages. It is only recorded if the
	// pre-execution beacon-block-root system call ran during witness building.
	if _, ok := witness.Keys[string(params.BeaconRootsAddress.Bytes())]; !ok {
		t.Fatalf("beacon-roots system-contract address missing from witness keys; " +
			"pre-execution EIP-4788 system call was not applied")
	}

	// Self-consistency: re-executing statelessly from the witness alone must
	// reproduce the canonical post-state root. This exercises the full
	// pre-execution + transaction replay path and fails if the witness omits
	// state the canonical execution touched.
	hdr := block.Header()
	hdr.Root = common.Hash{}
	hdr.ReceiptHash = common.Hash{}
	stateless := types.NewBlockWithHeader(hdr).WithBody(*block.Body())
	got, _, err := core.ExecuteStateless(context.Background(), bc.Config(), vm.Config{}, stateless, witness)
	if err != nil {
		t.Fatalf("ExecuteStateless: %v", err)
	}
	if got != block.Root() {
		t.Fatalf("state root mismatch: got %s want %s", got, block.Root())
	}
}

// storageProofNodes returns the storage-trie proof (RLP nodes) for slot in
// addr's storage against the state rooted at stateRoot.
func storageProofNodes(t *testing.T, bc *core.BlockChain, stateRoot common.Hash, addr common.Address, slot common.Hash) [][]byte {
	t.Helper()
	accTrie, err := trie.NewStateTrie(trie.StateTrieID(stateRoot), bc.TrieDB())
	if err != nil {
		t.Fatalf("open state trie at %s: %v", stateRoot, err)
	}
	acct, err := accTrie.GetAccount(addr)
	if err != nil {
		t.Fatalf("get account %s: %v", addr, err)
	}
	if acct == nil {
		t.Fatalf("account %s absent from state %s", addr, stateRoot)
	}
	stTrie, err := trie.NewStateTrie(trie.StorageTrieID(stateRoot, crypto.Keccak256Hash(addr.Bytes()), acct.Root), bc.TrieDB())
	if err != nil {
		t.Fatalf("open storage trie for %s: %v", addr, err)
	}
	var nodes proofNodeList
	if err := stTrie.Prove(crypto.Keccak256(slot.Bytes()), &nodes); err != nil {
		t.Fatalf("prove slot %s: %v", slot, err)
	}
	return nodes
}

// TestBuildTxListWitnessIncludesBlockhashHistoryStorage is a regression guard:
// when a transaction resolves a historical block hash via BLOCKHASH, the witness
// must include the EIP-2935 HistoryStorage (0x00..2935) storage-trie proof for
// the ring-buffer slot backing that hash.
//
// go-geth serves BLOCKHASH from the header chain and witnesses it as headers, so
// the HistoryStorage slot's storage nodes are absent. go-geth's own stateless
// re-execution also reads BLOCKHASH from headers, so the state-root
// self-consistency check cannot catch the gap; a cross-client stateless executor
// that resolves BLOCKHASH from the HistoryStorage contract fails to resolve its
// storage trie ("state trie unresolved") without those nodes.
//
// BLOCKHASH cannot run during chain generation (no chain context), so the
// BLOCKHASH transaction is supplied at replay time — exactly how the RPC feeds an
// explicit tx list to buildTxListWitness.
func TestBuildTxListWitnessIncludesBlockhashHistoryStorage(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")

	// Contract: PUSH0, BLOCKHASH, PUSH0, SSTORE, STOP => stores blockhash(0).
	bhContract := common.HexToAddress("0x00000000000000000000000000000000b10c4a54")
	bhCode := []byte{0x5f, 0x40, 0x5f, 0x55, 0x00}

	cfg := *params.MergedTestChainConfig // Prague-active (PragueTime == 0)
	cfg.Taiko = true
	cfg.ChainID = big.NewInt(167000)

	// Deep pre-populated HistoryStorage (slots 100..699) so the slot-0 proof
	// spans multiple nodes; leaves slots 0/1 fresh for the blocks' own EIP-2935
	// system calls. A shallow trie would hide the regression behind the root.
	hist := make(map[common.Hash]common.Hash)
	for i := 100; i < 700; i++ {
		var slot, val common.Hash
		binary.BigEndian.PutUint64(slot[24:], uint64(i))
		val[0] = 0xab
		binary.BigEndian.PutUint64(val[24:], uint64(i)+1)
		hist[slot] = val
	}
	gspec := &core.Genesis{
		Config: &cfg,
		Alloc: types.GenesisAlloc{
			addr:                             {Balance: big.NewInt(1e18)},
			bhContract:                       {Nonce: 1, Code: bhCode, Balance: common.Big0},
			params.BeaconRootsAddress:        {Nonce: 1, Code: params.BeaconRootsCode, Balance: common.Big0},
			params.HistoryStorageAddress:     {Nonce: 1, Code: params.HistoryStorageCode, Balance: common.Big0, Storage: hist},
			params.WithdrawalQueueAddress:    {Nonce: 1, Code: params.WithdrawalQueueCode, Balance: common.Big0},
			params.ConsolidationQueueAddress: {Nonce: 1, Code: params.ConsolidationQueueCode, Balance: common.Big0},
		},
	}
	engine := beacon.New(ethash.NewFaker())
	db := rawdb.NewMemoryDatabase()

	_, blocks, _ := core.GenerateChainWithGenesis(gspec, engine, 2, func(i int, gen *core.BlockGen) {
		gen.SetParentBeaconRoot(common.HexToHash("0x00000000000000000000000000000000000000000000000000000000cafebabe"))
		signer := types.LatestSigner(&cfg)
		tx0, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID: cfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
			GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
		}), signer, key)
		if err := tx0.MarkAsAnchor(); err != nil {
			t.Fatalf("mark anchor: %v", err)
		}
		gen.AddTx(tx0)
	})

	bc, err := core.NewBlockChain(db, gspec, engine, nil)
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	defer bc.Stop()
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("insert chain: %v", err)
	}

	block := blocks[len(blocks)-1] // block 2; parent = block 1 (addr nonce == 1)
	parent := bc.GetHeaderByHash(block.ParentHash())

	// BLOCKHASH(0) reads HistoryStorage slot 0. Block 2's own EIP-2935 system
	// call writes slot 1, so slot 0 enters the witness only if the BLOCKHASH read
	// path does. Sanity-check the proof is multi-node so this guards something.
	var slot common.Hash // slot 0
	stProof := storageProofNodes(t, bc, parent.Root, params.HistoryStorageAddress, slot)
	if len(stProof) < 2 {
		t.Fatalf("HistoryStorage slot-0 proof has %d node(s); need a deeper trie to guard the regression", len(stProof))
	}

	signer := types.LatestSigner(&cfg)
	anchor, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: cfg.ChainID, Nonce: 1, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(1),
	}), signer, key)
	callBH, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: cfg.ChainID, Nonce: 2, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 100000, To: &bhContract, Value: big.NewInt(0),
	}), signer, key)
	replay := types.Transactions{anchor, callBH}

	witness, committed, err := buildTxListWitness(bc, block, replay, txListWitnessOptions{SkipZkGasDifficultyCheck: true})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("expected anchor + BLOCKHASH tx committed, got %d", len(committed))
	}

	if _, ok := witness.Keys[string(slot.Bytes())]; !ok {
		t.Fatalf("HistoryStorage BLOCKHASH slot key preimage missing from witness keys")
	}
	for i, node := range stProof {
		if _, ok := witness.State[string(node)]; !ok {
			t.Fatalf("HistoryStorage BLOCKHASH slot proof node %d/%d missing from witness state", i+1, len(stProof))
		}
	}
}
