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

package core

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

// CHANGE(taiko): The shared L1 header schema must still decode Amsterdam headers
// generated without a BAL commitment by the existing execution code.
func TestAmsterdamGenesisRawDBReadback(t *testing.T) {
	config := *params.MergedTestChainConfig
	schedule := *config.BlobScheduleConfig
	schedule.Amsterdam = schedule.Osaka
	config.BlobScheduleConfig = &schedule
	config.AmsterdamTime = new(uint64)
	genesis := &Genesis{Config: &config}
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	triedb := triedb.NewDatabase(db, triedb.HashDefaults)
	defer triedb.Close()
	block, err := genesis.Commit(db, triedb, nil)
	if err != nil {
		t.Fatal(err)
	}
	header := block.Header()
	if header.BlockAccessListHash != nil || header.SlotNumber == nil || *header.SlotNumber != 0 {
		t.Fatalf("unexpected Amsterdam genesis fields: BAL %v, slot %v", header.BlockAccessListHash, header.SlotNumber)
	}
	stored := rawdb.ReadBlock(db, block.Hash(), 0)
	if stored == nil {
		t.Fatal("Amsterdam genesis with nil BAL and slot zero could not be read back")
	}
	if stored.Hash() != block.Hash() {
		t.Fatalf("stored hash: got %s, want %s", stored.Hash(), block.Hash())
	}
	if got := stored.Header(); got.BlockAccessListHash != nil || got.SlotNumber == nil || *got.SlotNumber != 0 {
		t.Fatalf("stored fields changed: BAL %v, slot %v", got.BlockAccessListHash, got.SlotNumber)
	}
}
