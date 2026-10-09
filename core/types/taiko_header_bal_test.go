// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// CHANGE(taiko): Preserve EIP-7928 fields when reading L1 headers.
func TestHeaderBALJSONHash(t *testing.T) {
	hash := common.HexToHash("0x0123456789abcdef")
	one := uint64(1)
	header := Header{Number: big.NewInt(1), Difficulty: big.NewInt(0),
		GasLimit: 60000000, Time: 1791500000, BaseFee: big.NewInt(1000000000),
		WithdrawalsHash: &hash, BlobGasUsed: &one, ExcessBlobGas: &one,
		ParentBeaconRoot: &hash, RequestsHash: &hash, SlotNumber: &one}
	data, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["blockAccessListHash"] = json.RawMessage(`"0x00000000000000000000000000000000aabbccddeeff00112233445566778899"`)
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Header
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	want := common.HexToHash("0xa0b985526c11666e3da8990ac0c07ab5ad84cab8fdabddf37a5e295235ce3245")
	if got := decoded.Hash(); got != want {
		t.Fatalf("header hash: got %s, want %s", got, want)
	}
	if decoded.BlockAccessListHash == nil || *decoded.BlockAccessListHash != common.HexToHash(headerBALValue) {
		t.Fatal("JSON decoding lost BAL hash")
	}
	if decoded.SlotNumber == nil || *decoded.SlotNumber != one {
		t.Fatal("JSON decoding lost slot number")
	}
	encoded, err := json.Marshal(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Header
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if got := roundTrip.Hash(); got != want {
		t.Fatalf("JSON round trip changed hash: got %s, want %s", got, want)
	}
}

// These bytes and hashes were captured before adding BlockAccessListHash.
// The fixtures represent legacy L1, the Taiko L2 base-fee header schema, and Prague L1.
const (
	headerBALLegacyBody = "a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000940000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008001840393870080846ac81ee080a00000000000000000000000000000000000000000000000000000000000000000880000000000000000"
	headerBALLondonTail = "843b9aca00"
	headerBALPragueTail = "843b9aca00a00000000000000000000000000000000000000000000000000123456789abcdef0101a00000000000000000000000000000000000000000000000000123456789abcdefa00000000000000000000000000000000000000000000000000123456789abcdef"
	headerBALValue      = "00000000000000000000000000000000aabbccddeeff00112233445566778899"
)

func headerBALFixture() *Header {
	hash := common.HexToHash("0x0123456789abcdef")
	one := uint64(1)
	bal := common.HexToHash(headerBALValue)
	return &Header{Number: big.NewInt(1), Difficulty: big.NewInt(0),
		GasLimit: 60000000, Time: 1791500000, BaseFee: big.NewInt(1000000000),
		WithdrawalsHash: &hash, BlobGasUsed: &one, ExcessBlobGas: &one,
		ParentBeaconRoot: &hash, RequestsHash: &hash, BlockAccessListHash: &bal, SlotNumber: &one}
}

func TestHeaderBALRLPRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bal, slot bool
		rlp       string
		hash      string
	}{
		{"bal_and_slot", true, true, "f90281" + headerBALLegacyBody + headerBALPragueTail + "a0" + headerBALValue + "01", "0xa0b985526c11666e3da8990ac0c07ab5ad84cab8fdabddf37a5e295235ce3245"},
		{"bal_only", true, false, "f90280" + headerBALLegacyBody + headerBALPragueTail + "a0" + headerBALValue, "0x0c684917556c551e2dbbfe14af9e5b88007fca0a7f8fb07f19d27b19b649592b"},
		{"neither", false, false, "f9025f" + headerBALLegacyBody + headerBALPragueTail, "0x6c67738268744a5563b0557feadaa29b39dbd7cad6a2939f3adf8507283e885d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := headerBALFixture()
			if !tc.bal {
				h.BlockAccessListHash = nil
			}
			if !tc.slot {
				h.SlotNumber = nil
			}
			wantRLP := common.FromHex(tc.rlp)
			encoded, err := rlp.EncodeToBytes(h)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, wantRLP) {
				t.Fatalf("RLP mismatch: got %x, want %x", encoded, wantRLP)
			}
			var decoded Header
			if err := rlp.DecodeBytes(wantRLP, &decoded); err != nil {
				t.Fatal(err)
			}
			if (decoded.BlockAccessListHash != nil) != tc.bal || (decoded.SlotNumber != nil) != tc.slot {
				t.Fatal("optional field presence changed")
			}
			if tc.bal && *decoded.BlockAccessListHash != *h.BlockAccessListHash {
				t.Fatal("BAL hash changed")
			}
			if tc.slot && *decoded.SlotNumber != *h.SlotNumber {
				t.Fatal("slot number changed")
			}
			if got := decoded.Hash(); got != common.HexToHash(tc.hash) {
				t.Fatalf("hash: got %s, want %s", got, tc.hash)
			}
			reencoded, err := rlp.EncodeToBytes(&decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(reencoded, wantRLP) {
				t.Fatal("RLP changed after round trip")
			}
			if got := NewBlockWithHeader(&decoded).Hash(); got != common.HexToHash(tc.hash) {
				t.Fatalf("block hash: got %s, want %s", got, tc.hash)
			}
		})
	}
}

