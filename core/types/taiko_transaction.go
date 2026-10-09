package types

// MarkAsAnchor marks tx as a Taiko anchor transaction, which
// core.TransactionToMessage carries into the message it converts. Only a
// dynamic-fee transaction can be one; any other type returns
// ErrTxTypeNotSupported.
//
// It mutates tx, so call it only on a private copy: a block's transactions are
// shared with its cached copies and with concurrent readers. Block import,
// sealing, replay and tracing never mark a transaction; they flag the anchor's
// message (core.Message.IsAnchor) instead.
func (tx *Transaction) MarkAsAnchor() error {
	return tx.inner.markAsAnchor()
}

// IsAnchor reports whether MarkAsAnchor marked tx.
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
