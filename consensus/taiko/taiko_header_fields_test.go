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

package taiko

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// CHANGE(taiko): L1-only Amsterdam fields must not expand the accepted L2 schema.
func TestVerifyHeaderRejectsL1AmsterdamFields(t *testing.T) {
	for _, era := range []string{"pre_unzen", "unzen", "shasta"} {
		t.Run(era, func(t *testing.T) {
			config := unzenChainConfig(0)
			header := unzenHeader()
			switch era {
			case "pre_unzen":
				config.UnzenTime = new(uint64)
				*config.UnzenTime = math.MaxUint64
				header.Difficulty = new(big.Int)
				header.ParentBeaconRoot = nil
				header.BlobGasUsed = nil
				header.ExcessBlobGas = nil
				header.RequestsHash = nil
			case "shasta":
				config.ShastaTime = new(uint64)
				header.Extra = make([]byte, params.ShastaExtraDataLen)
			}
			parent := &types.Header{Number: big.NewInt(0), Time: header.Time - 1}
			header.ParentHash = parent.Hash()
			db := rawdb.NewMemoryDatabase()
			defer db.Close()
			engine := New(config, db)
			chain := &stubHeaderReader{config: config}
			if err := engine.verifyHeader(chain, header, parent, int64(header.Time)); err != nil {
				t.Fatalf("otherwise valid baseline rejected: %v", err)
			}
			for _, tc := range []struct {
				name    string
				set     func(*types.Header)
				wantErr string
			}{
				{"bal_zero", func(h *types.Header) { h.BlockAccessListHash = new(common.Hash) }, "block access list hash"},
				{"bal_nonzero", func(h *types.Header) { hash := common.HexToHash("0x01"); h.BlockAccessListHash = &hash }, "block access list hash"},
				{"slot_zero", func(h *types.Header) { h.SlotNumber = new(uint64) }, "slot number"},
				{"slot_nonzero", func(h *types.Header) { slot := uint64(1); h.SlotNumber = &slot }, "slot number"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					candidate := types.CopyHeader(header)
					tc.set(candidate)
					if err := engine.verifyHeader(chain, candidate, parent, int64(header.Time)); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("expected forbidden %s to be rejected, got %v", tc.wantErr, err)
					}
				})
			}
		})
	}
}
