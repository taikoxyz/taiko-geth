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

package ethapi

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// CHANGE(taiko): RPC header responses must retain the fields committed by the hash.
func TestRPCMarshalHeaderBALRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../../core/types/testdata/taiko_sepolia_bal_header.json")
	if err != nil {
		t.Fatal(err)
	}
	var header types.Header
	if err := json.Unmarshal(data, &header); err != nil {
		t.Fatal(err)
	}
	want := common.HexToHash("0xfd4eb21751cb7c7d94d75f61c9f6364558923a2db73599f9ed917ee65d8c5680")
	fields := RPCMarshalHeader(&header)
	if _, ok := fields["blockAccessListHash"]; !ok {
		t.Error("RPC response dropped BAL hash")
	}
	if fields["hash"] != want {
		t.Fatalf("RPC hash: got %v, want %s", fields["hash"], want)
	}
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.Header
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.Hash(); got != want {
		t.Errorf("RPC round trip hash: got %s, want %s", got, want)
	}
	if decoded.BlockAccessListHash == nil || *decoded.BlockAccessListHash != *header.BlockAccessListHash {
		t.Error("RPC round trip lost BAL hash")
	}
	if decoded.SlotNumber == nil || *decoded.SlotNumber != *header.SlotNumber {
		t.Error("RPC round trip lost slot number")
	}

	header.BlockAccessListHash = nil
	header.SlotNumber = nil
	fields = RPCMarshalHeader(&header)
	for _, field := range []string{"blockAccessListHash", "slotNumber"} {
		if _, ok := fields[field]; ok {
			t.Errorf("nil %s should be omitted", field)
		}
	}
}
