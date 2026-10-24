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

package rpc

import (
	"encoding/json"
	"testing"
)

// taikoValueMarshaler implements json.Marshaler on the value receiver, as the
// gencodec output for rawdb.L1Origin does.
type taikoValueMarshaler struct{ N int }

func (v taikoValueMarshaler) MarshalJSON() ([]byte, error) { return json.Marshal(v.N) }

type taikoLookupService struct{}

func (taikoLookupService) Missing() (*taikoValueMarshaler, error) { return nil, nil }

func (taikoLookupService) Found() (*taikoValueMarshaler, error) {
	return &taikoValueMarshaler{N: 7}, nil
}

// Taiko lookup RPCs such as taikoAuth_lastCertainL1OriginByBatchID return
// (nil, nil) on a miss. The response must encode that nil pointer as null, as
// json.Marshal does, instead of calling MarshalJSON on it and panicking.
func TestTaikoNilPointerResultEncodesNull(t *testing.T) {
	server := NewServer()
	defer server.Stop()
	if err := server.RegisterName("test", taikoLookupService{}); err != nil {
		t.Fatal(err)
	}
	client := DialInProc(server)
	defer client.Close()

	var missing json.RawMessage
	if err := client.Call(&missing, "test_missing"); err != nil {
		t.Fatal(err)
	}
	if string(missing) != "null" {
		t.Fatalf("missing result: got %s, want null", missing)
	}
	var found json.RawMessage
	if err := client.Call(&found, "test_found"); err != nil {
		t.Fatal(err)
	}
	if string(found) != "7" {
		t.Fatalf("found result: got %s, want 7", found)
	}
}
