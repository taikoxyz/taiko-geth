package miner

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
)

var (
	two64  = new(big.Int).Lsh(common.Big1, 64)
	two128 = new(big.Int).Lsh(common.Big1, 128)
	two256 = new(big.Int).Lsh(common.Big1, 256)
)

// grammarTestTx is a named transaction for the shared execution transaction
// grammar tables.
type grammarTestTx struct {
	name string
	tx   *types.Transaction
}

// rawDynamicFeeTx returns a dynamic fee transaction with raw signature values
// (parity 0, r = s = 1) after applying edit. It only has to encode, not to
// carry a valid signature.
func rawDynamicFeeTx(chainID *big.Int, edit func(*types.DynamicFeeTx)) *types.Transaction {
	to := testUserAddress
	inner := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     1,
		GasTipCap: common.Big1,
		GasFeeCap: big.NewInt(2 * params.InitialBaseFee),
		Gas:       params.TxGas,
		To:        &to,
		Value:     common.Big0,
		V:         common.Big0,
		R:         common.Big1,
		S:         common.Big1,
	}
	if edit != nil {
		edit(inner)
	}
	return types.NewTx(inner)
}

// rawLegacyTx returns a legacy transaction with raw signature values (v = 27,
// r = s = 1) after applying edit.
func rawLegacyTx(edit func(*types.LegacyTx)) *types.Transaction {
	to := testUserAddress
	inner := &types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(2 * params.InitialBaseFee),
		Gas:      params.TxGas,
		To:       &to,
		Value:    common.Big0,
		V:        big.NewInt(27),
		R:        common.Big1,
		S:        common.Big1,
	}
	if edit != nil {
		edit(inner)
	}
	return types.NewTx(inner)
}

// rawBlobTx returns a blob transaction with raw signature values (parity 0,
// r = s = 1) after applying edit.
func rawBlobTx(chainID *big.Int, edit func(*types.BlobTx)) *types.Transaction {
	inner := &types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      1,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.NewInt(2 * params.InitialBaseFee),
		Gas:        params.TxGas,
		To:         testUserAddress,
		Value:      uint256.NewInt(0),
		BlobFeeCap: uint256.NewInt(1),
		BlobHashes: []common.Hash{{0x01}},
		V:          uint256.NewInt(0),
		R:          uint256.NewInt(1),
		S:          uint256.NewInt(1),
	}
	if edit != nil {
		edit(inner)
	}
	return types.NewTx(inner)
}

// legacyV returns the EIP-155 legacy v that encodes chainID with parity 0.
func legacyV(chainID *big.Int) *big.Int {
	return new(big.Int).Add(new(big.Int).Lsh(chainID, 1), big.NewInt(35))
}

// etnaGrammarViolations returns one transaction per rule of the shared
// execution transaction grammar that the RLP decoder still accepts.
func etnaGrammarViolations(chainID *big.Int) []grammarTestTx {
	return []grammarTestTx{
		{"max fee per gas of 2^128", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.GasFeeCap = two128 })},
		{"priority fee of 2^128", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.GasTipCap = two128 })},
		{"value of 2^256", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.Value = two256 })},
		{"signature s of 2^256", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.S = two256 })},
		{"typed parity 2", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.V = common.Big2 })},
		{"typed chain ID of 2^64", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.ChainID = two64 })},
		{"legacy v of 29", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = big.NewInt(29) })},
		{"legacy chain ID of 2^64", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = legacyV(two64) })},
		{"legacy signature r of 2^256", rawLegacyTx(func(tx *types.LegacyTx) { tx.R = two256 })},
		{"blob fee cap of 2^128", rawBlobTx(chainID, func(tx *types.BlobTx) { tx.BlobFeeCap = uint256.MustFromBig(two128) })},
		{"blob sidecar wrapper", rawBlobTx(chainID, func(tx *types.BlobTx) {
			tx.Sidecar = types.NewBlobTxSidecar(types.BlobSidecarVersion0, []kzg4844.Blob{{}}, []kzg4844.Commitment{{}}, []kzg4844.Proof{{}})
		})},
	}
}

