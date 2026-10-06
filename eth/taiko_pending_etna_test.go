package eth

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// TestEthAPIBackendPendingEtna pins that the pending block and header are
// null without an error when the head or the next block is Etna, which never
// has a local pending block, and that a pre-Etna chain keeps serving one.
func TestEthAPIBackendPendingEtna(t *testing.T) {
	now := uint64(time.Now().Unix())
	zero, nextBlock := uint64(0), uint64(1001) // the genesis is at 1000, long past
	futureEtna := now + 1800                   // after the wall clock, before the genesis below
	for _, tt := range []struct {
		name        string
		genesisTime uint64
		etnaTime    *uint64
		wantNull    bool
	}{
		{"etna head", 1000, &zero, true},
		{"etna next block", 1000, &nextBlock, true},
		// The head is Etna while the wall clock is not: only the head makes
		// the pending block unavailable.
		{"etna head before the wall clock", now + 3600, &futureEtna, true},
		{"before etna", 1000, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := etnaTestChainConfig()
			config.EtnaTime = tt.etnaTime
			eth := newEtnaTestEthereum(t, &core.Genesis{
				Config:    config,
				Timestamp: tt.genesisTime,
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

// TestPendingWitnessAndReceiptsEtna pins the pending-tagged calls that read
// the pending block when Etna leaves it unavailable:
// eth_getBlockReceipts("pending") answers null without an error, like the
// pending block, and debug_executionWitness answers -32001 for a block it
// cannot find, the pending block and a future number alike, instead of
// crashing. A pre-Etna chain keeps serving the pending receipts.
func TestPendingWitnessAndReceiptsEtna(t *testing.T) {
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
			eth.APIBackend = &EthAPIBackend{eth: eth}
			server := rpc.NewServer()
			t.Cleanup(server.Stop)
			if err := server.RegisterName("eth", ethapi.NewBlockChainAPI(eth.APIBackend)); err != nil {
				t.Fatalf("register eth API: %v", err)
			}
			if err := server.RegisterName("debug", NewDebugAPI(eth)); err != nil {
				t.Fatalf("register debug API: %v", err)
			}
			client := rpc.DialInProc(server)
			t.Cleanup(client.Close)

			var receipts json.RawMessage
			if err := client.Call(&receipts, "eth_getBlockReceipts", "pending"); err != nil {
				t.Fatalf("eth_getBlockReceipts(pending): %v", err)
			}
			if gotNull := string(receipts) == "null"; gotNull != tt.wantNull {
				t.Fatalf("eth_getBlockReceipts(pending) = %s, want null: %v", receipts, tt.wantNull)
			}
			if !tt.wantNull {
				return
			}
			for _, block := range []string{"pending", "0x10"} {
				err := client.Call(new(json.RawMessage), "debug_executionWitness", block)
				var rpcErr rpc.Error
				if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != -32001 {
					t.Fatalf("debug_executionWitness(%s): err = %v, want a -32001 JSON-RPC error", block, err)
				}
			}
		})
	}
}
