package eth

import (
	"errors"
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
	"github.com/ethereum/go-ethereum/ethdb"
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

func TestGetLastBlockByBatchIdNoHeadL1Origin(t *testing.T) {
	db, chain, proposalID, _ := newShastaTestChain(t)
	backend := &TaikoAPIBackend{eth: &Ethereum{blockchain: chain, chainDb: db}}

	blockID, err := backend.getLastBlockByBatchId(proposalID)
	if !errors.Is(err, ethereum.NotFound) {
		t.Fatalf("expected NotFound, got %v", err)
	}
	if blockID != nil {
		t.Fatalf("expected nil blockID, got %v", blockID)
	}
}

func TestGetLastBlockByBatchIdUncertainAtHead(t *testing.T) {
	db, chain, proposalID, blocks := newShastaTestChain(t)
	backend := &TaikoAPIBackend{eth: &Ethereum{blockchain: chain, chainDb: db}}
	headBlock := blocks[len(blocks)-1]
	rawdb.WriteL1Origin(db, headBlock.Number(), &rawdb.L1Origin{
		BlockID:     headBlock.Number(),
		L2BlockHash: headBlock.Hash(),
	})
	rawdb.WriteHeadL1Origin(db, headBlock.Number())

	blockID, err := backend.getLastBlockByBatchId(proposalID)
	if !errors.Is(err, ErrProposalLastBlockUncertain) {
		t.Fatalf("expected ErrProposalLastBlockUncertain, got %v", err)
	}
	if blockID != nil {
		t.Fatalf("expected nil blockID, got %v", blockID)
	}
}

func TestGetLastBlockByBatchIdLookbackLimit(t *testing.T) {
	chainLength := int(maxBatchLookupBlocks + 2)

	proposalBytes := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	proposalID := new(big.Int).SetBytes(proposalBytes)
	matchExtra := append([]byte{0x00}, proposalBytes...)
	otherExtra := append([]byte{0x00}, []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}...)
	data := make([]byte, len(taiko.AnchorV4Selector))
	copy(data, taiko.AnchorV4Selector)

	genesis := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc: types.GenesisAlloc{
			testAddr: {Balance: big.NewInt(1_000_000_000_000_000_000)},
		},
	}
	engine := ethash.NewFaker()

	db := rawdb.NewMemoryDatabase()
	chain, err := core.NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	genesisBlock := chain.Genesis()
	if genesisBlock == nil {
		t.Fatal("missing genesis block")
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &common.Address{1},
		Value:    big.NewInt(0),
		Gas:      50_000,
		GasPrice: big.NewInt(1),
		Data:     data,
	})

	parentHash := genesisBlock.Hash()
	var headBlock *types.Block
	for i := 1; i <= chainLength; i++ {
		extra := otherExtra
		if i == 1 {
			extra = matchExtra
		}
		header := &types.Header{
			ParentHash: parentHash,
			Number:     new(big.Int).SetUint64(uint64(i)),
			Time:       uint64(i),
			Difficulty: big.NewInt(1),
			GasLimit:   30_000_000,
			GasUsed:    0,
			BaseFee:    big.NewInt(0),
			Extra:      extra,
		}
		block := types.NewBlockWithHeader(header).WithBody(types.Body{
			Transactions: types.Transactions{tx},
		})
		rawdb.WriteBlock(db, block)
		rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
		parentHash = block.Hash()
		headBlock = block
	}
	if headBlock == nil {
		t.Fatal("failed to build test chain")
	}

	backend := &TaikoAPIBackend{eth: &Ethereum{blockchain: chain, chainDb: db}}
	rawdb.WriteL1Origin(db, headBlock.Number(), &rawdb.L1Origin{
		BlockID:     headBlock.Number(),
		L2BlockHash: headBlock.Hash(),
	})
	rawdb.WriteHeadL1Origin(db, headBlock.Number())

	blockID, err := backend.getLastBlockByBatchId(proposalID)
	if !errors.Is(err, ErrProposalLastBlockLookbackExceeded) {
		t.Fatalf("expected ErrProposalLastBlockLookbackExceeded, got %v", err)
	}
	if blockID != nil {
		t.Fatalf("expected nil blockID, got %v", blockID)
	}
}

func newShastaTestChain(t *testing.T) (ethdb.Database, *core.BlockChain, *big.Int, []*types.Block) {
	t.Helper()

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

	db := rawdb.NewMemoryDatabase()
	chain, err := core.NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert chain: %v", err)
	}

	return db, chain, proposalID, blocks
}
