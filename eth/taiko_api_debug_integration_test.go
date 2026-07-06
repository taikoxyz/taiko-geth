package eth

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"maps"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

// witnessChainConfig parameterizes newTxListWitnessChain. The zero value builds
// a two-block chain whose blocks each carry a single anchor transaction.
type witnessChainConfig struct {
	blocks        int                // chain length; 0 means 2
	transferTxs   int                // plain transfers per block after the anchor
	beaconRoot    bool               // give every block a non-zero ParentBeaconRoot
	alloc         types.GenesisAlloc // extra genesis accounts (system/test contracts)
	extraAccounts int                // filler accounts that deepen the account trie
}

// newTxListWitnessChain builds an in-memory Taiko chain on a Cancun+Prague
// -active config. Every block starts with a DynamicFeeTx marked as the anchor
// (exercising the Taiko anchor path) followed by cfg.transferTxs value
// transfers, all sent by the returned funded key.
func newTxListWitnessChain(t *testing.T, cfg witnessChainConfig) (*core.BlockChain, []*types.Block, *ecdsa.PrivateKey) {
	t.Helper()
	if cfg.blocks == 0 {
		cfg.blocks = 2
	}
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")

	chainCfg := *params.MergedTestChainConfig
	chainCfg.Taiko = true
	chainCfg.ChainID = big.NewInt(167000)

	alloc := types.GenesisAlloc{addr: {Balance: big.NewInt(1e18)}}
	maps.Copy(alloc, cfg.alloc)
	// Filler accounts deepen the account trie so exclusion proofs span multiple
	// nodes: a shallow trie proves absence with the root node alone, hiding
	// missing-node regressions.
	for i := range cfg.extraAccounts {
		var a common.Address
		binary.BigEndian.PutUint64(a[:8], uint64(i+1))
		a[19] = 0x11
		alloc[a] = types.Account{Balance: big.NewInt(1)}
	}

	gspec := &core.Genesis{Config: &chainCfg, Alloc: alloc}
	engine := beacon.New(ethash.NewFaker())
	db := rawdb.NewMemoryDatabase()

	signer := types.LatestSigner(&chainCfg)
	_, blocks, _ := core.GenerateChainWithGenesis(gspec, engine, cfg.blocks, func(i int, gen *core.BlockGen) {
		if cfg.beaconRoot {
			// CHANGE(taiko): an explicit non-zero beacon root makes the EIP-4788
			// system call write storage and gives the block header a canonical,
			// non-zero ParentBeaconRoot for the witness replay to reproduce.
			gen.SetParentBeaconRoot(common.HexToHash("0x00000000000000000000000000000000000000000000000000000000cafebabe"))
		}
		for j := 0; j <= cfg.transferTxs; j++ {
			tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
				ChainID: chainCfg.ChainID, Nonce: gen.TxNonce(addr), GasTipCap: big.NewInt(0),
				GasFeeCap: gen.BaseFee(), Gas: 100000, To: &dst, Value: big.NewInt(int64(j + 1)),
			}), signer, key)
			if j == 0 {
				// CHANGE(taiko): mark the block's first tx as the anchor during
				// generation so GenerateChain's fee accounting matches the Taiko
				// block-import path (which marks index 0 as anchor and skips
				// base-fee redirection); otherwise InsertChain rejects the block
				// with an invalid merkle root.
				if err := tx.MarkAsAnchor(); err != nil {
					t.Fatalf("mark anchor: %v", err)
				}
			}
			gen.AddTx(tx)
		}
	})

	bc, err := core.NewBlockChain(db, gspec, engine, nil)
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatalf("insert chain: %v", err)
	}
	t.Cleanup(bc.Stop)
	return bc, blocks, key
}

// systemContractsAlloc returns the EIP-4788 beacon-roots and EIP-2935
// history-storage system contracts for a genesis alloc, so the pre-execution
// system calls run against deployed contracts.
func systemContractsAlloc() types.GenesisAlloc {
	return types.GenesisAlloc{
		params.BeaconRootsAddress:    {Nonce: 1, Code: params.BeaconRootsCode, Balance: common.Big0},
		params.HistoryStorageAddress: {Nonce: 1, Code: params.HistoryStorageCode, Balance: common.Big0},
	}
}

