package core

import "testing"

func TestDecodeBasefeeSharingPctg(t *testing.T) {
	if got := DecodeBasefeeSharingPctg(nil); got != 0 {
		t.Fatalf("expected 0 for empty extradata, got %d", got)
	}
	if got := DecodeBasefeeSharingPctg([]byte{0x2a, 0x99, 0x88}); got != 0x2a {
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

func TestDecodeProposalIDFromExtraData(t *testing.T) {
	extra := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0a}
	id, err := DecodeProposalID(extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Uint64() != 10 {
		t.Fatalf("expected 10, got %d", id.Uint64())
	}
}

func TestDecodeProposalIDFromExtraDataInvalid(t *testing.T) {
	if _, err := DecodeProposalID([]byte{0x01}); err == nil {
		t.Fatal("expected error for short extradata")
	}
}
