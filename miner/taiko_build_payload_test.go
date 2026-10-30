package miner

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestBuildPayload_TaikoEngineEmptyBlockImports covers the build that every
// Taiko forkchoiceUpdated with payload attributes runs: BuildPayload first
// assembles an empty block on the Taiko consensus engine, through
// PostExecution, PrepareAssembly, Finalize and AssembleBlock. If that build
// fails, forkchoiceUpdated fails and the node stops producing blocks. The test
// pins the header fields of the empty block, pre-Unzen and on Unzen, and checks
// that the block imports.
func TestBuildPayload_TaikoEngineEmptyBlockImports(t *testing.T) {
	tests := []struct {
		name  string
		unzen bool
	}{
		{name: "pre-Unzen", unzen: false},
		{name: "Unzen", unzen: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := newUnzenTestChainConfig()
			if !tt.unzen {
				// Shanghai only: no Cancun, Prague, Osaka or Unzen header fields.
				config.CancunTime, config.PragueTime, config.OsakaTime, config.UnzenTime = nil, nil, nil, nil
			}
			w, b := newUnzenTestWorker(t, newUnzenTestGenesis(config))

			parent := b.chain.CurrentBlock()
			if got := config.IsUnzen(parent.Time); got != tt.unzen {
				t.Fatalf("IsUnzen(%d) = %v, want %v", parent.Time, got, tt.unzen)
			}
			// The arguments forkchoiceUpdated derives from the block it sealed.
			// Taiko allows a timestamp equal to the parent's before Shasta.
			txListHash := crypto.Keccak256Hash([]byte{0xc0})
			args := &BuildPayloadArgs{
				Parent:       parent.Hash(),
				Timestamp:    parent.Time,
				FeeRecipient: testUserAddress,
				Random:       common.HexToHash("0x2a"),
				Withdrawals:  types.Withdrawals{},
				Version:      engine.PayloadV2,
				TxListHash:   &txListHash,
				Extra:        []byte{0x32, 0, 0, 0, 0, 0, 1},
			}
			payload, err := w.BuildPayload(context.Background(), args, false)
			if err != nil {
				t.Fatalf("BuildPayload: %v", err)
			}
			// Resolve, as getPayload does. It also stops the background builder.
			env := payload.Resolve()
			if env == nil || env.ExecutionPayload == nil {
				t.Fatal("Resolve returned no execution payload")
			}
			block := payload.empty
			if block == nil {
				t.Fatal("BuildPayload produced no empty block")
			}
			if env.ExecutionPayload.BlockHash != block.Hash() {
				t.Fatalf("envelope block hash = %v, want the empty block's %v", env.ExecutionPayload.BlockHash, block.Hash())
			}

			if block.NumberU64() != parent.Number.Uint64()+1 {
				t.Errorf("number = %d, want %d", block.NumberU64(), parent.Number.Uint64()+1)
			}
			if block.ParentHash() != parent.Hash() {
				t.Errorf("parent hash = %v, want %v", block.ParentHash(), parent.Hash())
			}
			if block.Time() != args.Timestamp {
				t.Errorf("timestamp = %d, want %d", block.Time(), args.Timestamp)
			}
			if block.Coinbase() != args.FeeRecipient {
				t.Errorf("coinbase = %v, want %v", block.Coinbase(), args.FeeRecipient)
			}
			if block.MixDigest() != args.Random {
				t.Errorf("mix digest = %v, want %v", block.MixDigest(), args.Random)
			}
			if n := len(block.Transactions()); n != 0 {
				t.Errorf("empty block has %d transactions", n)
			}
			// Unzen stores the block's zk gas in the difficulty; an empty block uses none.
			if block.Difficulty() == nil || block.Difficulty().Sign() != 0 {
				t.Errorf("difficulty = %v, want 0", block.Difficulty())
			}
			if h := block.Header().WithdrawalsHash; h == nil || *h != types.EmptyWithdrawalsHash {
				t.Errorf("withdrawals hash = %v, want %v", h, types.EmptyWithdrawalsHash)
			}

			if tt.unzen {
				if root := block.BeaconRoot(); root == nil || *root != (common.Hash{}) {
					t.Errorf("parent beacon root = %v, want the zero hash", root)
				}
				if h := block.RequestsHash(); h == nil || *h != types.EmptyRequestsHash {
					t.Errorf("requests hash = %v, want %v", h, types.EmptyRequestsHash)
				}
				if used := block.BlobGasUsed(); used == nil || *used != 0 {
					t.Errorf("blob gas used = %v, want 0", used)
				}
				if excess := block.ExcessBlobGas(); excess == nil || *excess != 0 {
					t.Errorf("excess blob gas = %v, want 0", excess)
				}
			} else {
				if root := block.BeaconRoot(); root != nil {
					t.Errorf("parent beacon root = %v, want none before Cancun", *root)
				}
				if h := block.RequestsHash(); h != nil {
					t.Errorf("requests hash = %v, want none before Prague", *h)
				}
				if used := block.BlobGasUsed(); used != nil {
					t.Errorf("blob gas used = %d, want none before Cancun", *used)
				}
				if excess := block.ExcessBlobGas(); excess != nil {
					t.Errorf("excess blob gas = %d, want none before Cancun", *excess)
				}
			}

			// Import re-executes the block and verifies its header, as it would
			// for any block the engine API built.
			if n, err := b.chain.InsertChain(types.Blocks{block}); err != nil {
				t.Fatalf("importing the empty block failed (inserted %d): %v", n, err)
			}
			if head := b.chain.CurrentBlock(); head.Hash() != block.Hash() {
				t.Fatalf("head after import = %v, want the empty block %v", head.Hash(), block.Hash())
			}
		})
	}
}
