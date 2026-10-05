package eth

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// TestEthAPIBackendPendingEtna pins that the pending block and header are
// null without an error when the head or the next block is Etna, which never
// has a local pending block, and that a pre-Etna chain keeps serving one.
func TestEthAPIBackendPendingEtna(t *testing.T) {
	zero, nextBlock := uint64(0), uint64(1001) // the genesis is at 1000, long past
	for _, tt := range []struct {
		name     string
		etnaTime *uint64
		wantNull bool
	}{
		{"etna head", &zero, true},
		{"etna next block", &nextBlock, true},
		{"before etna", nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := etnaTestChainConfig()
			config.EtnaTime = tt.etnaTime
			eth := newEtnaTestEthereum(t, &core.Genesis{
				Config:    config,
				Timestamp: 1000,
				GasLimit:  30_000_000,
				BaseFee:   big.NewInt(params.ShastaInitialBaseFee),
			}, true)
			backend := &EthAPIBackend{eth: eth}

			block, err := backend.BlockByNumber(context.Background(), rpc.PendingBlockNumber)
			if err != nil {
				t.Fatalf("BlockByNumber(pending): %v", err)
			}
			header, err := backend.HeaderByNumber(context.Background(), rpc.PendingBlockNumber)
			if err != nil {
				t.Fatalf("HeaderByNumber(pending): %v", err)
			}
			if gotNull := block == nil && header == nil; gotNull != tt.wantNull {
				t.Fatalf("pending block %v, header %v; want null: %v", block, header, tt.wantNull)
			}
		})
	}
}
