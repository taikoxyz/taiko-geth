package miner

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// unzenAnchorTx signs the anchor transaction of a block on the Unzen test
// chain: a golden-touch call to TaikoL2 paying the initial base fee.
func unzenAnchorTx(t *testing.T, config *params.ChainConfig) *types.Transaction {
	t.Helper()
	taikoL2 := core.TaikoTreasuryAddress(config.ChainID)
	return types.MustSignNewTx(goldenTouchTestKey(t), types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		GasTipCap: common.Big0,
		GasFeeCap: big.NewInt(params.InitialBaseFee),
		Gas:       taiko.AnchorGasLimit,
		To:        &taikoL2,
		Data:      taiko.AnchorSelector,
	})
}

// unzenAnchorAttributes returns the payload attributes of the block after
// parent on the Unzen test chain, sealing txList.
func unzenAnchorAttributes(parent *types.Header, txList []byte) *engine.PayloadAttributes {
	timestamp := parent.Time + 1
	return &engine.PayloadAttributes{
		Timestamp:     timestamp,
		BaseFeePerGas: big.NewInt(params.InitialBaseFee),
		BlockMetadata: &engine.BlockMetadata{
			Beneficiary: testUserAddress,
			GasLimit:    parent.GasLimit,
			Timestamp:   timestamp,
			TxList:      txList,
			ExtraData:   []byte{},
		},
	}
}

// TestSealBlockWith_FlagsAnchorOnMessage pins that sealing grants the pre-Etna
// anchor its exemptions through its message: the anchor of the golden touch,
// which holds no funds, seals and imports with the transfer after it, and
// neither the sealed nor the imported anchor transaction is marked.
func TestSealBlockWith_FlagsAnchorOnMessage(t *testing.T) {
	config := newUnzenTestChainConfig()
	w, b := newUnzenTestWorker(t, newUnzenTestGenesis(config))
	parent := b.chain.CurrentBlock()
	anchor, transfer := unzenAnchorTx(t, config), bankTransfer(t, config, 0)

	block := sealAndImportEtna(t, w, b, unzenAnchorAttributes(parent, encodeTestTxList(t, anchor, transfer)))
	txs := block.Transactions()
	if len(txs) != 2 || txs[0].Hash() != anchor.Hash() || txs[1].Hash() != transfer.Hash() {
		t.Fatalf("sealed %d transactions, want the anchor and the transfer", len(txs))
	}
	if txs[0].IsAnchor() {
		t.Fatal("sealing marked the anchor transaction")
	}
	if imported := b.chain.GetBlockByHash(block.Hash()); imported.Transactions()[0].IsAnchor() {
		t.Fatal("import marked the anchor transaction")
	}
	statedb, err := b.chain.StateAt(block.Root())
	if err != nil {
		t.Fatalf("StateAt: %v", err)
	}
	if balance := statedb.GetBalance(taiko.GoldenTouchAccount); !balance.IsZero() {
		t.Fatalf("golden touch balance = %v, want the fee-exempt anchor to leave 0", balance)
	}
}

// TestSealBlockWith_RejectsAnchorOfUnsupportedType pins that before Etna a
// first transaction that cannot be an anchor fails sealing with
// ErrTxTypeNotSupported, a blob transaction included: the anchor check runs
// before blob transactions are skipped.
func TestSealBlockWith_RejectsAnchorOfUnsupportedType(t *testing.T) {
	config := newUnzenTestChainConfig()
	signer := types.LatestSigner(config)
	taikoL2 := core.TaikoTreasuryAddress(config.ChainID)
	for name, tx := range map[string]*types.Transaction{
		"legacy": types.MustSignNewTx(goldenTouchTestKey(t), signer, &types.LegacyTx{
			GasPrice: big.NewInt(params.InitialBaseFee), Gas: taiko.AnchorGasLimit, To: &taikoL2, Data: taiko.AnchorSelector,
		}),
		"blob": types.MustSignNewTx(goldenTouchTestKey(t), signer, &types.BlobTx{
			ChainID: uint256.MustFromBig(config.ChainID), GasTipCap: uint256.NewInt(0), GasFeeCap: uint256.NewInt(params.InitialBaseFee),
			Gas: taiko.AnchorGasLimit, To: taikoL2, Value: uint256.NewInt(0), Data: taiko.AnchorSelector,
			BlobFeeCap: uint256.NewInt(1), BlobHashes: []common.Hash{{0x01}},
		}),
	} {
		t.Run(name, func(t *testing.T) {
			w, b := newUnzenTestWorker(t, newUnzenTestGenesis(config))
			parent := b.chain.CurrentBlock()
			_, err := w.sealBlockWith(parent, 0, unzenAnchorAttributes(parent, encodeTestTxList(t, tx, bankTransfer(t, config, 0))))
			if !errors.Is(err, types.ErrTxTypeNotSupported) {
				t.Fatalf("sealBlockWith: expected %v, got %v", types.ErrTxTypeNotSupported, err)
			}
		})
	}
}
