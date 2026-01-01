package core

import "testing"

func TestDecodeShastaBasefeeSharingPctg(t *testing.T) {
	if got := DecodeShastaBasefeeSharingPctg(nil); got != 0 {
		t.Fatalf("expected 0 for empty extradata, got %d", got)
	}
	if got := DecodeShastaBasefeeSharingPctg([]byte{0x2a, 0x99, 0x88}); got != 0x2a {
		t.Fatalf("expected 0x2a, got %d", got)
	}
}

func TestDecodeOntakeExtraDataBackwardCompat(t *testing.T) {
	tests := []struct {
		name  string
		extra []byte
		want  uint8
	}{
		{name: "empty", extra: nil, want: 0},
		{name: "single byte", extra: []byte{0x2a}, want: 0x2a},
		{name: "two bytes", extra: []byte{0x00, 0x32}, want: 0x32},
		{name: "multi bytes", extra: []byte{0x01, 0x02}, want: 0x02},
	}
	for _, test := range tests {
		if got := DecodeOntakeExtraData(test.extra); got != test.want {
			t.Fatalf("%s: expected %d, got %d", test.name, test.want, got)
		}
	}
}

func TestDecodeShastaProposalIDFromExtraData(t *testing.T) {
	extra := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0a}
	id, err := DecodeShastaProposalID(extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Uint64() != 10 {
		t.Fatalf("expected 10, got %d", id.Uint64())
	}
}

func TestDecodeShastaProposalIDFromExtraDataInvalid(t *testing.T) {
	if _, err := DecodeShastaProposalID([]byte{0x01}); err == nil {
		t.Fatal("expected error for short extradata")
	}
}
