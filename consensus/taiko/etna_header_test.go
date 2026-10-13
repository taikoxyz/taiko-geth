package taiko

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

// etnaTestRoot stands in for the state root of the L1 block at the final
// anchorBlockNumber, which an Etna header carries as its parent beacon root.
var etnaTestRoot = common.HexToHash("0xdead")

// etnaChainConfig returns the Unzen test chain config with Shasta active from
// genesis and Etna activating at etnaTime.
func etnaChainConfig(etnaTime uint64) *params.ChainConfig {
	config := unzenChainConfig(0)
	shastaTime := uint64(0)
	config.ShastaTime = &shastaTime
	config.EtnaTime = &etnaTime
	return config
}

// etnaHeader returns block 1 at timestamp 100 carrying every canonical Etna
// field, so a test only has to break the one field it is about.
func etnaHeader() *types.Header {
	header := unzenHeader()
	root := etnaTestRoot
	header.ParentBeaconRoot = &root
	header.Extra = make([]byte, params.EtnaExtraDataLen)
	header.BaseFee = big.NewInt(params.ShastaInitialBaseFee)
	return header
}

// preEtnaHeader returns block 1 at timestamp 100 carrying every canonical
// Shasta/Unzen field. Its base fee is not the Shasta initial base fee, which
// block 1 needs only from Etna on.
func preEtnaHeader() *types.Header {
	header := unzenHeader()
	header.Extra = make([]byte, params.ShastaExtraDataLen)
	return header
}