func TestHeaderBALLegacyEncoding(t *testing.T) {
	for _, tc := range []struct{ name, rlp, hash string }{
		{"legacy_l1", "f901f5" + headerBALLegacyBody, "0x47ec26d5e9e84d61b47a30c045522a6624afdab74e8fe23938c284cfbf2b3600"},
		{"taiko_l2", "f901fa" + headerBALLegacyBody + headerBALLondonTail, "0x0c9e79fdef3291c0f0be728b5b5c5d7a6b497ff4dd6a3aa97aa2744e3acc11ff"},
		{"prague_l1", "f9025f" + headerBALLegacyBody + headerBALPragueTail, "0x6c67738268744a5563b0557feadaa29b39dbd7cad6a2939f3adf8507283e885d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantRLP := common.FromHex(tc.rlp)
			var h Header
			if err := rlp.DecodeBytes(wantRLP, &h); err != nil {
				t.Fatal(err)
			}
			if h.BlockAccessListHash != nil || h.SlotNumber != nil {
				t.Fatal("absent trailing fields were populated")
			}
			encoded, err := rlp.EncodeToBytes(&h)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, wantRLP) {
				t.Fatalf("legacy RLP changed: got %x, want %x", encoded, wantRLP)
			}
			if got := h.Hash(); got != common.HexToHash(tc.hash) {
				t.Fatalf("legacy hash: got %s, want %s", got, tc.hash)
			}
		})
	}
}

func TestHeaderBALHashMutation(t *testing.T) {
	h := headerBALFixture()
	before := h.Hash()
	h.BlockAccessListHash[0] ^= 1
	if h.Hash() == before {
		t.Fatal("changing BAL did not change the header hash")
	}
}

func TestHeaderBALCopies(t *testing.T) {
	for _, tc := range []struct {
		name string
		copy func(*Header) *Header
	}{
		{"CopyHeader", CopyHeader},
		{"Block.Header", func(h *Header) *Header { return NewBlockWithHeader(h).Header() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := headerBALFixture()
			copied := tc.copy(original)
			if copied.BlockAccessListHash == original.BlockAccessListHash {
				t.Fatal("copy shares BAL pointer")
			}
			if copied.BlockAccessListHash == nil || *copied.BlockAccessListHash != *original.BlockAccessListHash {
				t.Fatal("copy lost BAL value")
			}
			if copied.Hash() != original.Hash() {
				t.Fatal("copy changed header hash")
			}
			copied.BlockAccessListHash[0] ^= 1
			if *copied.BlockAccessListHash == *original.BlockAccessListHash {
				t.Fatal("copy mutation reached original")
			}
		})
	}
	// Block construction and repeated Header calls must both isolate the stored header.
	original := headerBALFixture()
	block := NewBlockWithHeader(original)
	want := *original.BlockAccessListHash
	original.BlockAccessListHash[0] ^= 1
	returned := block.Header()
	if returned.BlockAccessListHash == nil || *returned.BlockAccessListHash != want {
		t.Fatal("input mutation reached block")
	}
	returned.BlockAccessListHash[0] ^= 1
	if got := block.Header().BlockAccessListHash; got == nil || *got != want {
		t.Fatal("returned header mutation reached block")
	}
	if got := block.Hash(); got != common.HexToHash("0xa0b985526c11666e3da8990ac0c07ab5ad84cab8fdabddf37a5e295235ce3245") {
		t.Fatalf("block hash changed: %s", got)
	}
}

func TestHeaderBALMalformedJSON(t *testing.T) {
	data, err := json.Marshal(headerBALFixture())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, value string }{
		{"empty", "0x"},
		{"short", "0x" + strings.Repeat("11", 31)},
		{"long", "0x" + strings.Repeat("11", 33)},
		{"non_hex", "0x" + strings.Repeat("zz", 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields["blockAccessListHash"], err = json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			malformed, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			var h Header
			if err := json.Unmarshal(malformed, &h); err == nil {
				t.Fatal("malformed BAL was accepted")
			}
		})
	}
}

func TestHeaderBALMalformedRLP(t *testing.T) {
	// Build malformed inputs from the independently pinned canonical fields.
	var fields []rlp.RawValue
	canonical := common.FromHex("f90281" + headerBALLegacyBody + headerBALPragueTail + "a0" + headerBALValue + "01")
	if err := rlp.DecodeBytes(canonical, &fields); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"empty", []byte{}},
		{"short", make([]byte, 31)},
		{"long", make([]byte, 33)},
		{"list", []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := rlp.EncodeToBytes(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			fields[21] = encoded
			malformed, err := rlp.EncodeToBytes(fields)
			if err != nil {
				t.Fatal(err)
			}
			var h Header
			if err := rlp.DecodeBytes(malformed, &h); err == nil {
				t.Fatal("malformed BAL was accepted")
			}
		})
	}
}
