package eth

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
)

func TestAnchorV4ProposalID(t *testing.T) {
	proposalType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "proposalId", Type: "uint48"},
	})
	if err != nil {
		t.Fatalf("failed to build proposal params type: %v", err)
	}

	checkpointType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "blockNumber", Type: "uint48"},
		{Name: "blockHash", Type: "bytes32"},
		{Name: "stateRoot", Type: "bytes32"},
	})
	if err != nil {
		t.Fatalf("failed to build checkpoint type: %v", err)
	}

	args := abi.Arguments{
		{Type: proposalType},
		{Type: checkpointType},
	}

	expectedProposalID := big.NewInt(10)
	proposal := struct {
		ProposalID *big.Int `abi:"proposalId"`
	}{
		ProposalID: expectedProposalID,
	}

	checkpoint := struct {
		BlockNumber *big.Int `abi:"blockNumber"`
		BlockHash   [32]byte `abi:"blockHash"`
		StateRoot   [32]byte `abi:"stateRoot"`
	}{
		BlockNumber: big.NewInt(100),
		BlockHash:   common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"),
		StateRoot:   common.HexToHash("0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"),
	}

	encodedArgs, err := args.Pack(proposal, checkpoint)
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