// assertStatelessReplay re-executes the block's canonical body from the witness
// alone and asserts it reproduces the canonical post-state root. Witnesses that
// carry extra pure-read state are a harmless superset for this check.
func assertStatelessReplay(t *testing.T, bc *core.BlockChain, block *types.Block, witness *stateless.Witness) {
	t.Helper()
	hdr := block.Header()
	hdr.Root = common.Hash{}
	hdr.ReceiptHash = common.Hash{}
	replay := types.NewBlockWithHeader(hdr).WithBody(*block.Body())
	got, _, err := core.ExecuteStateless(context.Background(), bc.Config(), vm.Config{}, replay, witness)
	if err != nil {
		t.Fatalf("ExecuteStateless: %v", err)
	}
	if got != block.Root() {
		t.Fatalf("state root mismatch: got %s want %s", got, block.Root())
	}
}

func TestBuildTxListWitnessReproducesStateRoot(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{transferTxs: 1})
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
	assertStatelessReplay(t, bc, block, witness)
}

func TestBuildTxListWitnessSkipsInvalidNonAnchorTx(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{blocks: 1, transferTxs: 1})
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
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{blocks: 1, transferTxs: 1})
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
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{transferTxs: 1})
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
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{blocks: 1})
	bn := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blocks[0].NumberU64()))
	mode := "canonical"
	if _, err := executionWitnessForTxList(bc, bn, []byte{0xc0}, &mode, nil); err == nil {
		t.Fatalf("expected error for unsupported mode")
	}
}

// TestBuildTxListWitnessSkipsPostExecutionQueueCalls is a regression guard: the
// replay must NOT run the Prague post-execution system calls (EIP-7002
// withdrawal queue, EIP-7251 consolidation queue). The cross-client reference
// block executor never performs them (its requests are unconditionally empty),
// so witnessing the queue contracts' account state would leak geth-only nodes
// into the witness. The queue contracts are deliberately deployed here so the
// guard proves the replay leaves them untouched even when present.
func TestBuildTxListWitnessSkipsPostExecutionQueueCalls(t *testing.T) {
	alloc := systemContractsAlloc()
	alloc[params.WithdrawalQueueAddress] = types.Account{Nonce: 1, Code: params.WithdrawalQueueCode, Balance: common.Big0}
	alloc[params.ConsolidationQueueAddress] = types.Account{Nonce: 1, Code: params.ConsolidationQueueCode, Balance: common.Big0}
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{transferTxs: 1, beaconRoot: true, alloc: alloc})
	block := blocks[len(blocks)-1]

	// Sanity: the target block must be Prague-active, otherwise this test would
	// not guard against the EIP-7002/7251 system calls sneaking back in.
	if !bc.Config().IsPrague(block.Number(), block.Time()) {
		t.Fatalf("test block is not Prague-active; cannot exercise post-execution queue behavior")
	}

	witness, committed, err := buildTxListWitness(bc, block, block.Transactions(), txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != len(block.Transactions()) {
		t.Fatalf("expected all %d txs committed, got %d", len(block.Transactions()), len(committed))
	}

	// No stateless re-execution check here: with the queue contracts deployed,
	// canonical processing does call them, so this witness intentionally lacks
	// their state. Real Taiko networks do not deploy the queue contracts.
	if _, ok := witness.Keys[string(params.WithdrawalQueueAddress.Bytes())]; ok {
		t.Fatalf("withdrawal-queue system-contract address must not appear in witness keys; " +
			"the reference executor never runs the EIP-7002 system call")
	}
	if _, ok := witness.Keys[string(params.ConsolidationQueueAddress.Bytes())]; ok {
		t.Fatalf("consolidation-queue system-contract address must not appear in witness keys; " +
			"the reference executor never runs the EIP-7251 system call")
	}
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
// system-call caller account's (params.SystemAddress, 0xff..fe) account-trie
// exclusion proof must appear in the witness state — every node of it,
// including the terminal divergence node — while its key preimage must not.
//
// The reference EVM touches the caller during the EIP-4788/2935 system calls,
// so the reference witness proves the account's absence (verified against a
// live reference node: the terminal exclusion node is present there). The
// caller account never exists, so per the existence rule it carries no key
// preimage. go-geth's system calls skip the caller entirely, so without an
// explicit load its exclusion path is missing and the witness under-covers.
// The deep trie makes the missing nodes observable.
func TestBuildTxListWitnessIncludesSystemCallCaller(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{
		beaconRoot: true, alloc: systemContractsAlloc(), extraAccounts: 2000,
	})
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

	if _, ok := witness.Keys[string(params.SystemAddress.Bytes())]; ok {
		t.Fatalf("system-call caller %s must not appear in witness keys", params.SystemAddress)
	}
	for i, node := range sysProof {
		if _, ok := witness.State[string(node)]; !ok {
			t.Fatalf("system-call caller exclusion-proof node %d/%d missing from witness state", i+1, len(sysProof))
		}
	}

	// The witness must still re-execute statelessly to the canonical root.
	assertStatelessReplay(t, bc, block, witness)
}

