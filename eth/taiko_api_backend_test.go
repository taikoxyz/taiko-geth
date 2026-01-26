package eth

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
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

func TestGetLastBlockByBatchIdStartsFromHeadL1Origin(t *testing.T) {
	key, err := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	if err != nil {
		t.Fatalf("failed to load key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)

	genesis := &core.Genesis{
		Config: params.AllEthashProtocolChanges,
		Alloc: types.GenesisAlloc{
			addr: {Balance: new(big.Int).SetUint64(1_000_000_000_000_000_000)},
		},
	}
	engine := ethash.NewFaker()

	anchorData := append([]byte{}, taiko.AnchorV4Selector...)
	anchorData = append(anchorData, 0x01)

	db, blocks, _ := core.GenerateChainWithGenesis(genesis, engine, 4, func(i int, b *core.BlockGen) {
		b.SetExtra(shastaExtraData(uint64(i + 1)))
		tx := types.MustSignNewTx(key, b.Signer(), &types.LegacyTx{
			Nonce:    b.TxNonce(addr),
			To:       &common.Address{},
			Gas:      100000,
			GasPrice: new(big.Int).SetUint64(params.InitialBaseFee * 2),
			Data:     anchorData,
		})
		b.AddTx(tx)
	})

	chain, err := core.NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert chain: %v", err)
	}
	defer chain.Stop()

	headBlockID := big.NewInt(2)
	rawdb.WriteL1Origin(db, headBlockID, &rawdb.L1Origin{
		BlockID:     headBlockID,
		L2BlockHash: blocks[1].Hash(),
	})
	rawdb.WriteHeadL1Origin(db, headBlockID)

	backend := NewTaikoAPIBackend(&Ethereum{
		blockchain: chain,
		chainDb:    db,
	})

	blockID, err := backend.getLastBlockByBatchId(big.NewInt(4))
	if err != ethereum.NotFound {
		t.Fatalf("expected not found error, got %v", err)
	}
	if blockID != nil {
		t.Fatalf("expected nil block ID, got %v", blockID)
	}
}

func shastaExtraData(proposalID uint64) []byte {
	extra := make([]byte, params.ShastaExtraDataLen)
	encoded := new(big.Int).SetUint64(proposalID).Bytes()
	buf := make([]byte, params.ShastaExtraDataProposalIDLength)
	copy(buf[len(buf)-len(encoded):], encoded)
	copy(extra[params.ShastaExtraDataProposalIDIndex:], buf)
	return extra
}
