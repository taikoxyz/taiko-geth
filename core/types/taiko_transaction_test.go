package types

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// TestDecodeTaikoTxList pins that a transaction whose chain ID or signature
// value does not fit 256 bits decodes in place, exactly as a whole-list decode
// did before decoding started rejecting such values, while every other
// decoding failure still fails the whole list.
func TestDecodeTaikoTxList(t *testing.T) {
	var (
		chainID = big.NewInt(167000)
		signer  = LatestSignerForChainID(chainID)
		key, _  = crypto.GenerateKey()
		from    = crypto.PubkeyToAddress(key.PublicKey)
		to      = common.HexToAddress("0x000000000000000000000000000000000000dead")
		huge    = new(big.Int).Lsh(common.Big1, 300)
	)
	dynamicFee := func(nonce uint64) *DynamicFeeTx {
		return &DynamicFeeTx{
			ChainID: chainID, Nonce: nonce, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
			Gas: 21000, To: &to, Value: big.NewInt(3), V: common.Big0, R: common.Big0, S: common.Big0,
		}
	}
	var (
		valid0      = MustSignNewTx(key, signer, dynamicFee(0))
		valid1      = MustSignNewTx(key, signer, dynamicFee(1))
		validLegacy = MustSignNewTx(key, signer, &LegacyTx{Nonce: 2, GasPrice: big.NewInt(2), Gas: 21000, To: &to, Value: big.NewInt(3)})

		typedV = func() *Transaction {
			inner := dynamicFee(3)
			inner.V, inner.R, inner.S = huge, common.Big1, common.Big1
			return NewTx(inner)
		}()
		legacyV = NewTx(&LegacyTx{
			Nonce: 4, GasPrice: big.NewInt(2), Gas: 21000, To: &to, Value: big.NewInt(3),
			V: huge, R: common.Big1, S: common.Big1,
		})
		typedChainID = func() *Transaction {
			inner := dynamicFee(5)
			inner.ChainID, inner.V, inner.R, inner.S = huge, common.Big1, common.Big1, common.Big1
			return NewTx(inner)
		}()
		oversized = map[*Transaction]bool{typedV: true, legacyV: true, typedChainID: true}
	)

	for _, tt := range []struct {
		name string
		txs  Transactions
	}{
		{"oversized typed V mid-list", Transactions{valid0, typedV, valid1}},
		{"oversized legacy V mid-list", Transactions{validLegacy, legacyV, valid1}},
		{"oversized typed chain ID mid-list", Transactions{valid0, typedChainID, valid1}},
		{"oversized in the first slot", Transactions{typedV, valid0, valid1}},
		{"all oversized kinds", Transactions{legacyV, valid0, typedChainID, typedV}},
		{"no oversized", Transactions{valid0, validLegacy, valid1}},
		{"empty", Transactions{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input, err := rlp.EncodeToBytes(tt.txs)
			if err != nil {
				t.Fatalf("encode list: %v", err)
			}
			got, err := DecodeTaikoTxList(input)
			if err != nil {
				t.Fatalf("DecodeTaikoTxList: %v", err)
			}
			if got == nil || len(got) != len(tt.txs) {
				t.Fatalf("decoded %d transactions (nil=%v), want %d", len(got), got == nil, len(tt.txs))
			}
			if out, err := rlp.EncodeToBytes(got); err != nil || !bytes.Equal(out, input) {
				t.Fatalf("re-encoded list differs from input (err=%v)", err)
			}
			for i, want := range tt.txs {
				elem, err := rlp.EncodeToBytes(want)
				if err != nil {
					t.Fatalf("tx %d: encode: %v", i, err)
				}
				if out, err := rlp.EncodeToBytes(got[i]); err != nil || !bytes.Equal(out, elem) {
					t.Fatalf("tx %d: re-encoding differs from input element (err=%v)", i, err)
				}
				if got[i].Hash() != want.Hash() {
					t.Fatalf("tx %d: hash %v, want %v", i, got[i].Hash(), want.Hash())
				}
				// A decoded transaction caches the size DecodeRLP records: the
				// whole element for a legacy list, the envelope for a typed one.
				wantSize := uint64(len(elem))
				if want.Type() != LegacyTxType {
					bin, err := want.MarshalBinary()
					if err != nil {
						t.Fatalf("tx %d: marshal: %v", i, err)
					}
					wantSize = uint64(len(bin))
				}
				if size := got[i].size.Load(); size != wantSize {
					t.Fatalf("tx %d: decoded size %d, want %d", i, size, wantSize)
				}

				strict := new(Transaction)
				strictErr := rlp.DecodeBytes(elem, strict)
				sender, sendErr := signer.Sender(got[i])
				if !oversized[want] {
					if strictErr != nil {
						t.Fatalf("tx %d: strict decode: %v", i, strictErr)
					}
					if size := strict.size.Load(); size != wantSize {
						t.Fatalf("tx %d: strict decoded size %d, want %d", i, size, wantSize)
					}
					if sendErr != nil || sender != from {
						t.Fatalf("tx %d: sender = (%v, %v), want %v", i, sender, sendErr, from)
					}
					continue
				}
				if !errors.Is(strictErr, errOversizedSigValue) {
					t.Fatalf("tx %d: strict decode = %v, want %v", i, strictErr, errOversizedSigValue)
				}
				v, _, _ := got[i].RawSignatureValues()
				if v.BitLen() <= 256 && got[i].ChainId().BitLen() <= 256 {
					t.Fatalf("tx %d: neither V (%d bits) nor chain ID (%d bits) exceeds 256 bits", i, v.BitLen(), got[i].ChainId().BitLen())
				}
				if sendErr == nil {
					t.Fatalf("tx %d: sender recovered from an oversized transaction", i)
				}
			}
		})
	}

	mustList := func(elems ...[]byte) []byte {
		raw := make([]rlp.RawValue, len(elems))
		for i, e := range elems {
			raw[i] = e
		}
		b, err := rlp.EncodeToBytes(raw)
		if err != nil {
			t.Fatalf("encode raw list: %v", err)
		}
		return b
	}
	mustElem := func(tx *Transaction) []byte {
		b, err := rlp.EncodeToBytes(tx)
		if err != nil {
			t.Fatalf("encode tx: %v", err)
		}
		return b
	}
	unsupported, _ := rlp.EncodeToBytes([]byte{0x7f, 0xc0})

	for _, tt := range []struct {
		name    string
		input   []byte
		wantErr error // nil: any error
	}{
		{"trailing byte", append(mustList(mustElem(valid0)), 0x00), rlp.ErrMoreThanOneValue},
		{"unsupported type", mustList(mustElem(valid0), unsupported), ErrTxTypeNotSupported},
		{"unsupported type after oversized", mustList(mustElem(typedV), unsupported), ErrTxTypeNotSupported},
		{"not a list", mustElem(valid0), nil},
		{"malformed", []byte{0xff, 0xff}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeTaikoTxList(tt.input)
			if err == nil {
				t.Fatalf("DecodeTaikoTxList decoded %d transactions, want an error", len(got))
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("DecodeTaikoTxList error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("DecodeTaikoTxList returned %d transactions with an error", len(got))
			}
			if err := rlp.DecodeBytes(tt.input, new(Transactions)); err == nil {
				t.Fatal("a whole-list decode accepts the input")
			}
		})
	}
}