// TestBuildTxListWitnessIncludesAbsentSystemContractExclusionProof is a regression
// guard: a system contract touched by a pre-execution system call but NOT deployed
// on-chain (as on a Taiko L2 that activates Prague/Cancun without the EIP system
// contracts) must appear in the witness with a COMPLETE account-trie exclusion
// proof, not just a key preimage.
//
// go-geth loads the absent contract during the EIP-4788/EIP-2935 system call and
// records its address in the witness keys, but getStateObject returns at the
// acct==nil check before prefetching, so the account-trie exclusion-proof nodes
// never enter the witness. go-geth's own stateless re-execution reads the same
// absent account and tolerates the gap, so the state-root self-consistency check
// stays green; a cross-client stateless executor that resolves the absent account
// from the sparse trie fails ("state trie unresolved") without those nodes. The
// deep trie makes the missing intermediate nodes observable.
func TestBuildTxListWitnessIncludesAbsentSystemContractExclusionProof(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{beaconRoot: true, extraAccounts: 2000})
	block := blocks[len(blocks)-1]
	parent := bc.GetHeaderByHash(block.ParentHash())

	// The system contracts must actually be absent, otherwise this would test the
	// inclusion path instead of the absent-account exclusion path.
	statedb, err := bc.StateAt(parent.Root)
	if err != nil {
		t.Fatalf("state at parent: %v", err)
	}
	for _, addr := range []common.Address{params.HistoryStorageAddress, params.BeaconRootsAddress} {
		if len(statedb.GetCode(addr)) != 0 {
			t.Fatalf("%s unexpectedly deployed; test requires an absent contract", addr)
		}
	}

	witness, _, err := buildTxListWitness(bc, block, block.Transactions(), txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}

	// Both the EIP-2935 history-storage and EIP-4788 beacon-roots contracts are
	// touched by the pre-execution system calls. Each must carry every node of
	// its account-trie exclusion proof, but no key preimage: the cross-client
	// wire format only carries keys of accounts that exist post-execution.
	for _, tc := range []struct {
		name string
		addr common.Address
	}{
		{"history-storage", params.HistoryStorageAddress},
		{"beacon-roots", params.BeaconRootsAddress},
	} {
		proof := accountProofNodes(t, bc, parent.Root, tc.addr)
		if len(proof) < 2 {
			t.Fatalf("%s exclusion proof has %d node(s); need a deeper trie to guard the regression", tc.name, len(proof))
		}
		if _, ok := witness.Keys[string(tc.addr.Bytes())]; ok {
			t.Fatalf("%s (%s) must not appear in witness keys; the account does not exist", tc.name, tc.addr)
		}
		for i, node := range proof {
			if _, ok := witness.State[string(node)]; !ok {
				t.Fatalf("%s exclusion-proof node %d/%d missing from witness state", tc.name, i+1, len(proof))
			}
		}
	}

	// The extra exclusion-proof nodes must not perturb the post-state: the witness
	// must still re-execute statelessly to the canonical root.
	assertStatelessReplay(t, bc, block, witness)
}