// encodeWithDepositTx returns the RLP list [tx, deposit-type transaction]. The
// transaction types cannot build a deposit-type transaction, so the list is
// encoded by hand: typed transactions are RLP strings inside the list.
func encodeWithDepositTx(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	enc, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("encode transaction: %v", err)
	}
	if handmade, err := rlp.EncodeToBytes([][]byte{enc}); err != nil || !bytes.Equal(handmade, encodeTestTxList(t, tx)) {
		t.Fatalf("hand-encoded list %x (err %v) differs from the canonical encoding", handmade, err)
	}
	deposit, err := rlp.EncodeToBytes(&types.DepositTx{From: testBankAddress, To: &testUserAddress, Value: common.Big0, Gas: params.TxGas})
	if err != nil {
		t.Fatalf("encode deposit: %v", err)
	}
	list, err := rlp.EncodeToBytes([][]byte{enc, append([]byte{types.DepositTxType}, deposit...)})
	if err != nil {
		t.Fatalf("encode list: %v", err)
	}
	return list
}

// TestValidateEtnaTxEncoding pins the rules of the shared execution transaction
// grammar at their boundaries. The type rule cannot be reached from here: a
// transaction can only hold one of the allowed types, and the decoder rejects
// any other (see TestDecodeEtnaTxList).
func TestValidateEtnaTxEncoding(t *testing.T) {
	config := newEtnaTestChainConfig()
	chainID := config.ChainID
	signer := types.LatestSigner(config)
	to := testUserAddress
	var (
		minus1 = big.NewInt(-1)
		max64  = new(big.Int).Sub(two64, common.Big1)
		max128 = new(big.Int).Sub(two128, common.Big1)
		max256 = new(big.Int).Sub(two256, common.Big1)
	)

	accepted := []grammarTestTx{
		{"signed dynamic fee transfer", bankTransfer(t, config, 0)},
		{"signed EIP-155 legacy transfer", types.MustSignNewTx(testBankKey, signer, &types.LegacyTx{
			GasPrice: big.NewInt(2 * params.InitialBaseFee), Gas: params.TxGas, To: &to, Value: common.Big1,
		})},
		{"signed access list transfer", types.MustSignNewTx(testBankKey, signer, &types.AccessListTx{
			ChainID: chainID, GasPrice: big.NewInt(2 * params.InitialBaseFee), Gas: params.TxGas, To: &to, Value: common.Big1,
		})},
		{"set code transaction", types.NewTx(&types.SetCodeTx{
			ChainID: uint256.MustFromBig(chainID), GasTipCap: uint256.NewInt(1), GasFeeCap: uint256.NewInt(2 * params.InitialBaseFee),
			Gas: params.TxGas, To: to, Value: uint256.NewInt(0), V: uint256.NewInt(1), R: uint256.NewInt(1), S: uint256.NewInt(1),
		})},
		{"blob transaction without sidecar", rawBlobTx(chainID, nil)},
		{"legacy v of 27", rawLegacyTx(nil)},
		{"legacy v of 28", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = big.NewInt(28) })},
		{"legacy v of 35", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = big.NewInt(35) })},
		{"legacy chain ID of 2^64-1", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = legacyV(max64) })},
		{"legacy chain ID of 2^64-1 with parity 1", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = new(big.Int).Add(legacyV(max64), common.Big1) })},
		{"legacy signature r of 2^256-1", rawLegacyTx(func(tx *types.LegacyTx) { tx.R = max256 })},
		{"legacy gas price of 2^128-1", rawLegacyTx(func(tx *types.LegacyTx) { tx.GasPrice = max128 })},
		{"max fee per gas of 2^128-1", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.GasFeeCap = max128 })},
		{"priority fee of 2^128-1", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.GasTipCap = max128 })},
		{"value of 2^256-1", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.Value = max256 })},
		{"signature s of 2^256-1", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.S = max256 })},
		{"typed parity 1", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.V = common.Big1 })},
		{"typed chain ID of 2^64-1", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.ChainID = max64 })},
		{"blob fee cap of 2^128-1", rawBlobTx(chainID, func(tx *types.BlobTx) { tx.BlobFeeCap = uint256.MustFromBig(max128) })},
	}
	rejected := append(etnaGrammarViolations(chainID),
		grammarTestTx{"negative max fee per gas", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.GasFeeCap = minus1 })},
		grammarTestTx{"negative priority fee", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.GasTipCap = minus1 })},
		grammarTestTx{"negative value", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.Value = minus1 })},
		grammarTestTx{"negative signature r", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.R = minus1 })},
		grammarTestTx{"negative signature s", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.S = minus1 })},
		grammarTestTx{"typed parity -1", rawDynamicFeeTx(chainID, func(tx *types.DynamicFeeTx) { tx.V = minus1 })},
		grammarTestTx{"legacy gas price of 2^128", rawLegacyTx(func(tx *types.LegacyTx) { tx.GasPrice = two128 })},
		grammarTestTx{"legacy v of 0", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = common.Big0 })},
		grammarTestTx{"legacy v of 34", rawLegacyTx(func(tx *types.LegacyTx) { tx.V = big.NewInt(34) })},
		grammarTestTx{"legacy signature s of 2^256", rawLegacyTx(func(tx *types.LegacyTx) { tx.S = two256 })},
	)

	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			if err := validateEtnaTxEncoding(tc.tx); err != nil {
				t.Fatalf("rejected: %v", err)
			}
		})
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if err := validateEtnaTxEncoding(tc.tx); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// TestDecodeEtnaTxList pins that only the first RLP value is the list, that
// any bytes after it are ignored, and that a list fails as a whole.
func TestDecodeEtnaTxList(t *testing.T) {
	config := newEtnaTestChainConfig()
	transfer := bankTransfer(t, config, 0)
	transferList := encodeTestTxList(t, transfer)
	violation := etnaGrammarViolations(config.ChainID)[0].tx

	for _, tt := range []struct {
		name   string
		txList []byte
		want   []*types.Transaction
		fails  bool
	}{
		{"empty list", []byte{0xc0}, nil, false},
		{"one transfer", transferList, []*types.Transaction{transfer}, false},
		{"trailing zero byte", append(bytes.Clone(transferList), 0x00), []*types.Transaction{transfer}, false},
		{"trailing empty list", append(bytes.Clone(transferList), 0xc0), []*types.Transaction{transfer}, false},
		{"trailing truncated header", append(bytes.Clone(transferList), 0xff), []*types.Transaction{transfer}, false},
		{"no bytes", []byte{}, nil, true},
		{"truncated list", []byte{0xc1}, nil, true},
		{"not a list", []byte{0x01}, nil, true},
		{"deposit type", encodeWithDepositTx(t, transfer), nil, true},
		{"grammar violation after a transfer", encodeTestTxList(t, transfer, violation), nil, true},
		{"grammar violation before a transfer", encodeTestTxList(t, violation, transfer), nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			txs, err := decodeEtnaTxList(tt.txList)
			if (err != nil) != tt.fails {
				t.Fatalf("err = %v, want failure %t", err, tt.fails)
			}
			if len(txs) != len(tt.want) {
				t.Fatalf("decoded %d transactions, want %d", len(txs), len(tt.want))
			}
			for i := range txs {
				if txs[i].Hash() != tt.want[i].Hash() {
					t.Fatalf("transaction %d = %v, want %v", i, txs[i].Hash(), tt.want[i].Hash())
				}
			}
		})
	}
}

