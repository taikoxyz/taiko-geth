package core

import (
	"math/big"

	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// Before Etna the first transaction of a Taiko block is its anchor
// transaction, which executes with the exemptions Message.IsAnchor grants: no
// gas purchase, fee-cap check or refund, and no base-fee redistribution. The
// flag is set on the message converted for one execution, never on the
// transaction: a block's transactions are shared with its cached copies and
// with concurrent readers such as the state prefetcher.

// ValidateAnchorTxType returns types.ErrTxTypeNotSupported unless tx has a
// type an anchor transaction can have: only a dynamic-fee transaction can be
// one.
func ValidateAnchorTxType(tx *types.Transaction) error {
	if tx.Type() != types.DynamicFeeTxType {
		return types.ErrTxTypeNotSupported
	}
	return nil
}

// AnchorTransactionToMessage converts the anchor transaction tx into a message
// like TransactionToMessage and flags the message as the anchor, leaving tx
// untouched. A transaction that cannot be an anchor returns
// types.ErrTxTypeNotSupported and no message. A sender that cannot be
// recovered returns its error together with the flagged message, as
// TransactionToMessage does.
func AnchorTransactionToMessage(tx *types.Transaction, s types.Signer, baseFee *big.Int) (*Message, error) {
	if err := ValidateAnchorTxType(tx); err != nil {
		return nil, err
	}
	msg, err := TransactionToMessage(tx, s, baseFee)
	msg.IsAnchor = true
	return msg, err
}

// ApplyAnchorTransaction applies the anchor transaction tx like
// ApplyTransaction, with its message flagged as the anchor. The anchor's base
// fee is never redistributed, so the base-fee share ApplyTransaction decodes
// does not apply to it.
func ApplyAnchorTransaction(evm *vm.EVM, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction) (*types.Receipt, error) {
	msg, err := AnchorTransactionToMessage(tx, types.MakeSigner(evm.ChainConfig(), header.Number, header.Time), header.BaseFee)
	if err != nil {
		return nil, err
	}
	return ApplyTransactionWithEVM(msg, gp, statedb, header.Number, header.Hash(), header.Time, tx, evm)
}