func TestExecutionWitnessIncludesAbsentSystemContractExclusionProof(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{beaconRoot: true, extraAccounts: 2000})
	block := blocks[len(blocks)-1]
	parent := bc.GetHeaderByHash(block.ParentHash())

	out, err := executionWitnessForBlock(bc, block)
	if err != nil {
		t.Fatalf("executionWitnessForBlock: %v", err)
	}
	if len(out.State) == 0 || len(out.Keys) == 0 || len(out.Headers) == 0 {
		t.Fatalf("empty witness fields: %+v", out)
	}
	var decoded types.Header
	if err := rlp.DecodeBytes(out.Headers[0], &decoded); err != nil {
		t.Fatalf("headers must be RLP: %v", err)
	}

	for _, tc := range []struct {
		name string
		addr common.Address
	}{
		{"history-storage", params.HistoryStorageAddress},
		{"beacon-roots", params.BeaconRootsAddress},
	} {
		proof := accountProofNodes(t, bc, parent.Root, tc.addr)
		if len(proof) < 2 {
			t.Fatalf("%s exclusion proof has %d node(s); need a deeper trie to guard the regression", tc.name, len(proof))
		}
		for _, key := range out.Keys {
			if string(key) == string(tc.addr.Bytes()) {
				t.Fatalf("%s (%s) must not appear in witness keys; the account does not exist", tc.name, tc.addr)
			}
		}
		for i, node := range proof {
			stateFound := false
			for _, got := range out.State {
				if string(got) == string(node) {
					stateFound = true
					break
				}
			}
			if !stateFound {
				t.Fatalf("%s exclusion-proof node %d/%d missing from witness state", tc.name, i+1, len(proof))
			}
		}
	}
}

// TestBuildTxListWitnessAppliesPreExecutionSystemCalls verifies buildTxListWitness
// runs the EIP-4788 beacon-block-root system call before replaying transactions,
// so the witness records the beacon-roots system-contract account. Without the
// pre-execution system calls the account is never touched and this key is absent.
func TestBuildTxListWitnessAppliesPreExecutionSystemCalls(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{
		transferTxs: 1, beaconRoot: true, alloc: systemContractsAlloc(),
	})
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
	assertStatelessReplay(t, bc, block, witness)
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

// TestBuildTxListWitnessRecordsBlockhashAncestorHeaders is a regression guard:
// when a replayed transaction resolves a historical block hash via BLOCKHASH,
// the witness must carry that ancestor's header. The cross-client stateless
// executor serves BLOCKHASH from the witness ancestor headers (EIP-2935 does
// not change the opcode's semantics), so a missing header would make the
// replay unverifiable. The EIP-2935 history contract's storage is not involved
// — real Taiko networks do not deploy the contract, matching this chain.
//
// BLOCKHASH cannot run during chain generation (no chain context), so the
// BLOCKHASH transaction is supplied at replay time — exactly how the RPC feeds
// an explicit tx list to buildTxListWitness.
func TestBuildTxListWitnessRecordsBlockhashAncestorHeaders(t *testing.T) {
	// Contract: PUSH0, BLOCKHASH, POP, STOP => resolves blockhash(0).
	bhContract := common.HexToAddress("0x00000000000000000000000000000000b10c4a54")
	bc, blocks, key := newTxListWitnessChain(t, witnessChainConfig{alloc: types.GenesisAlloc{
		bhContract: {Nonce: 1, Code: []byte{0x5f, 0x40, 0x50, 0x00}, Balance: common.Big0},
	}})
	block := blocks[len(blocks)-1] // block 2: blockhash(0) is inside the 256-block window

	signer := types.LatestSigner(bc.Config())
	callBH, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 2, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 100000, To: &bhContract, Value: big.NewInt(0),
	}), signer, key)
	replay := types.Transactions{block.Transactions()[0], callBH}

	witness, committed, err := buildTxListWitness(bc, block, replay, txListWitnessOptions{SkipZkGasDifficultyCheck: true})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("expected anchor + BLOCKHASH tx committed, got %d", len(committed))
	}

	found := false
	for _, hdr := range witness.Headers {
		if hdr.Number.Uint64() == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ancestor header 0 resolved via BLOCKHASH missing from witness headers")
	}

	// The header pull is a pure read: the witness must still re-execute the
	// block's canonical body to the canonical post-state root.
	assertStatelessReplay(t, bc, block, witness)
}