// etnaTxListCorpusEntry is one input of testdata/etna_txlist_corpus.json with
// its expected verdict from the shared cross-client vectors: whether the list
// decodes and, for every decoded transaction, its hash and the recovered
// sender (null when signer recovery fails).
type etnaTxListCorpusEntry struct {
	Name  string        `json:"name"`
	Input hexutil.Bytes `json:"input"`
	OK    bool          `json:"ok"`
	Txs   []struct {
		Hash   common.Hash     `json:"hash"`
		Sender *common.Address `json:"sender"`
	} `json:"txs"`
}

// TestDecodeEtnaTxListCorpus checks decodeEtnaTxList and the signer recovery
// of the Etna sealer against the shared cross-client vectors, which cover the
// list framing, every transaction type, field widths, signature values and
// sender recovery.
//
// The vectors recover a sender without checking the chain ID; execution then
// rejects a mismatching chain ID as an invalid transaction. The sealer's
// signer rejects it during recovery instead. Both skip the transaction, so a
// chain-ID error here stands for a recovered sender.
func TestDecodeEtnaTxListCorpus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "etna_txlist_corpus.json"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var corpus []etnaTxListCorpusEntry
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	if len(corpus) < 40 {
		t.Fatalf("corpus has %d entries, want at least 40", len(corpus))
	}
	signer := types.LatestSignerForChainID(params.TaikoInternalNetworkID)
	for _, tc := range corpus {
		t.Run(tc.Name, func(t *testing.T) {
			txs, err := decodeEtnaTxList(tc.Input)
			if !tc.OK {
				if err == nil {
					t.Fatalf("decoded %d transactions, want the list rejected", len(txs))
				}
				return
			}
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(txs) != len(tc.Txs) {
				t.Fatalf("decoded %d transactions, want %d", len(txs), len(tc.Txs))
			}
			for i, want := range tc.Txs {
				if txs[i].Hash() != want.Hash {
					t.Fatalf("transaction %d hash = %v, want %v", i, txs[i].Hash(), want.Hash)
				}
				sender, err := types.Sender(signer, txs[i])
				switch {
				case want.Sender == nil:
					if err == nil {
						t.Fatalf("transaction %d recovered sender %v, want a recovery failure", i, sender)
					}
				case errors.Is(err, types.ErrInvalidChainId):
					// Skipped by both: see the test comment.
				case err != nil:
					t.Fatalf("transaction %d recovery failed: %v, want sender %v", i, err, *want.Sender)
				case sender != *want.Sender:
					t.Fatalf("transaction %d sender = %v, want %v", i, sender, *want.Sender)
				}
			}
		})
	}
}

