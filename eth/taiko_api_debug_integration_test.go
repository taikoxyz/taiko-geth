package eth

import (
	"context"
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