// TestBuildTxListWitnessIncludesUntouchedStorageRootNode is a regression guard
// for the cross-client legacy witness format: the witness state must carry the
// parent-state storage-trie root node of every loaded account even when the
// replay never reads or writes any of its storage slots, and the empty storage
// trie's root node is the RLP empty string (0x80).
//
// go-geth only collects storage nodes for tries the execution walks, so a
// called contract whose code never touches storage contributes no storage
// nodes, and accounts with empty storage never contribute the 0x80 node. Both
// are inert for go-geth's own stateless re-execution (the account leaf already
// carries the storage root hash), which is why the state-root self-consistency
// check cannot catch the gap.
func TestBuildTxListWitnessIncludesUntouchedStorageRootNode(t *testing.T) {
	// STOP-only code with populated storage: calling it loads the account and
	// code but walks no storage-trie path.
	storContract := common.HexToAddress("0x000000000000000000000000000000005700a6e1")
	bc, blocks, key := newTxListWitnessChain(t, witnessChainConfig{alloc: types.GenesisAlloc{
		storContract: {
			Nonce:   1,
			Code:    []byte{0x00}, // STOP: loads account+code, reads no slot
			Balance: common.Big0,
			Storage: map[common.Hash]common.Hash{
				common.HexToHash("0x01"): common.HexToHash("0xff"),
				common.HexToHash("0x02"): common.HexToHash("0xfe"),
			},
		},
	}})
	block := blocks[len(blocks)-1]
	parent := bc.GetHeaderByHash(block.ParentHash())

	// The storage-trie root node is the first node of any slot proof.
	rootNode := storageProofNodes(t, bc, parent.Root, storContract, common.HexToHash("0x01"))[0]

	signer := types.LatestSigner(bc.Config())
	callTx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 2, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 100000, To: &storContract, Value: big.NewInt(0),
	}), signer, key)
	replay := types.Transactions{block.Transactions()[0], callTx}

	witness, committed, err := buildTxListWitness(bc, block, replay, txListWitnessOptions{SkipZkGasDifficultyCheck: true})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("expected anchor + storage-contract call committed, got %d", len(committed))
	}

	if _, ok := witness.State[string(rootNode)]; !ok {
		t.Fatalf("storage-trie root node of untouched-storage account %s missing from witness state", storContract)
	}
	if _, ok := witness.State[string([]byte{0x80})]; !ok {
		t.Fatalf("empty storage-trie root node (0x80) missing from witness state")
	}

	// The extra nodes are pure pre-state reads: the witness must still re-execute
	// the canonical block body to the canonical post-state root.
	assertStatelessReplay(t, bc, block, witness)
}

// TestExecutionWitnessForTxListOmitsAbsentAccountKeys verifies the wire-level
// keys field only carries preimages of accounts that exist when the replay
// finishes: absent system contracts and the system caller are probed by the
// pre-execution system calls but must not surface as keys, while existing
// accounts (the sender) must keep theirs.
func TestExecutionWitnessForTxListOmitsAbsentAccountKeys(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{beaconRoot: true, extraAccounts: 16})
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

	absent := []common.Address{
		params.BeaconRootsAddress,
		params.HistoryStorageAddress,
		params.WithdrawalQueueAddress,
		params.ConsolidationQueueAddress,
		params.SystemAddress,
	}
	for _, key := range out.Keys {
		for _, addr := range absent {
			if string(key) == string(addr.Bytes()) {
				t.Fatalf("nonexistent account %s must not appear in witness keys", addr)
			}
		}
	}

	sender, err := types.LatestSigner(bc.Config()).Sender(block.Transactions()[0])
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	senderFound := false
	for _, key := range out.Keys {
		if string(key) == string(sender.Bytes()) {
			senderFound = true
			break
		}
	}
	if !senderFound {
		t.Fatalf("existing sender account %s missing from witness keys", sender)
	}
}

// TestBuildTxListWitnessIncludesCreatedContractCode is a regression guard for
// the cross-client legacy witness format: bytecode deployed during the replay
// must appear in the witness codes even when the created contract is never
// called afterwards. A read-driven collector records code only on code reads,
// so a deploy-and-stop transaction leaves the runtime bytecode out.
func TestBuildTxListWitnessIncludesCreatedContractCode(t *testing.T) {
	bc, blocks, senderKey := newTxListWitnessChain(t, witnessChainConfig{})
	block := blocks[len(blocks)-1]

	// Init code returns the 2-byte runtime {PUSH0, STOP}.
	runtime := []byte{0x5f, 0x00}
	initcode := []byte{0x60, 0x02, 0x80, 0x60, 0x0b, 0x60, 0x00, 0x39, 0x60, 0x00, 0xf3, 0x5f, 0x00}

	signer := types.LatestSigner(bc.Config())
	deploy, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 2, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 300000, To: nil, Value: big.NewInt(0), Data: initcode,
	}), signer, senderKey)
	replay := types.Transactions{block.Transactions()[0], deploy}

	witness, committed, err := buildTxListWitness(bc, block, replay, txListWitnessOptions{SkipZkGasDifficultyCheck: true})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("expected anchor + deploy committed, got %d", len(committed))
	}

	if _, ok := witness.Codes[string(runtime)]; !ok {
		t.Fatalf("bytecode deployed during replay missing from witness codes")
	}
}

