package taiko

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// stubHeaderReader satisfies consensus.ChainHeaderReader with empty lookups.
// verifyHeader only consults it for the EIP-4396 grandparent, which these
// tests sidestep by verifying block number 1.
type stubHeaderReader struct{ config *params.ChainConfig }

func (r *stubHeaderReader) Config() *params.ChainConfig                 { return r.config }
func (r *stubHeaderReader) CurrentHeader() *types.Header                { return nil }
func (r *stubHeaderReader) GetHeader(common.Hash, uint64) *types.Header { return nil }
func (r *stubHeaderReader) GetHeaderByNumber(uint64) *types.Header      { return nil }
func (r *stubHeaderReader) GetHeaderByHash(common.Hash) *types.Header   { return nil }
func (r *stubHeaderReader) GetTd(common.Hash, uint64) *big.Int          { return nil }

// TestVerifyHeaderShastaExtraDataShapes pins the Shasta extraData rule: only
// the exact 7-byte [pctg | proposalId(6)] layout imports; every other length
// is rejected.
func TestVerifyHeaderShastaExtraDataShapes(t *testing.T) {
	shastaTime := uint64(0)
	config := &params.ChainConfig{
		ChainID:    big.NewInt(167),
		ShastaTime: &shastaTime,
		Taiko:      true,
	}
	engine := New(config, rawdb.NewMemoryDatabase())
	parent := &types.Header{Number: big.NewInt(0), Time: 100}

	tests := []struct {
		name    string
		extra   []byte
		wantErr string
	}{
		{"proposal-id format (7 bytes)", []byte{75, 0, 0, 0, 0, 0x4c, 0x81}, ""},
		{"empty rejected", nil, "invalid Shasta extra-data length"},
		{"2 bytes rejected", []byte{75, 0}, "invalid Shasta extra-data length"},
		{"3 bytes rejected", []byte{75, 0, 1}, "invalid Shasta extra-data length"},
		{"12 bytes rejected (legal under the old >=7 floor)", make([]byte, 12), "invalid Shasta extra-data length"},
		{"over maximum", make([]byte, params.MaximumExtraDataSize+1), "extra-data too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withdrawalsHash := types.EmptyWithdrawalsHash
			header := &types.Header{
				Number:          big.NewInt(1),
				Time:            parent.Time + 1,
				GasLimit:        30_000_000,
				UncleHash:       types.CalcUncleHash(nil),
				BaseFee:         big.NewInt(params.ShastaInitialBaseFee),
				WithdrawalsHash: &withdrawalsHash,
				Extra:           tt.extra,
			}
			err := engine.verifyHeader(&stubHeaderReader{config: config}, header, parent, time.Now().Unix())
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected header to be accepted, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
