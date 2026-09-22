package taiko

import (
	"context"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// unzenChainConfig returns a Taiko chain config with Unzen — and the Ethereum
// forks every Taiko network schedules at the very same timestamp — active from
// genesis, unless unzenTime pushes Unzen into the future.
func unzenChainConfig(unzenTime uint64) *params.ChainConfig {
	zero := uint64(0)
	return &params.ChainConfig{
		ChainID:      big.NewInt(167),
		LondonBlock:  common.Big0,
		ShanghaiTime: &zero,
		CancunTime:   &zero,
		PragueTime:   &zero,
		OsakaTime:    &zero,
		UnzenTime:    &unzenTime,
		Taiko:        true,
	}
}

// unzenHeader returns a header carrying every canonical Unzen field, so a test
// only has to break the one field it is about.
func unzenHeader() *types.Header {
	zeroRoot := common.Hash{}
	zeroBlobGas := uint64(0)
	excessBlobGas := uint64(0)
	emptyRequests := types.EmptyRequestsHash
	return &types.Header{
		Number:           big.NewInt(1),
		Time:             100,
		GasLimit:         30_000_000,
		UncleHash:        types.CalcUncleHash(nil),
		Difficulty:       big.NewInt(21_000), // Unzen repurposes difficulty for zk gas.
		BaseFee:          big.NewInt(params.InitialBaseFee),
		WithdrawalsHash:  &types.EmptyWithdrawalsHash,
		ParentBeaconRoot: &zeroRoot,
		BlobGasUsed:      &zeroBlobGas,
		ExcessBlobGas:    &excessBlobGas,
		RequestsHash:     &emptyRequests,
	}
}

func newTestStateDB(t *testing.T) *state.StateDB {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("failed to create state db: %v", err)
	}
	return statedb
}

