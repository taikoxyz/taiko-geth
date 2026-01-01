package eth

import (
	"testing"

	"github.com/ethereum/go-ethereum/core"
)

func TestShastaProposalIDFromExtraData(t *testing.T) {
	extra := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0a}
	proposalID, err := core.DecodeShastaProposalID(extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposalID.Uint64() != 10 {
		t.Fatalf("expected 10, got %d", proposalID.Uint64())
	}
}

func TestShastaProposalIDFromExtraDataInvalid(t *testing.T) {
	if _, err := core.DecodeShastaProposalID([]byte{0x01}); err == nil {
		t.Fatal("expected error for short extradata")
	}
}