// TestSealBlockWith_EtnaAcceptsEveryItemShape seals and imports Etna blocks
// whose single transfer is framed in each list item shape the grammar
// accepts beyond the canonical one. The sealed block holds the transfer in its
// canonical encoding either way.
func TestSealBlockWith_EtnaAcceptsEveryItemShape(t *testing.T) {
	config := newEtnaTestChainConfig()
	signer := types.LatestSigner(config)
	transfer := bankTransfer(t, config, 0)
	typed, err := transfer.MarshalBinary()
	if err != nil {
		t.Fatalf("encode transfer: %v", err)
	}
	legacy := types.MustSignNewTx(testBankKey, signer, &types.LegacyTx{
		GasPrice: big.NewInt(2 * params.InitialBaseFee), Gas: params.TxGas, To: &testUserAddress, Value: common.Big1,
	})
	legacyPayload, err := legacy.MarshalBinary()
	if err != nil {
		t.Fatalf("encode legacy transfer: %v", err)
	}
	list := func(items ...[]byte) []byte {
		raw := make([]rlp.RawValue, len(items))
		for i, item := range items {
			raw[i] = item
		}
		enc, err := rlp.EncodeToBytes(raw)
		if err != nil {
			t.Fatalf("encode list: %v", err)
		}
		return enc
	}

	for _, tt := range []struct {
		name string
		item []byte
		want *types.Transaction
	}{
		{"bare typed item", typed, transfer},
		{"wrapped with declared length 0", append([]byte{0x80}, typed...), transfer},
		{"payload without type byte", typed[1:], transfer},
		{"legacy with type byte 0", append([]byte{types.LegacyTxType}, legacyPayload...), legacy},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))
			block := sealAndImportEtna(t, w, b, etnaTestAttributes(b.chain.CurrentBlock(), list(tt.item)))
			if txs := block.Transactions(); len(txs) != 1 || txs[0].Hash() != tt.want.Hash() {
				t.Fatalf("sealed %d transactions, want exactly %v", len(txs), tt.want.Hash())
			}
		})
	}
}

