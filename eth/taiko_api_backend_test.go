package eth

import (
	"testing"

	"github.com/ethereum/go-ethereum/core"
)

func TestProposalIDFromExtraData(t *testing.T) {
	extra := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0a}
	proposalID, err := core.DecodeProposalID(extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposalID.Uint64() != 10 {
		t.Fatalf("expected 10, got %d", proposalID.Uint64())
	}
}

func TestProposalIDFromExtraDataInvalid(t *testing.T) {
	if _, err := core.DecodeProposalID([]byte{0x01}); err == nil {
		t.Fatal("expected error for short extradata")
	}
}