// newStorageShapeChain builds a Taiko chain whose genesis deploys a contract
// with exactly two storage slots chosen so their hashed keys share a
// three-nibble prefix: the storage trie is an extension (key c25) into a
// two-child branch. The contract code stores calldata: SSTORE(calldata[0:32] ->
// calldata[32:64]), letting replayed transactions insert or delete chosen slots.
//
//	slot 3   -> keccak c2575a0e...
//	slot 497 -> keccak c254b84a...
func newStorageShapeChain(t *testing.T) (*core.BlockChain, []*types.Block, *ecdsa.PrivateKey, common.Address) {
	t.Helper()
	contract := common.HexToAddress("0x000000000000000000000000000000005700a6e3")
	val := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	bc, blocks, key := newTxListWitnessChain(t, witnessChainConfig{alloc: types.GenesisAlloc{
		contract: {
			Nonce: 1,
			// PUSH1 32 CALLDATALOAD PUSH1 0 CALLDATALOAD SSTORE STOP
			Code:    []byte{0x60, 0x20, 0x35, 0x60, 0x00, 0x35, 0x55, 0x00},
			Balance: common.Big0,
			Storage: map[common.Hash]common.Hash{
				common.HexToHash("0x03"):  val, // keccak c2575a0e...
				common.HexToHash("0x1f1"): val, // keccak c254b84a...
			},
		},
	}})
	return bc, blocks, key, contract
}

// storeCallTx signs a transaction calling the storage-shape contract with
// calldata SSTORE(slot -> value).
func storeCallTx(t *testing.T, bc *core.BlockChain, key *ecdsa.PrivateKey, contract common.Address, baseFee *big.Int, nonce uint64, slot, value common.Hash) *types.Transaction {
	t.Helper()
	data := append(slot.Bytes(), value.Bytes()...)
	signer := types.LatestSigner(bc.Config())
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: nonce, GasTipCap: big.NewInt(0),
		GasFeeCap: baseFee, Gas: 200000, To: &contract, Value: big.NewInt(0), Data: data,
	}), signer, key)
	if err != nil {
		t.Fatalf("sign store tx: %v", err)
	}
	return tx
}

// runStorageShapeReplay replays [anchor, SSTORE(slot->value)] on the shape
// chain and returns the witness plus the parent-state proof nodes of genesis
// slot 3 (ext, branch, leaf) and slot 497.
func runStorageShapeReplay(t *testing.T, slot, value common.Hash) (*stateless.Witness, [][]byte, [][]byte) {
	t.Helper()
	bc, blocks, key, contract := newStorageShapeChain(t)
	block := blocks[len(blocks)-1]
	parent := bc.GetHeaderByHash(block.ParentHash())

	proof3 := storageProofNodes(t, bc, parent.Root, contract, common.HexToHash("0x03"))
	proof497 := storageProofNodes(t, bc, parent.Root, contract, common.HexToHash("0x1f1"))
	// Shape guard: ext -> branch -> leaf. A different shape would silently
	// invalidate what these tests guard.
	if len(proof3) != 3 || len(proof497) != 3 {
		t.Fatalf("unexpected storage trie shape: proof lengths %d/%d, want 3/3", len(proof3), len(proof497))
	}

	call := storeCallTx(t, bc, key, contract, block.BaseFee(), 2, slot, value)
	replay := types.Transactions{block.Transactions()[0], call}
	witness, committed, err := buildTxListWitness(bc, block, replay, txListWitnessOptions{SkipZkGasDifficultyCheck: true})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("expected anchor + store call committed, got %d", len(committed))
	}
	return witness, proof3, proof497
}

// TestBuildTxListWitnessRevealsExtensionChildOnInsert is a regression guard for
// the cross-client legacy witness format: inserting a storage slot whose hashed
// key splits an extension node must reveal the extension's child node in the
// witness state. Slot 241 hashes to c29b...: it shares two nibbles with the
// genesis extension (key c25) and splits it at its final nibble, so the
// exclusion proof ends at the extension and the child branch is otherwise
// absent from the witness.
func TestBuildTxListWitnessRevealsExtensionChildOnInsert(t *testing.T) {
	witness, proof3, _ := runStorageShapeReplay(t,
		common.HexToHash("0xf1"), common.HexToHash("0x01"))
	if _, ok := witness.State[string(proof3[1])]; !ok {
		t.Fatalf("extension child branch missing from witness state after extension-splitting insert")
	}
}