// TestDecodeTaikoNetworkTransactionCorpus decodes the first item of every
// decodable list of the shared cross-client vectors as a single transaction,
// with the later items as trailing bytes, and recovers its signer without a
// chain-ID check: the vectors' sender, or a recovery failure where they have
// none.
func TestDecodeTaikoNetworkTransactionCorpus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "etna_txlist_corpus.json"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var corpus []etnaTxListCorpusEntry
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	var checked int
	for _, tc := range corpus {
		if !tc.OK || len(tc.Txs) == 0 {
			continue
		}
		checked++
		t.Run(tc.Name, func(t *testing.T) {
			_, items, _, err := rlp.Split(tc.Input)
			if err != nil {
				t.Fatalf("split list: %v", err)
			}
			tx, err := DecodeTaikoNetworkTransaction(items)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			want := tc.Txs[0]
			if tx.Hash() != want.Hash {
				t.Fatalf("hash = %v, want %v", tx.Hash(), want.Hash)
			}
			sender, err := RecoverTaikoTransactionSender(tx)
			switch {
			case want.Sender == nil:
				if err == nil {
					t.Fatalf("recovered sender %v, want a recovery failure", sender)
				}
			case err != nil:
				t.Fatalf("recovery failed: %v, want sender %v", err, *want.Sender)
			case sender != *want.Sender:
				t.Fatalf("sender = %v, want %v", sender, *want.Sender)
			}
		})
	}
	if checked < 40 {
		t.Fatalf("checked %d decodable lists, want at least 40", checked)
	}
}

// TestDecodeTaikoNetworkTransactionRejects checks that a single item outside
// the grammar does not decode: the only item of some undecodable lists of the
// shared cross-client vectors, and inputs too short to hold a transaction.
func TestDecodeTaikoNetworkTransactionRejects(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "etna_txlist_corpus.json"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var corpus []etnaTxListCorpusEntry
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	entries := make(map[string]etnaTxListCorpusEntry, len(corpus))
	for _, tc := range corpus {
		entries[tc.Name] = tc
	}
	for _, name := range []string{
		"element-empty-list",
		"element-zero-byte",
		"element-non-canonical-single-byte",
		"typed-payload-extra-field",
		"typed-payload-not-a-list",
		"unknown-type-5-bare",
		"wrapped-declared-one",
		"wrapped-declared-beyond-list",
		"wrapped-legacy-without-type",
		"fee-cap-2^128",
		"typed-parity-2",
		"legacy-v-29",
		"blob-sidecar-wrapper",
	} {
		tc, ok := entries[name]
		if !ok || tc.OK {
			t.Fatalf("%s: not an undecodable list of the corpus", name)
		}
		_, items, _, err := rlp.Split(tc.Input)
		if err != nil {
			t.Fatalf("%s: split list: %v", name, err)
		}
		if tx, err := DecodeTaikoNetworkTransaction(items); err == nil {
			t.Errorf("%s: decoded %v, want an error", name, tx.Hash())
		}
	}
	for _, input := range []string{"0x", "0x80", "0xc0", "0x02", "0x8102", "0xdead"} {
		if tx, err := DecodeTaikoNetworkTransaction(common.FromHex(input)); err == nil {
			t.Errorf("%s: decoded %v, want an error", input, tx.Hash())
		}
	}
}

