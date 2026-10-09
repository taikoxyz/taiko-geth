package eth

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// newAnchorReplayChain seals and imports block 1 of a chain with every fork
// through Unzen active from genesis and Etna unscheduled, whose golden touch
// holds no funds. The block's first transaction is its anchor, the second a
// transfer from testAddr; its extraData shares 25% of the base fee with the
// beneficiary.
func newAnchorReplayChain(t *testing.T) (*Ethereum, *types.Block) {
	t.Helper()
	config := etnaTestChainConfig()
	config.EtnaTime = nil
	gspec := etnaReplayGenesis(config)
	delete(gspec.Alloc, taiko.GoldenTouchAccount)
	eth := newEtnaTestEthereum(t, gspec, false)
	parent := eth.blockchain.CurrentBlock()

	// Before Etna the sealer recomputes the base fee with EIP-4396, and the
	// anchor must pay exactly that base fee.
	baseFee := misc.CalcEIP4396BaseFee(config, parent, 0)
	signer := types.LatestSigner(config)
	taikoL2 := core.TaikoTreasuryAddress(config.ChainID)
	anchor := types.MustSignNewTx(goldenTouchTestKey(t), signer, &types.DynamicFeeTx{
		ChainID: config.ChainID, GasTipCap: common.Big0, GasFeeCap: baseFee,
		Gas: taiko.AnchorV3V4GasLimit, To: &taikoL2, Data: taiko.AnchorV4Selector,
	})
	transfer := types.MustSignNewTx(testKey, signer, &types.DynamicFeeTx{
		ChainID: config.ChainID, GasTipCap: common.Big1, GasFeeCap: new(big.Int).Mul(baseFee, common.Big2),
		Gas: params.TxGas, To: &etnaReplayBeneficiary, Value: common.Big1,
	})
	txList, err := rlp.EncodeToBytes(types.Transactions{anchor, transfer})
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	timestamp := parent.Time + 1
	block, err := eth.miner.SealBlockWith(parent, 0, &engine.PayloadAttributes{
		Timestamp:             timestamp,
		Random:                common.HexToHash("0x0a"),
		SuggestedFeeRecipient: etnaReplayBeneficiary,
		Withdrawals:           []*types.Withdrawal{},
		BaseFeePerGas:         baseFee,
		BlockMetadata: &engine.BlockMetadata{
			Beneficiary: etnaReplayBeneficiary,
			GasLimit:    30_000_000,
			Timestamp:   timestamp,
			MixHash:     common.HexToHash("0x0b"),
			TxList:      txList,
			ExtraData:   []byte{25, 0, 0, 0, 0, 0, 1},
		},
	})
	if err != nil {
		t.Fatalf("SealBlockWith: %v", err)
	}
	if len(block.Transactions()) != 2 {
		t.Fatalf("sealed %d transactions, want 2", len(block.Transactions()))
	}
	if _, err := eth.blockchain.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("InsertChain: %v", err)
	}
	return eth, block
}

// TestStateAtTransactionFlagsAnchorOnMessage pins that replaying a pre-Etna
// block executes its first transaction as the fee-exempt anchor, which leaves
// the unfunded golden touch untouched, and that the replayed state reproduces
// the block's state root, without marking the block's anchor transaction.
func TestStateAtTransactionFlagsAnchorOnMessage(t *testing.T) {
	eth, block := newAnchorReplayChain(t)
	config := eth.blockchain.Config()

	tx, blockCtx, statedb, release, err := eth.stateAtTransaction(context.Background(), block, 1, 0)
	if err != nil {
		t.Fatalf("stateAtTransaction: %v", err)
	}
	defer release()
	if balance := statedb.GetBalance(taiko.GoldenTouchAccount); !balance.IsZero() {
		t.Fatalf("golden touch balance = %v, want the fee-exempt anchor to leave 0", balance)
	}

	// Executing the last transaction on the replayed state reproduces the root.
	msg, err := core.TransactionToMessage(tx, types.MakeSigner(config, block.Number(), block.Time()), block.BaseFee())
	if err != nil {
		t.Fatalf("TransactionToMessage: %v", err)
	}
	msg.BasefeeSharingPctg = core.DecodeShastaBasefeeSharingPctg(block.Extra())
	statedb.SetTxContext(tx.Hash(), 1)
	if _, err := core.ApplyMessage(vm.NewEVM(blockCtx, statedb, config, vm.Config{}), msg, core.NewGasPool(block.GasLimit())); err != nil {
		t.Fatalf("ApplyMessage: %v", err)
	}
	if root := statedb.IntermediateRoot(config.IsEIP158(block.Number())); root != block.Root() {
		t.Fatalf("replayed state root = %v, want %v", root, block.Root())
	}

	for _, b := range []*types.Block{block, eth.blockchain.GetBlockByHash(block.Hash())} {
		if b.Transactions()[0].IsAnchor() {
			t.Fatal("the block's anchor transaction is marked")
		}
	}
}

// TestStateAtTransactionRejectsAnchorOfUnsupportedType pins that replaying a
// pre-Etna block whose first transaction cannot be an anchor fails with
// ErrTxTypeNotSupported, whichever transaction is requested.
func TestStateAtTransactionRejectsAnchorOfUnsupportedType(t *testing.T) {
	eth, canonical := newAnchorReplayChain(t)
	config := eth.blockchain.Config()
	taikoL2 := core.TaikoTreasuryAddress(config.ChainID)
	legacy := types.MustSignNewTx(goldenTouchTestKey(t), types.LatestSigner(config), &types.LegacyTx{
		GasPrice: canonical.BaseFee(), Gas: taiko.AnchorV3V4GasLimit, To: &taikoL2, Data: taiko.AnchorV4Selector,
	})
	block := types.NewBlockWithHeader(canonical.Header()).WithBody(types.Body{
		Transactions: types.Transactions{legacy, canonical.Transactions()[1]},
	})
	for _, index := range []int{0, 1} {
		if _, _, _, _, err := eth.stateAtTransaction(context.Background(), block, index, 0); !errors.Is(err, types.ErrTxTypeNotSupported) {
			t.Fatalf("stateAtTransaction(%d): expected %v, got %v", index, types.ErrTxTypeNotSupported, err)
		}
	}
}
