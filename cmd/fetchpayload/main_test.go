// Copyright 2026 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestExecutionWitnessResponseToExtWitnessDecodesRLPHeaders(t *testing.T) {
	header := &types.Header{Number: big.NewInt(7)}
	encodedHeader, err := rlp.EncodeToBytes(header)
	if err != nil {
		t.Fatalf("encode header: %v", err)
	}

	response := executionWitnessResponse{
		Headers: []hexutil.Bytes{encodedHeader},
		Codes:   []hexutil.Bytes{[]byte("code")},
		State:   []hexutil.Bytes{[]byte("node")},
		Keys:    []hexutil.Bytes{[]byte("key")},
	}
	ext, err := response.toExtWitness()
	if err != nil {
		t.Fatalf("toExtWitness: %v", err)
	}
	if len(ext.Headers) != 1 {
		t.Fatalf("expected one header, got %d", len(ext.Headers))
	}
	if ext.Headers[0].Number.Uint64() != 7 {
		t.Fatalf("wrong header number: %d", ext.Headers[0].Number.Uint64())
	}
	if string(ext.Codes[0]) != "code" || string(ext.State[0]) != "node" || string(ext.Keys[0]) != "key" {
		t.Fatalf("unexpected witness payload: %+v", ext)
	}
}
