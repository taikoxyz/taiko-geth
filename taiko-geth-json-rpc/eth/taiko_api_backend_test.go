package eth

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

func TestShastaProposalIDFromExtraData(t *testing.T) {
	extra := []byte{0x2a, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	proposalID, err := core.DecodeShastaProposalID(extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := new(big.Int).SetBytes([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	if proposalID.Cmp(expected) != 0 {
		t.Fatalf("expected %s, got %s", expected.String(), proposalID.String())
	}
}

func TestShastaBasefeeSharingPctgFromExtraData(t *testing.T) {
	extra := []byte{0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if pctg := core.DecodeShastaBasefeeSharingPctg(extra); pctg != 0x64 {
		t.Fatalf("expected 0x64, got %d", pctg)
	}
	if pctg := core.DecodeShastaBasefeeSharingPctg(nil); pctg != 0 {
		t.Fatalf("expected 0, got %d", pctg)
	}
}

func TestShastaProposalIDFromExtraDataInvalid(t *testing.T) {
	if _, err := core.DecodeShastaProposalID([]byte{0x01}); err == nil {
		t.Fatal("expected error for short extradata")
	}
}

func TestGetLastBlockByBatchIdUncertainAtHead(t *testing.T) {
	proposalBytes := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	proposalID := new(big.Int).SetBytes(proposalBytes)
	extra := append([]byte{0x00}, proposalBytes...)
	data := make([]byte, len(taiko.AnchorV4Selector))
	copy(data, taiko.AnchorV4Selector)

	genesis := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc: types.GenesisAlloc{
			testAddr: {Balance: big.NewInt(1_000_000_000_000_000_000)},
		},
	}
	engine := ethash.NewFaker()

	_, blocks, _ := core.GenerateChainWithGenesis(genesis, engine, 1, func(i int, b *core.BlockGen) {
		b.SetExtra(extra)
		tx := types.NewTx(&types.LegacyTx{
			Nonce:    0,
			To:       &common.Address{1},
			Value:    big.NewInt(0),
			Gas:      50_000,
			GasPrice: b.BaseFee(),
			Data:     data,
		})
		signed, err := types.SignTx(tx, types.HomesteadSigner{}, testKey)
		if err != nil {
			t.Fatalf("failed to sign tx: %v", err)
		}
		b.AddTx(signed)
	})

	chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), nil, genesis, nil, engine, vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert chain: %v", err)
	}

	backend := &TaikoAPIBackend{eth: &Ethereum{blockchain: chain}}
	blockID, err := backend.getLastBlockByBatchId(proposalID)
	if !errors.Is(err, ErrProposalLastBlockUncertain) {
		t.Fatalf("expected ErrProposalLastBlockUncertain, got %v", err)
	}
	if blockID != nil {
		t.Fatalf("expected nil blockID, got %v", blockID)
	}
}
