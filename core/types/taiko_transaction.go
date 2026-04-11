package types

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