// TestVerifyUnzenHeaderFieldsEtnaParentBeaconRoot pins the Etna root rule: the
// parent beacon root carries the state root of the L1 block at the final
// anchorBlockNumber, so it must be present and non-zero, while blocks before
// Etna keep the zero root.
func TestVerifyUnzenHeaderFieldsEtnaParentBeaconRoot(t *testing.T) {
	zero := common.Hash{}
	nonZero := etnaTestRoot
	tests := []struct {
		name    string
		isEtna  bool
		root    *common.Hash
		wantErr string
	}{
		{"etna non-zero root accepted", true, &nonZero, ""},
		{"etna zero root rejected", true, &zero, "requires a non-zero root"},
		{"etna missing root rejected", true, nil, "parent beacon root missing"},
		{"pre-etna zero root accepted", false, &zero, ""},
		{"pre-etna non-zero root rejected", false, &nonZero, "invalid parent beacon root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := unzenHeader()
			header.ParentBeaconRoot = tt.root
			err := verifyUnzenHeaderFields(header, tt.isEtna)
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

// TestVerifyHeaderEtnaRules walks every header rule that changes at Etna
// through the import path: before Etna, from Etna on, and at the boundary,
// where block 1 is the first Etna block and its parent predates Etna. Each row
// starts from a header that is canonical for its fork and breaks one field.
func TestVerifyHeaderEtnaRules(t *testing.T) {
	zero := common.Hash{}
	nonZero := etnaTestRoot
	// A withdrawals root that commits to a single zero-amount withdrawal.
	oneWithdrawal := types.DeriveSha(types.Withdrawals{{}}, trie.NewStackTrie(nil))

	tests := []struct {
		name    string
		mutate  func(*types.Header)
		preEtna string // expected error substring before Etna; "" means accepted
		etna    string // expected error substring from Etna on and at the boundary
	}{
		{"canonical header", func(*types.Header) {}, "", ""},

		{"zero root", func(h *types.Header) { h.ParentBeaconRoot = &zero }, "", "requires a non-zero root"},
		{"non-zero root", func(h *types.Header) { h.ParentBeaconRoot = &nonZero }, "invalid parent beacon root", ""},
		{"missing root", func(h *types.Header) { h.ParentBeaconRoot = nil }, "parent beacon root missing", "parent beacon root missing"},

		{"7-byte extra", func(h *types.Header) { h.Extra = make([]byte, 7) }, "", "invalid Etna extra-data length"},
		{"13-byte extra", func(h *types.Header) { h.Extra = make([]byte, 13) }, "invalid Shasta extra-data length", ""},
		{"empty extra", func(h *types.Header) { h.Extra = nil }, "invalid Shasta extra-data length", "invalid Etna extra-data length"},
		{"12-byte extra", func(h *types.Header) { h.Extra = make([]byte, 12) }, "invalid Shasta extra-data length", "invalid Etna extra-data length"},
		{"14-byte extra", func(h *types.Header) { h.Extra = make([]byte, 14) }, "invalid Shasta extra-data length", "invalid Etna extra-data length"},

		{"withdrawals root of one withdrawal", func(h *types.Header) { h.WithdrawalsHash = &oneWithdrawal }, "", "invalid Etna withdrawals hash"},
		{"missing withdrawals root", func(h *types.Header) { h.WithdrawalsHash = nil }, "withdrawals hash missing", "withdrawals hash missing"},

		{"block 1 base fee above the Shasta initial base fee", func(h *types.Header) { h.BaseFee = big.NewInt(params.ShastaInitialBaseFee + 1) }, "", "invalid baseFee"},
		{"block 1 base fee below the Shasta initial base fee", func(h *types.Header) { h.BaseFee = big.NewInt(params.ShastaInitialBaseFee - 1) }, "", "invalid baseFee"},
		{"block 1 base fee equal to the Shasta initial base fee", func(h *types.Header) { h.BaseFee = big.NewInt(params.ShastaInitialBaseFee) }, "", ""},
	}
	forks := []struct {
		name     string
		etnaTime uint64 // the parent is at timestamp 99 and the header at 100
		isEtna   bool
	}{
		{"pre-etna", 101, false},
		{"etna", 0, true},
		{"boundary", 100, true},
	}
	for _, fork := range forks {
		for _, tt := range tests {
			t.Run(fork.name+"/"+tt.name, func(t *testing.T) {
				config := etnaChainConfig(fork.etnaTime)
				engine := New(config, rawdb.NewMemoryDatabase())
				parent := &types.Header{Number: big.NewInt(0), Time: 99}

				header, wantErr := preEtnaHeader(), tt.preEtna
				if fork.isEtna {
					header, wantErr = etnaHeader(), tt.etna
				}
				header.ParentHash = parent.Hash()
				tt.mutate(header)

				err := engine.verifyHeader(&stubHeaderReader{config: config}, header, parent, time.Now().Unix())
				if wantErr == "" {
					if err != nil {
						t.Fatalf("expected header to be accepted, got %v", err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("expected error containing %q, got %v", wantErr, err)
				}
			})
		}
	}
}

// TestFinalizeEtnaCreditsNoWithdrawals pins that Etna blocks credit no
// withdrawals, while earlier blocks still do.
func TestFinalizeEtnaCreditsNoWithdrawals(t *testing.T) {
	recipient := common.HexToAddress("0xc0ffee")
	tests := []struct {
		name     string
		etnaTime uint64
		want     uint64
	}{
		{"etna", 0, 0},
		{"boundary", 100, 0},
		{"before etna", 101, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := etnaChainConfig(tt.etnaTime)
			statedb := newTestStateDB(t)
			body := &types.Body{Withdrawals: types.Withdrawals{{Address: recipient, Amount: 7}}}
			New(config, rawdb.NewMemoryDatabase()).Finalize(&stubHeaderReader{config: config}, etnaHeader(), statedb, body)
			if got := statedb.GetBalance(recipient).Uint64(); got != tt.want {
				t.Fatalf("recipient balance = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestFinalizeAndAssembleEtna pins the Etna build-side rules: the first
// transaction need not be an anchor, and the parent beacon root must be a
// non-zero L1 state root.
func TestFinalizeAndAssembleEtna(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	to := common.HexToAddress("0xbeef")
	assemble := func(t *testing.T, config *params.ChainConfig, root *common.Hash) (*types.Block, error) {
		tx := types.MustSignNewTx(key, types.LatestSigner(config), &types.LegacyTx{
			Nonce: 0, To: &to, Gas: params.TxGas, GasPrice: big.NewInt(params.InitialBaseFee),
		})
		header := etnaHeader()
		header.ParentBeaconRoot = root
		return New(config, rawdb.NewMemoryDatabase()).FinalizeAndAssemble(
			context.Background(),
			&stubHeaderReader{config: config},
			header,
			newTestStateDB(t),
			&types.Body{Transactions: types.Transactions{tx}},
			nil,
		)
	}
	root := etnaTestRoot

	t.Run("ordinary first transaction accepted", func(t *testing.T) {
		block, err := assemble(t, etnaChainConfig(0), &root)
		if err != nil {
			t.Fatalf("expected the block to be assembled, got %v", err)
		}
		if got := block.BeaconRoot(); got == nil || *got != root {
			t.Fatalf("expected parent beacon root %v, got %v", root, got)
		}
		if got := block.Header().WithdrawalsHash; got == nil || *got != types.EmptyWithdrawalsHash {
			t.Fatalf("expected the empty withdrawals root, got %v", got)
		}
	})
	t.Run("same block before etna needs an anchor", func(t *testing.T) {
		zero := common.Hash{}
		if _, err := assemble(t, etnaChainConfig(math.MaxUint64), &zero); !errors.Is(err, ErrAnchorTxNotFound) {
			t.Fatalf("expected %v, got %v", ErrAnchorTxNotFound, err)
		}
	})
	t.Run("zero root rejected", func(t *testing.T) {
		zero := common.Hash{}
		if _, err := assemble(t, etnaChainConfig(0), &zero); err == nil || !strings.Contains(err.Error(), "requires a non-zero root") {
			t.Fatalf("expected a zero-root error, got %v", err)
		}
	})
	t.Run("missing root rejected", func(t *testing.T) {
		if _, err := assemble(t, etnaChainConfig(0), nil); err == nil || !strings.Contains(err.Error(), "parent beacon root missing") {
			t.Fatalf("expected a missing-root error, got %v", err)
		}
	})
}
