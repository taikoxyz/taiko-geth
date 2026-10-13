package core

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func TestEtnaBasefeeSharing(t *testing.T) {
	for _, tt := range []struct {
		name         string
		extra        []byte
		pctg         uint8
		redistribute bool
	}{
		{"thirteen bytes", []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 25, true},
		{"zero share", make([]byte, 13), 0, true},
		{"seven bytes", []byte{25, 0, 0, 0, 0, 0, 1}, 0, false},
		{"empty", nil, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pctg, redistribute := etnaBasefeeSharing(tt.extra)
			if pctg != tt.pctg || redistribute != tt.redistribute {
				t.Fatalf("share = (%d, %v), want (%d, %v)", pctg, redistribute, tt.pctg, tt.redistribute)
			}
		})
	}
}

// TestTaikoRPCBasefeeSharing pins the base-fee share of RPC calls: from Etna
// on only the 13-byte layout shares extra[0], and before Etna the whole base
// fee goes to the treasury, as today.
func TestTaikoRPCBasefeeSharing(t *testing.T) {
	etnaTime := uint64(10)
	config := &params.ChainConfig{Taiko: true, EtnaTime: &etnaTime}
	for _, tt := range []struct {
		name         string
		time         uint64
		extra        []byte
		pctg         uint8
		redistribute bool
	}{
		{"before etna", 9, []byte{25, 0, 0, 0, 0, 0, 1}, 0, true},
		{"before etna, thirteen bytes", 9, []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 0, true},
		{"etna", 10, []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 25, true},
		{"etna genesis", 10, nil, 0, false},
		{"etna seven bytes", 10, []byte{25, 0, 0, 0, 0, 0, 1}, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pctg, redistribute := TaikoRPCBasefeeSharing(config, &types.Header{Time: tt.time, Extra: tt.extra})
			if pctg != tt.pctg || redistribute != tt.redistribute {
				t.Fatalf("share = (%d, %v), want (%d, %v)", pctg, redistribute, tt.pctg, tt.redistribute)
			}
		})
	}
}

// TestSetTaikoRPCBasefeeSharing pins that an RPC message takes the Etna share
// of its block context, overwriting any earlier share, and that before Etna
// the message is left as it was.
func TestSetTaikoRPCBasefeeSharing(t *testing.T) {
	etnaTime := uint64(10)
	config := &params.ChainConfig{Taiko: true, EtnaTime: &etnaTime}
	for _, tt := range []struct {
		name     string
		time     uint64
		extra    []byte
		pctg     uint8
		skipping bool
	}{
		{"etna thirteen bytes", 10, []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 25, false},
		{"etna seven bytes", 10, []byte{25, 0, 0, 0, 0, 0, 1}, 0, true},
		{"before etna leaves the message untouched", 9, []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1}, 7, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{BasefeeSharingPctg: 7, SkipBasefeeRedistribution: true}
			SetTaikoRPCBasefeeSharing(msg, config, &types.Header{Time: tt.time, Extra: tt.extra})
			if msg.BasefeeSharingPctg != tt.pctg || msg.SkipBasefeeRedistribution != tt.skipping {
				t.Fatalf("message share = (%d, skip %v), want (%d, skip %v)",
					msg.BasefeeSharingPctg, msg.SkipBasefeeRedistribution, tt.pctg, tt.skipping)
			}
		})
	}
}
