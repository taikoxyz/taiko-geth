// Copyright 2025 The go-ethereum Authors
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

package engine

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
)

func TestBlobs(t *testing.T) {
	var (
		emptyBlob          = new(kzg4844.Blob)
		emptyBlobCommit, _ = kzg4844.BlobToCommitment(emptyBlob)
		emptyBlobProof, _  = kzg4844.ComputeBlobProof(emptyBlob, emptyBlobCommit)
		emptyCellProof, _  = kzg4844.ComputeCellProofs(emptyBlob)
	)
	header := types.Header{}
	block := types.NewBlock(&header, &types.Body{}, nil, nil)

	sidecarWithoutCellProofs := types.NewBlobTxSidecar(types.BlobSidecarVersion0, []kzg4844.Blob{*emptyBlob}, []kzg4844.Commitment{emptyBlobCommit}, []kzg4844.Proof{emptyBlobProof})
	env := BlockToExecutableData(block, common.Big0, []*types.BlobTxSidecar{sidecarWithoutCellProofs}, nil)
	if len(env.BlobsBundle.Proofs) != 1 {
		t.Fatalf("Expect 1 proof in blobs bundle, got %v", len(env.BlobsBundle.Proofs))
	}

	sidecarWithCellProofs := types.NewBlobTxSidecar(types.BlobSidecarVersion0, []kzg4844.Blob{*emptyBlob}, []kzg4844.Commitment{emptyBlobCommit}, emptyCellProof)
	env = BlockToExecutableData(block, common.Big0, []*types.BlobTxSidecar{sidecarWithCellProofs}, nil)
	if len(env.BlobsBundle.Proofs) != 128 {
		t.Fatalf("Expect 128 proofs in blobs bundle, got %v", len(env.BlobsBundle.Proofs))
	}
}

func TestExecutableDataToBlockNormalizesUzenBeaconRootAndRequestsHash(t *testing.T) {
	timestamp := uint64(1_780_000_001)
	payload := ExecutableData{
		Number:        1,
		Timestamp:     timestamp,
		GasLimit:      30_000_000,
		GasUsed:       0,
		BaseFeePerGas: big.NewInt(params.InitialBaseFee),
		Transactions:  nil,
		ExtraData:     nil,
		LogsBloom:     make([]byte, 256),
		TaikoBlock:    true,
	}

	block, err := ExecutableDataToBlockNoHash(payload, nil, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if block.BeaconRoot() == nil || *block.BeaconRoot() != (common.Hash{}) {
		t.Fatalf("expected zero beacon root under Uzen, got %v", block.BeaconRoot())
	}
	if block.RequestsHash() == nil || *block.RequestsHash() != types.EmptyRequestsHash {
		t.Fatalf("expected empty requests hash under Uzen, got %v", block.RequestsHash())
	}
}

func TestExecutableDataToBlockPreservesNonUzenBeaconRootAndRequestsHash(t *testing.T) {
	beaconRoot := common.HexToHash("0x1234")
	requests := [][]byte{[]byte{0x01}, []byte{0x02, 0x03}}
	payload := ExecutableData{
		Number:        1,
		Timestamp:     1_779_999_999,
		GasLimit:      30_000_000,
		GasUsed:       0,
		BaseFeePerGas: big.NewInt(params.InitialBaseFee),
		Transactions:  nil,
		ExtraData:     nil,
		LogsBloom:     make([]byte, 256),
		TaikoBlock:    true,
	}

	block, err := ExecutableDataToBlockNoHash(payload, nil, &beaconRoot, requests, false)
	if err != nil {
		t.Fatal(err)
	}
	if block.BeaconRoot() == nil || *block.BeaconRoot() != beaconRoot {
		t.Fatalf("expected preserved beacon root outside Uzen, got %v", block.BeaconRoot())
	}
	wantRequestsHash := types.CalcRequestsHash(requests)
	if block.RequestsHash() == nil || *block.RequestsHash() != wantRequestsHash {
		t.Fatalf("expected requests hash %v outside Uzen, got %v", wantRequestsHash, block.RequestsHash())
	}
}

func TestExecutableDataJSONDoesNotExposeUzenBlock(t *testing.T) {
	payload := ExecutableData{
		Number:        1,
		Timestamp:     1,
		GasLimit:      30_000_000,
		BaseFeePerGas: big.NewInt(params.InitialBaseFee),
		Transactions:  [][]byte{},
		LogsBloom:     make([]byte, 256),
		TaikoBlock:    true,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "UzenBlock") {
		t.Fatalf("unexpected UzenBlock field in payload JSON: %s", encoded)
	}

	var decoded ExecutableData
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.TaikoBlock {
		t.Fatal("expected TaikoBlock to survive payload JSON round-trip")
	}
}