// TestVerifyUnzenHeaderFieldsParentBeaconRoot pins the canonical Unzen parent
// beacon root: only the zero hash is a valid value. The reference client
// rebuilds every Unzen payload header with the zero root, so it can never
// import a block carrying any other root; accepting one here is a client split
// vector — geth would run the EIP-4788 system call with a root the reference
// never executes, agree with itself on the resulting state root and make the
// block canonical while the reference rejects it.
func TestVerifyUnzenHeaderFieldsParentBeaconRoot(t *testing.T) {
	zero := common.Hash{}
	nonZero := common.HexToHash("0xdead")

	tests := []struct {
		name    string
		root    *common.Hash
		wantErr string
	}{
		{"zero root accepted", &zero, ""},
		{"missing root rejected", nil, "parent beacon root missing"},
		{"non-zero root rejected", &nonZero, "invalid parent beacon root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := unzenHeader()
			header.ParentBeaconRoot = tt.root
			err := verifyUnzenHeaderFields(header)
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

// TestVerifyHeaderUnzenRejectsNonZeroParentBeaconRoot walks the same rule
// through the import path that engine_newPayload feeds, where the split would
// actually happen.
func TestVerifyHeaderUnzenRejectsNonZeroParentBeaconRoot(t *testing.T) {
	config := unzenChainConfig(0)
	engine := New(config, rawdb.NewMemoryDatabase())
	parent := &types.Header{Number: big.NewInt(0), Time: 99}

	header := unzenHeader()
	header.ParentHash = parent.Hash()
	nonZero := common.HexToHash("0xdead")
	header.ParentBeaconRoot = &nonZero

	err := engine.verifyHeader(&stubHeaderReader{config: config}, header, parent, time.Now().Unix())
	if err == nil || !strings.Contains(err.Error(), "invalid parent beacon root") {
		t.Fatalf("expected error containing %q, got %v", "invalid parent beacon root", err)
	}
}

// TestFinalizeAndAssembleUnzenParentBeaconRoot pins that the build path refuses
// to assemble an Unzen block whose parent beacon root was never set. The root
// has to be on the header before the block executes, so the EIP-4788 system
// call runs and its writes are inside the state root committed here. Silently
// backfilling a nil root at finalization instead dresses a build that skipped
// that system call up as a canonical-looking header over a wrong state root.
func TestFinalizeAndAssembleUnzenParentBeaconRoot(t *testing.T) {
	t.Run("missing parent beacon root rejected", func(t *testing.T) {
		config := unzenChainConfig(0)
		engine := New(config, rawdb.NewMemoryDatabase())
		header := unzenHeader()
		header.ParentBeaconRoot = nil

		block, err := engine.FinalizeAndAssemble(
			context.Background(),
			&stubHeaderReader{config: config},
			header,
			newTestStateDB(t),
			&types.Body{},
			nil,
		)
		if err == nil || !strings.Contains(err.Error(), "parent beacon root missing") {
			t.Fatalf("expected error containing %q, got %v", "parent beacon root missing", err)
		}
		if block != nil {
			t.Fatalf("expected no block, got %v", block.Hash())
		}
		if header.ParentBeaconRoot != nil {
			t.Fatalf("expected the missing root to stay missing, got backfilled %v", *header.ParentBeaconRoot)
		}
	})

	t.Run("non-zero parent beacon root kept", func(t *testing.T) {
		// Assembly does not police the root's value: prepareWork rejects a
		// non-zero root before building and header verification rejects one on
		// import, while eth_simulateV1 has to keep a caller's beaconRoot block
		// override, as the reference client does.
		config := unzenChainConfig(0)
		engine := New(config, rawdb.NewMemoryDatabase())
		header := unzenHeader()
		nonZero := common.HexToHash("0xdead")
		header.ParentBeaconRoot = &nonZero

		block, err := engine.FinalizeAndAssemble(
			context.Background(),
			&stubHeaderReader{config: config},
			header,
			newTestStateDB(t),
			&types.Body{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected the block to be assembled, got %v", err)
		}
		if root := block.BeaconRoot(); root == nil || *root != nonZero {
			t.Fatalf("expected the parent beacon root %v to be kept, got %v", nonZero, root)
		}
	})

	t.Run("zero parent beacon root accepted", func(t *testing.T) {
		config := unzenChainConfig(0)
		engine := New(config, rawdb.NewMemoryDatabase())
		header := unzenHeader()

		block, err := engine.FinalizeAndAssemble(
			context.Background(),
			&stubHeaderReader{config: config},
			header,
			newTestStateDB(t),
			&types.Body{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected the block to be assembled, got %v", err)
		}
		if root := block.BeaconRoot(); root == nil || *root != (common.Hash{}) {
			t.Fatalf("expected the zero parent beacon root to survive, got %v", root)
		}
		// The remaining canonical Unzen stamps still belong to Finalize.
		if got := block.RequestsHash(); got == nil || *got != types.EmptyRequestsHash {
			t.Fatalf("expected the empty requests hash, got %v", got)
		}
		if got := block.BlobGasUsed(); got == nil || *got != 0 {
			t.Fatalf("expected zero blob gas used, got %v", got)
		}
		if got := block.ExcessBlobGas(); got == nil || *got != 0 {
			t.Fatalf("expected zero excess blob gas, got %v", got)
		}
	})

	t.Run("pre-Unzen missing parent beacon root accepted", func(t *testing.T) {
		config := unzenChainConfig(math.MaxUint64)
		engine := New(config, rawdb.NewMemoryDatabase())
		header := unzenHeader()
		header.ParentBeaconRoot = nil

		block, err := engine.FinalizeAndAssemble(
			context.Background(),
			&stubHeaderReader{config: config},
			header,
			newTestStateDB(t),
			&types.Body{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected the block to be assembled, got %v", err)
		}
		if block.BeaconRoot() != nil {
			t.Fatalf("expected no parent beacon root before Unzen, got %v", *block.BeaconRoot())
		}
	})
}
