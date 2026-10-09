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

package beacon

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

type balHeaderReader struct {
	config *params.ChainConfig
	parent *types.Header
}

func (r *balHeaderReader) Config() *params.ChainConfig  { return r.config }
func (r *balHeaderReader) CurrentHeader() *types.Header { return r.parent }
func (r *balHeaderReader) GetHeader(hash common.Hash, number uint64) *types.Header {
	if hash == r.parent.Hash() && number == r.parent.Number.Uint64() {
		return r.parent
	}
	return nil
}
func (r *balHeaderReader) GetHeaderByNumber(number uint64) *types.Header {
	return r.GetHeader(r.parent.Hash(), number)
}
func (r *balHeaderReader) GetHeaderByHash(hash common.Hash) *types.Header {
	return r.GetHeader(hash, r.parent.Number.Uint64())
}

// CHANGE(taiko): Extending the shared header schema for L1 must not let Beacon
// import a BAL commitment before Amsterdam. Existing partial Amsterdam rules
// continue to accept nil BAL before a present slot.
func TestBeaconHeaderBALForkBoundary(t *testing.T) {
	for _, fork := range []string{"prague", "osaka", "amsterdam_slot_zero", "amsterdam_slot_one"} {
		t.Run(fork, func(t *testing.T) {
			config := *params.MergedTestChainConfig
			if fork == "prague" {
				config.OsakaTime = nil
			}
			amsterdam := strings.HasPrefix(fork, "amsterdam")
			if amsterdam {
				config.AmsterdamTime = new(uint64)
				schedule := *config.BlobScheduleConfig
				schedule.Amsterdam = schedule.Osaka
				config.BlobScheduleConfig = &schedule
			}
			parent := &types.Header{
				Number: big.NewInt(0), Time: 1, Difficulty: new(big.Int),
				UncleHash: types.EmptyUncleHash,
				GasLimit:  30_000_000, GasUsed: 15_000_000,
				BaseFee:         big.NewInt(params.InitialBaseFee),
				WithdrawalsHash: &types.EmptyWithdrawalsHash,
				BlobGasUsed:     new(uint64), ExcessBlobGas: new(uint64),
				ParentBeaconRoot: new(common.Hash), RequestsHash: &types.EmptyRequestsHash,
			}
			header := types.CopyHeader(parent)
			header.ParentHash = parent.Hash()
			header.Number = big.NewInt(1)
			header.Time = 2
			header.GasUsed = 0
			if amsterdam {
				header.SlotNumber = new(uint64)
				if fork == "amsterdam_slot_one" {
					*header.SlotNumber = 1
				}
			}
			chain := &balHeaderReader{config: &config, parent: parent}
			engine := New(nil)
			verify := func(candidate *types.Header) error {
				_, results := engine.VerifyHeaders(chain, []*types.Header{candidate})
				return <-results
			}
			// Exercise actual header wire decoding before the import path.
			roundTrip := func(candidate *types.Header) *types.Header {
				t.Helper()
				encoded, err := rlp.EncodeToBytes(candidate)
				if err != nil {
					t.Fatal(err)
				}
				var decoded types.Header
				if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Hash() != candidate.Hash() {
					t.Fatal("RLP round trip changed hash")
				}
				if (decoded.BlockAccessListHash != nil) != (candidate.BlockAccessListHash != nil) {
					t.Fatal("RLP round trip changed BAL presence")
				}
				if candidate.BlockAccessListHash != nil && *decoded.BlockAccessListHash != *candidate.BlockAccessListHash {
					t.Fatal("RLP round trip changed BAL value")
				}
				if (decoded.SlotNumber != nil) != (candidate.SlotNumber != nil) {
					t.Fatal("RLP round trip changed slot presence")
				}
				if candidate.SlotNumber != nil && *decoded.SlotNumber != *candidate.SlotNumber {
					t.Fatal("RLP round trip changed slot value")
				}
				return &decoded
			}
			if err := verify(roundTrip(header)); err != nil {
				t.Fatalf("otherwise valid nil-BAL baseline rejected: %v", err)
			}
			if amsterdam {
				return
			}
			for _, tc := range []struct {
				name string
				bal  common.Hash
			}{
				{"zero_bal", common.Hash{}},
				{"nonzero_bal", common.HexToHash("0x01")},
			} {
				t.Run(tc.name, func(t *testing.T) {
					candidate := types.CopyHeader(header)
					candidate.BlockAccessListHash = &tc.bal
					decoded := roundTrip(candidate)
					if decoded.SlotNumber != nil {
						t.Fatal("pre-Amsterdam candidate must have nil slot")
					}
					if err := verify(decoded); err == nil || !strings.Contains(err.Error(), "blockAccessListHash") {
						t.Fatalf("expected pre-Amsterdam BAL rejection, got %v", err)
					}
				})
			}
		})
	}
}
