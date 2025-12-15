package eth

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func TestAnchorV4ProposalID(t *testing.T) {
	proposalType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "proposalId", Type: "uint48"},
		{Name: "proposer", Type: "address"},
		{Name: "proverAuth", Type: "bytes"},
	})
	if err != nil {
		t.Fatalf("failed to build proposal params type: %v", err)
	}

	blockType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "anchorBlockNumber", Type: "uint48"},
		{Name: "anchorBlockHash", Type: "bytes32"},
		{Name: "anchorStateRoot", Type: "bytes32"},
	})
	if err != nil {
		t.Fatalf("failed to build block params type: %v", err)
	}

	args := abi.Arguments{
		{Type: proposalType},
		{Type: blockType},
	}

	expectedProposalID := big.NewInt(10)
	proposal := struct {
		ProposalID *big.Int       `abi:"proposalId"`
		Proposer   common.Address `abi:"proposer"`
		ProverAuth []byte         `abi:"proverAuth"`
	}{
		ProposalID: expectedProposalID,
		Proposer:   common.HexToAddress("0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc"),
		ProverAuth: []byte{0x01, 0x02, 0x03},
	}

	block := struct {
		AnchorBlockNumber *big.Int `abi:"anchorBlockNumber"`
		AnchorBlockHash   [32]byte `abi:"anchorBlockHash"`
		AnchorStateRoot   [32]byte `abi:"anchorStateRoot"`
	}{
		AnchorBlockNumber: big.NewInt(100),
		AnchorBlockHash:   common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"),
		AnchorStateRoot:   common.HexToHash("0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"),
	}

	encodedArgs, err := args.Pack(proposal, block)
	if err != nil {
		t.Fatalf("failed to pack anchorV4 args: %v", err)
	}

	data := append([]byte{}, taiko.AnchorV4Selector...)
	data = append(data, encodedArgs...)

	proposalID, err := AnchorV4ProposalID(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := new(big.Int).Set(expectedProposalID)
	if proposalID.Cmp(expected) != 0 {
		t.Fatalf("expected proposal ID %s, got %s", expected, proposalID)
	}
}

func TestAnchorV4ProposalIDInvalidData(t *testing.T) {
	if _, err := AnchorV4ProposalID([]byte{0x10, 0x0f}); err == nil {
		t.Fatal("expected error for malformed calldata")
	}
}

func TestMergeL1Origin_PreservesPopulatedFieldsOnPartialUpdate(t *testing.T) {
	existing := &rawdb.L1Origin{
		BlockID:       big.NewInt(123),
		L2BlockHash:   common.HexToHash("0x01"),
		L1BlockHeight: big.NewInt(100),
		L1BlockHash:   common.HexToHash("0x02"),
	}

	var buildID [8]byte
	buildID[0] = 0xAA
	var sig [65]byte
	sig[0] = 0xBB
	update := &rawdb.L1Origin{
		BlockID:            big.NewInt(123),
		BuildPayloadArgsID: buildID,
		Signature:          sig,
		// L1 fields intentionally omitted (zero values).
	}

	merged := mergeL1Origin(existing, update)
	if merged == nil {
		t.Fatal("expected merged origin")
	}
	if merged.L1BlockHeight == nil || merged.L1BlockHeight.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected L1BlockHeight preserved, got %v", merged.L1BlockHeight)
	}
	if merged.L1BlockHash != common.HexToHash("0x02") {
		t.Fatalf("expected L1BlockHash preserved, got %v", merged.L1BlockHash)
	}
	if merged.BuildPayloadArgsID != buildID {
		t.Fatalf("expected BuildPayloadArgsID updated, got %x", merged.BuildPayloadArgsID)
	}
	if merged.Signature != sig {
		t.Fatalf("expected Signature updated")
	}
}

func TestMergeL1Origin_UpdatesPreconfToAnchored(t *testing.T) {
	existing := &rawdb.L1Origin{
		BlockID:       big.NewInt(1),
		L2BlockHash:   common.HexToHash("0x10"),
		L1BlockHeight: nil,
		L1BlockHash:   common.Hash{},
	}

	update := &rawdb.L1Origin{
		BlockID:       big.NewInt(1),
		L1BlockHeight: big.NewInt(999),
		L1BlockHash:   common.HexToHash("0x20"),
	}

	merged := mergeL1Origin(existing, update)
	if merged.L1BlockHeight == nil || merged.L1BlockHeight.Cmp(big.NewInt(999)) != 0 {
		t.Fatalf("expected L1BlockHeight updated, got %v", merged.L1BlockHeight)
	}
	if merged.L1BlockHash != common.HexToHash("0x20") {
		t.Fatalf("expected L1BlockHash updated, got %v", merged.L1BlockHash)
	}
}

func TestMergeL1Origin_AllowsHeightZeroWhenHashSet(t *testing.T) {
	existing := &rawdb.L1Origin{
		BlockID:       big.NewInt(1),
		L1BlockHeight: big.NewInt(7),
		L1BlockHash:   common.HexToHash("0x07"),
	}

	update := &rawdb.L1Origin{
		BlockID:       big.NewInt(1),
		L1BlockHeight: big.NewInt(0),
		L1BlockHash:   common.HexToHash("0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3"),
	}

	merged := mergeL1Origin(existing, update)
	if merged.L1BlockHeight == nil || merged.L1BlockHeight.Sign() != 0 {
		t.Fatalf("expected L1BlockHeight updated to 0, got %v", merged.L1BlockHeight)
	}
	if merged.L1BlockHash != update.L1BlockHash {
		t.Fatalf("expected L1BlockHash updated, got %v", merged.L1BlockHash)
	}
}
