package eth

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

func TestShastaProposalIDFromExtraData(t *testing.T) {
	extra := []byte{0x2a, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x01}
	proposalID, endOfProposal, err := core.DecodeShastaProposalID(extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := new(big.Int).SetBytes([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	if proposalID.Cmp(expected) != 0 {
		t.Fatalf("expected %s, got %s", expected.String(), proposalID.String())
	}
	if !endOfProposal {
		t.Fatal("expected endOfProposal to be true")
	}
}

func TestShastaBasefeeSharingPctgFromExtraData(t *testing.T) {
	extra := []byte{0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if pctg := core.DecodeShastaBasefeeSharingPctg(extra); pctg != 0x64 {
		t.Fatalf("expected 0x64, got %d", pctg)
	}
	if pctg := core.DecodeShastaBasefeeSharingPctg(nil); pctg != 0 {
		t.Fatalf("expected 0, got %d", pctg)
	}
}

func TestShastaProposalIDFromExtraDataInvalid(t *testing.T) {
	tests := []struct {
		name  string
		extra []byte
	}{
		{
			name:  "short",
			extra: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
		{
			name:  "long",
			extra: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := core.DecodeShastaProposalID(test.extra); err == nil {
				t.Fatal("expected error for invalid extradata length")
			}
		})
	}
}

func TestLastBlockIDByBatchIDRequiresEndOfProposal(t *testing.T) {
	extra := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00}
	chain := newShastaTestChain(t, extra)
	defer chain.Stop()
	backend := &TaikoAPIBackend{eth: &Ethereum{blockchain: chain}}
	if _, err := backend.LastBlockIDByBatchID((*math.HexOrDecimal256)(big.NewInt(1))); err == nil {
		t.Fatal("expected error when endOfProposal is false")
	}
}

func TestLastBlockIDByBatchIDTooLarge(t *testing.T) {
	extra := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x01}
	chain := newShastaTestChain(t, extra)
	defer chain.Stop()

	backend := &TaikoAPIBackend{eth: &Ethereum{blockchain: chain}}
	if _, err := backend.LastBlockIDByBatchID((*math.HexOrDecimal256)(big.NewInt(2))); err == nil {
		t.Fatal("expected error when batchID is greater than head proposalID")
	} else if errors.Is(err, ethereum.NotFound) {
		t.Fatal("expected direct error, got NotFound")
	}
}

func newShastaTestChain(t *testing.T, extra []byte) *core.BlockChain {
	t.Helper()

	genesis := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc: types.GenesisAlloc{
			testAddr: {Balance: big.NewInt(1_000_000_000_000_000_000)},
		},
		BaseFee: big.NewInt(params.InitialBaseFee),
	}

	db, blocks, _ := core.GenerateChainWithGenesis(genesis, ethash.NewFaker(), 1, func(i int, gen *core.BlockGen) {
		gen.SetExtra(extra)
		data := append(append([]byte{}, taiko.AnchorV4Selector...), 0x00)
		tx := types.NewTransaction(
			gen.TxNonce(testAddr),
			common.Address{},
			big.NewInt(0),
			100000,
			big.NewInt(params.InitialBaseFee*2),
			data,
		)
		signedTx, err := types.SignTx(tx, gen.Signer(), testKey)
		if err != nil {
			panic(err)
		}
		gen.AddTx(signedTx)
	})

	chain, err := core.NewBlockChain(db, nil, genesis, nil, ethash.NewFaker(), vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert chain: %v", err)
	}
	return chain
}
