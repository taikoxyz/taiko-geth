package types

func (tx *Transaction) MarkAsAnchor() error {
	return tx.inner.markAsAnchor()
}

func (tx *Transaction) IsAnchor() bool {
	return tx.inner.isAnchor()
}

func (tx *DynamicFeeTx) isAnchor() bool {
	return tx.anchor
}

func (tx *LegacyTx) isAnchor() bool {
	return tx.anchor
}

func (tx *AccessListTx) isAnchor() bool {
	return false
}

func (tx *BlobTx) isAnchor() bool {
	return false
}

func (tx *DynamicFeeTx) markAsAnchor() error {
	tx.anchor = true
	return nil
}

func (tx *LegacyTx) markAsAnchor() error {
	tx.anchor = true
	return nil
}

func (tx *AccessListTx) markAsAnchor() error {
	return ErrInvalidTxType
}

func (tx *BlobTx) markAsAnchor() error {
	return ErrInvalidTxType
}