// TestBuildTxListWitnessRevealsExtensionChildOnMidKeySplitInsert covers the
// mid-key variant: slot 10 hashes to c65a..., sharing only one nibble with the
// genesis extension (key c25), so the split leaves a shortened extension in
// place. The reference sparse-trie mutation still requires the extension's
// child node revealed.
func TestBuildTxListWitnessRevealsExtensionChildOnMidKeySplitInsert(t *testing.T) {
	witness, proof3, _ := runStorageShapeReplay(t,
		common.HexToHash("0x0a"), common.HexToHash("0x01"))
	if _, ok := witness.State[string(proof3[1])]; !ok {
		t.Fatalf("extension child branch missing from witness state after mid-key extension-splitting insert")
	}
}

// TestBuildTxListWitnessRevealsCollapseSiblingOnDelete is a regression guard
// for the cross-client legacy witness format: deleting a storage slot whose
// removal leaves its parent branch with a single surviving child must reveal
// that surviving child node (here the untouched leaf of slot 497), because the
// reference sparse-trie collapse cannot merge the branch without it.
func TestBuildTxListWitnessRevealsCollapseSiblingOnDelete(t *testing.T) {
	witness, _, proof497 := runStorageShapeReplay(t,
		common.HexToHash("0x03"), common.Hash{})
	if _, ok := witness.State[string(proof497[2])]; !ok {
		t.Fatalf("surviving sibling leaf missing from witness state after branch-collapsing delete")
	}
}

// goldenTouchTestKey is the well-known golden-touch signing key (also used by
// the consensus package tests).
func goldenTouchTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA("92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38")
	if err != nil {
		t.Fatalf("golden touch key: %v", err)
	}
	if got := crypto.PubkeyToAddress(key.PublicKey); got != taiko.GoldenTouchAccount {
		t.Fatalf("golden touch key derives %s, want %s", got, taiko.GoldenTouchAccount)
	}
	return key
}

// goldenTouchTripleTx signs a transaction from the golden touch to the chain's
// treasury with the golden touch's block-start nonce — the reference anchor
// identity.
func goldenTouchTripleTx(t *testing.T, bc *core.BlockChain, baseFee *big.Int) *types.Transaction {
	t.Helper()
	treasury := core.TaikoTreasuryAddress(bc.Config().ChainID)
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 0, GasTipCap: big.NewInt(0),
		GasFeeCap: baseFee, Gas: 1_000_000, To: &treasury, Value: common.Big0,
	}), types.LatestSigner(bc.Config()), goldenTouchTestKey(t))
	if err != nil {
		t.Fatalf("sign golden touch tx: %v", err)
	}
	return tx
}

// TestBuildTxListWitnessGoldenTouchTripleGetsAnchorExemptions: the reference
// grants anchor execution exemptions to the (golden touch, block-start nonce,
// treasury) triple at any list position — the unfunded golden touch must
// execute without buying gas even when it is not the first transaction.
func TestBuildTxListWitnessGoldenTouchTripleGetsAnchorExemptions(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{blocks: 1, transferTxs: 1})
	block := blocks[0]

	t.Run("first position", func(t *testing.T) {
		// A fresh tx per subtest: the transaction object must carry no
		// in-memory anchor flag from a previous replay.
		gtTx := goldenTouchTripleTx(t, bc, block.BaseFee())
		_, committed, err := buildTxListWitness(bc, block, types.Transactions{gtTx}, txListWitnessOptions{})
		if err != nil {
			t.Fatalf("buildTxListWitness: %v", err)
		}
		if len(committed) != 1 {
			t.Fatalf("expected the golden touch tx committed, got %d", len(committed))
		}
	})

	t.Run("later position", func(t *testing.T) {
		gtTx := goldenTouchTripleTx(t, bc, block.BaseFee())
		first := block.Transactions()[0] // funded sender, executes as a normal tx
		_, committed, err := buildTxListWitness(bc, block, types.Transactions{first, gtTx}, txListWitnessOptions{})
		if err != nil {
			t.Fatalf("buildTxListWitness: %v", err)
		}
		if len(committed) != 2 {
			t.Fatalf("expected both txs committed, got %d", len(committed))
		}
		if committed[1].Hash() != gtTx.Hash() {
			t.Fatalf("expected the golden touch tx committed at position 1")
		}
	})
}