// TestDecodeTaikoNetworkTransactionTrailingBytes checks that the bytes after
// the transaction, in each item shape, are ignored.
func TestDecodeTaikoNetworkTransactionTrailingBytes(t *testing.T) {
	config := newEtnaTestChainConfig()
	transfer := bankTransfer(t, config, 0)
	typed, err := transfer.MarshalBinary()
	if err != nil {
		t.Fatalf("encode transfer: %v", err)
	}
	wrapped, err := rlp.EncodeToBytes(typed)
	if err != nil {
		t.Fatalf("wrap transfer: %v", err)
	}
	for _, tt := range []struct {
		name string
		item []byte
	}{
		{"bare typed", typed},
		{"wrapped typed", wrapped},
		{"payload without type byte", typed[1:]},
	} {
		for _, junk := range [][]byte{nil, {0x00}, {0xc0}, {0xff, 0xff}, typed} {
			input := append(append([]byte{}, tt.item...), junk...)
			tx, err := DecodeTaikoNetworkTransaction(input)
			if err != nil || tx.Hash() != transfer.Hash() {
				t.Fatalf("%s with trailing %x: decoded %v (err %v), want %v", tt.name, junk, tx, err, transfer.Hash())
			}
		}
	}
}

// TestRecoverTaikoTransactionSenderChainIDs checks that a signature recovers
// whatever chain ID it was made for, including the zero chain ID the signers
// do not take, and that a high s never recovers.
func TestRecoverTaikoTransactionSenderChainIDs(t *testing.T) {
	want := crypto.PubkeyToAddress(testBankKey.PublicKey)
	to := testUserAddress
	unsigned := types.DynamicFeeTx{
		Nonce:     3,
		GasTipCap: common.Big1,
		GasFeeCap: big.NewInt(params.InitialBaseFee),
		Gas:       params.TxGas,
		To:        &to,
		Value:     common.Big1,
		Data:      []byte{0xca, 0xfe},
	}
	// sign signs unsigned for chainID over its EIP-1559 signing payload,
	// returning a signature with a low s.
	sign := func(chainID *big.Int) *types.DynamicFeeTx {
		inner := unsigned
		inner.ChainID = chainID
		payload, err := rlp.EncodeToBytes([]any{
			inner.ChainID, inner.Nonce, inner.GasTipCap, inner.GasFeeCap, inner.Gas,
			inner.To, inner.Value, inner.Data, types.AccessList{},
		})
		if err != nil {
			t.Fatalf("encode signing payload: %v", err)
		}
		sig, err := crypto.Sign(crypto.Keccak256(append([]byte{types.DynamicFeeTxType}, payload...)), testBankKey)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		inner.R = new(big.Int).SetBytes(sig[:32])
		inner.S = new(big.Int).SetBytes(sig[32:64])
		inner.V = big.NewInt(int64(sig[64]))
		return &inner
	}
	for _, chainID := range []*big.Int{common.Big0, common.Big1, params.TaikoInternalNetworkID, new(big.Int).SetUint64(math.MaxUint64)} {
		signed := sign(chainID)
		if sender, err := RecoverTaikoTransactionSender(types.NewTx(signed)); err != nil || sender != want {
			t.Fatalf("chain ID %v: sender %v (err %v), want %v", chainID, sender, err, want)
		}
		// The same signature with s mirrored to the upper half of the curve
		// order is valid ECDSA but not EIP-2.
		highS := *signed
		highS.S = new(big.Int).Sub(crypto.S256().Params().N, signed.S)
		highS.V = new(big.Int).Xor(signed.V, common.Big1)
		if sender, err := RecoverTaikoTransactionSender(types.NewTx(&highS)); err == nil {
			t.Fatalf("chain ID %v: high-s signature recovered %v", chainID, sender)
		}
	}
}
