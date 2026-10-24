package types

import (
	"bytes"
	"errors"

	"github.com/ethereum/go-ethereum/rlp"
)

// DecodeTaikoTxList decodes an RLP list of transactions as a whole-list
// rlp.DecodeBytes into Transactions did before decoding began rejecting chain
// IDs and signature values wider than 256 bits. Such a transaction decodes
// without that check and keeps its list position, so a caller that assigns
// roles by position (the anchor at index 0) and drops transactions whose
// sender cannot be recovered builds the same block as before. Any other
// decoding error fails the whole list.
func DecodeTaikoTxList(b []byte) (Transactions, error) {
	r := bytes.NewReader(b)
	s := rlp.NewStream(r, uint64(len(b)))
	if _, err := s.List(); err != nil {
		return nil, err
	}
	txs := Transactions{}
	for {
		elem, err := s.Raw()
		if errors.Is(err, rlp.EOL) {
			break
		} else if err != nil {
			return nil, err
		}
		tx := new(Transaction)
		if err := rlp.DecodeBytes(elem, tx); err != nil {
			if !errors.Is(err, errOversizedSigValue) {
				return nil, err
			}
			if err := tx.decodeUnvalidated(elem); err != nil {
				return nil, err
			}
		}
		txs = append(txs, tx)
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	if r.Len() > 0 {
		return nil, rlp.ErrMoreThanOneValue
	}
	return txs, nil
}

// decodeUnvalidated decodes one RLP-encoded transaction like DecodeRLP, but
// without validateDecoded, recording the same size DecodeRLP would.
func (tx *Transaction) decodeUnvalidated(elem []byte) error {
	kind, content, rest, err := rlp.Split(elem)
	switch {
	case err != nil:
		return err
	case len(rest) > 0:
		return rlp.ErrMoreThanOneValue
	case kind == rlp.List:
		var inner LegacyTx
		if err := rlp.DecodeBytes(elem, &inner); err != nil {
			return err
		}
		tx.setDecoded(&inner, rlp.ListSize(uint64(len(content))))
		return nil
	case kind == rlp.Byte || len(content) <= 1:
		return errShortTypedTx
	}
	// The type switch of decodeTyped.
	var inner TxData
	switch content[0] {
	case AccessListTxType:
		inner = new(AccessListTx)
	case DynamicFeeTxType:
		inner = new(DynamicFeeTx)
	case BlobTxType:
		inner = new(BlobTx)
	case SetCodeTxType:
		inner = new(SetCodeTx)
	default:
		return ErrTxTypeNotSupported
	}
	if err := inner.decode(content[1:]); err != nil {
		return err
	}
	tx.setDecoded(inner, uint64(len(content)))
	return nil
}

func (tx *Transaction) MarkAsAnchor() error {
	return tx.inner.markAsAnchor()
}

func (tx *Transaction) IsAnchor() bool {
	return tx.inner.isAnchorTx()
}

func (tx *DynamicFeeTx) isAnchorTx() bool {
	return tx.isAnchor
}

func (tx *LegacyTx) isAnchorTx() bool {
	return false
}

func (tx *AccessListTx) isAnchorTx() bool {
	return false
}

func (tx *BlobTx) isAnchorTx() bool {
	return false
}

func (tx *SetCodeTx) isAnchorTx() bool {
	return false
}

func (tx *DynamicFeeTx) markAsAnchor() error {
	tx.isAnchor = true
	return nil
}

func (tx *LegacyTx) markAsAnchor() error {
	return ErrTxTypeNotSupported
}

func (tx *AccessListTx) markAsAnchor() error {
	return ErrTxTypeNotSupported
}

func (tx *BlobTx) markAsAnchor() error {
	return ErrTxTypeNotSupported
}

func (tx *SetCodeTx) markAsAnchor() error {
	return ErrTxTypeNotSupported
}