// TestBuildTxListWitnessFirstTxWithoutTripleGetsNoExemptions: a first-position
// transaction from an ordinary zero-balance sender is a normal fee-paying
// transaction in the reference, so it must fail the replay fatally instead of
// silently executing with anchor exemptions.
func TestBuildTxListWitnessFirstTxWithoutTripleGetsNoExemptions(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{blocks: 1})
	block := blocks[0]

	key, _ := crypto.GenerateKey() // zero balance
	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")
	tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 0, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 100000, To: &dst, Value: common.Big0,
	}), types.LatestSigner(bc.Config()), key)

	if _, _, err := buildTxListWitness(bc, block, types.Transactions{tx}, txListWitnessOptions{}); err == nil {
		t.Fatalf("expected fatal error: zero-balance first tx must pay for gas")
	}
}

// TestBuildTxListWitnessLegacyFirstTxExecutes: the reference has no
// transaction-type requirement on the first list position; a funded legacy
// transaction must replay as a normal transaction instead of failing the
// request.
func TestBuildTxListWitnessLegacyFirstTxExecutes(t *testing.T) {
	bc, blocks, key := newTxListWitnessChain(t, witnessChainConfig{blocks: 1, transferTxs: 1})
	block := blocks[0]

	dst := common.HexToAddress("0x00000000000000000000000000000000deadbeef")
	// The replay runs on the parent (genesis) state, where the funded key's
	// next nonce is 0.
	legacy, err := types.SignTx(types.NewTx(&types.LegacyTx{
		Nonce: 0, To: &dst, Value: common.Big0, Gas: 100000, GasPrice: block.BaseFee(),
	}), types.LatestSigner(bc.Config()), key)
	if err != nil {
		t.Fatalf("sign legacy tx: %v", err)
	}

	_, committed, err := buildTxListWitness(bc, block, types.Transactions{legacy}, txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 1 {
		t.Fatalf("expected the legacy tx committed, got %d", len(committed))
	}
}

// TestBuildTxListWitnessSkipsOversizedInitcodeTx: an over-limit initcode
// creation is a recoverable skip in the reference replay, not a fatal error.
func TestBuildTxListWitnessSkipsOversizedInitcodeTx(t *testing.T) {
	bc, blocks, key := newTxListWitnessChain(t, witnessChainConfig{blocks: 1, transferTxs: 1})
	block := blocks[0]

	signer := types.LatestSigner(bc.Config())
	oversized, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: bc.Config().ChainID, Nonce: 2, GasTipCap: big.NewInt(0),
		GasFeeCap: block.BaseFee(), Gas: 2_000_000, To: nil, Value: common.Big0,
		Data: make([]byte, params.MaxInitCodeSize+1),
	}), signer, key)
	if err != nil {
		t.Fatalf("sign oversized initcode tx: %v", err)
	}

	first := block.Transactions()[0]
	second := block.Transactions()[1]
	_, committed, err := buildTxListWitness(bc, block, types.Transactions{first, second, oversized}, txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("expected oversized-initcode tx skipped, got %d committed", len(committed))
	}
}

// TestExecutionWitnessForTxListDropsUnrecoverableSigner: a transaction whose
// signature cannot be recovered is dropped at decode time, before list
// positions are assigned — the request must succeed with the remaining
// transactions taking their shifted positions.
func TestExecutionWitnessForTxListDropsUnrecoverableSigner(t *testing.T) {
	bc, blocks, _ := newTxListWitnessChain(t, witnessChainConfig{blocks: 1, transferTxs: 1})
	block := blocks[0]

	junk, err := block.Transactions()[0].WithSignature(types.LatestSigner(bc.Config()), make([]byte, 65))
	if err != nil {
		t.Fatalf("make junk-signature tx: %v", err)
	}
	txs := append(types.Transactions{junk}, block.Transactions()...)
	rlpTxs, err := rlp.EncodeToBytes(txs)
	if err != nil {
		t.Fatalf("encode txs: %v", err)
	}

	bn := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(block.NumberU64()))
	out, err := executionWitnessForTxList(bc, bn, rlpTxs, nil, nil)
	if err != nil {
		t.Fatalf("executionWitnessForTxList: %v", err)
	}
	if len(out.State) == 0 {
		t.Fatalf("expected a witness for the recoverable remainder of the list")
	}
}
