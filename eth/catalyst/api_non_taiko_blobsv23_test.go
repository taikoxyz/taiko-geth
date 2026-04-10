//go:build non_taiko_tests
// +build non_taiko_tests

// Copyright 2021 The go-ethereum Authors
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

package catalyst

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/internal/testrand"
)

func TestGetBlobsV2And3(t *testing.T) {
	n, api := newGetBlobEnv(t, 1)
	defer n.Close()

	suites := []struct {
		start      int
		limit      int
		fillRandom bool
	}{
		{start: 0, limit: 1},
		{start: 0, limit: 2},
		{start: 1, limit: 3},
		{start: 0, limit: 6},
		{start: 1, limit: 5},
		{start: 0, limit: 6, fillRandom: true},
	}
	for i, suite := range suites {
		runGetBlobs(t, api.GetBlobsV2, suite.start, suite.limit, suite.fillRandom, false, fmt.Sprintf("GetBlobsV2 suite=%d", i))
		runGetBlobs(t, api.GetBlobsV3, suite.start, suite.limit, suite.fillRandom, true, fmt.Sprintf("GetBlobsV3 suite=%d %v", i, suite))
	}
}

// Benchmark GetBlobsV2 internals.
// Note that this is not an RPC-level benchmark, so JSON-RPC overhead is not included.
func BenchmarkGetBlobsV2(b *testing.B) {
	n, api := newGetBlobEnv(b, 1)
	defer n.Close()

	for _, blobs := range []int{1, 2, 4, 6} {
		name := fmt.Sprintf("blobs=%d", blobs)
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				runGetBlobs(b, api.GetBlobsV2, 0, blobs, false, false, name)
			}
		})
	}
}

type getBlobsFn func(hashes []common.Hash) ([]*engine.BlobAndProofV2, error)

func runGetBlobs(t testing.TB, getBlobs getBlobsFn, start, limit int, fillRandom bool, expectPartialResponse bool, name string) {
	var (
		vhashes []common.Hash
		expect  []*engine.BlobAndProofV2
	)
	for j := start; j < limit; j++ {
		vhashes = append(vhashes, testBlobVHashes[j])
		var cellProofs []hexutil.Bytes
		for _, proof := range testBlobCellProofs[j] {
			cellProofs = append(cellProofs, proof[:])
		}
		expect = append(expect, &engine.BlobAndProofV2{
			Blob:       testBlobs[j][:],
			CellProofs: cellProofs,
		})
	}
	if fillRandom {
		vhashes = append(vhashes, testrand.Hash())
	}
	result, err := getBlobs(vhashes)
	if err != nil {
		t.Errorf("unexpected error for case %s: %v", name, err)
	}
	if fillRandom {
		if expectPartialResponse {
			expect = append(expect, nil)
		} else {
			expect = nil
		}
	}
	if !reflect.DeepEqual(result, expect) {
		t.Fatalf("unexpected result for case %s", name)
	}
}
