package core

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestEtnaSimulationExtraData pins the simulated extraData of an Etna child
// for each parent shape, with the shared cross-client values.
func TestEtnaSimulationExtraData(t *testing.T) {
	for _, tt := range []struct {
		name   string
		number int64
		extra  []byte
		want   []byte
	}{
		{"empty genesis", 0, nil, make([]byte, 13)},
		{"seven bytes", 0, []byte{25, 0, 0, 0, 0, 1, 2}, []byte{25, 0, 0, 0, 0, 1, 2, 0, 0, 0, 0, 0, 0}},
		{"thirteen bytes", 1, bytes.Repeat([]byte{9}, 13), bytes.Repeat([]byte{9}, 13)},
		{"empty non-genesis", 1, nil, nil},
		{"other length", 0, []byte{25, 1, 2, 3, 4}, []byte{25, 1, 2, 3, 4}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := &types.Header{Number: big.NewInt(tt.number), Extra: tt.extra}
			got := EtnaSimulationExtraData(parent)
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("extraData = %x, want %x", got, tt.want)
			}
			if len(got) > 0 && len(tt.extra) > 0 {
				got[0]++
				if parent.Extra[0] != tt.extra[0] {
					t.Fatal("the simulated extraData aliases the parent's")
				}
			}
		})
	}
}

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
