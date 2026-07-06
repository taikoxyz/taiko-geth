package eth

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestGetLastBlockByBatchIdSkipsFlagFormatExtraData covers the batch-ID
// fallback walk crossing from flag-format extraData (which carries no
// embedded proposalId) into proposal-id-bearing history: the flag-format
// blocks must be skipped, not fail the whole lookup.
func TestGetLastBlockByBatchIdSkipsFlagFormatExtraData(t *testing.T) {
	proposalBytes := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	proposalID := new(big.Int).SetBytes(proposalBytes)
	matchExtra := append([]byte{0x4b}, proposalBytes...)
	higherExtra := append([]byte{0x4b}, []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}...)
	flagExtra := []byte{0x4b, 0x00}
	extras := map[uint64][]byte{
		1: matchExtra,
		2: higherExtra,
		3: flagExtra,
		4: flagExtra,
	}

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
	chain, err := core.NewBlockChain(db, genesis, engine, nil)
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
	for i := uint64(1); i <= 4; i++ {
		header := &types.Header{
			ParentHash: parentHash,
			Number:     new(big.Int).SetUint64(i),
			Time:       i,
			Difficulty: big.NewInt(1),
			GasLimit:   30_000_000,
			GasUsed:    0,
			BaseFee:    big.NewInt(0),
			Extra:      extras[i],
		}
		block := types.NewBlockWithHeader(header).WithBody(types.Body{
			Transactions: types.Transactions{tx},
		})
		rawdb.WriteBlock(db, block)
		rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
		parentHash = block.Hash()
		headBlock = block
	}

	backend := &TaikoAuthAPIBackend{eth: &Ethereum{blockchain: chain, chainDb: db}}
	rawdb.WriteL1Origin(db, headBlock.Number(), &rawdb.L1Origin{
		BlockID:       headBlock.Number(),
		L2BlockHash:   headBlock.Hash(),
		L1BlockHeight: big.NewInt(1),
	})
	rawdb.WriteHeadL1Origin(db, headBlock.Number())
	chain.HeaderChain().SetCurrentHeader(headBlock.Header())

	blockID, err := backend.getLastBlockByBatchIdWithLimit(proposalID, 10)
	if err != nil {
		t.Fatalf("expected lookup to succeed, got %v", err)
	}
	if blockID == nil || (*big.Int)(blockID).Uint64() != 1 {
		t.Fatalf("expected block 1, got %v", blockID)
	}
}
