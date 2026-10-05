package miner

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// etnaTxTypes are the transaction types of the shared execution transaction
// grammar, in the order an untyped payload list is matched against them.
var etnaTxTypes = []byte{
	types.LegacyTxType,
	types.AccessListTxType,
	types.DynamicFeeTxType,
	types.BlobTxType,
	types.SetCodeTxType,
}

// decodeEtnaTxList decodes the transaction list of an Etna block with the
// shared execution transaction grammar.
//
// Only the first RLP value of txList is the list, and any bytes after it are
// ignored. The list items are read back to back, and each one is either:
//   - an RLP list, which is a transaction payload without a type byte. It
//     decodes as the first of the legacy, access list, dynamic fee, blob and
//     set code payloads it matches, so a typed payload needs no type byte;
//   - a type byte followed by the RLP payload list of that type. The type byte
//     is either bare or preceded by an RLP string header. The length a string
//     header declares must fit in the remaining list bytes and is otherwise
//     ignored: the next item starts right after the payload list. Type 0 is a
//     legacy payload.
//
// One item outside the grammar makes the whole list undecodable.
func decodeEtnaTxList(txList []byte) (types.Transactions, error) {
	kind, items, _, err := rlp.Split(txList)
	if err != nil {
		return nil, err
	}
	if kind != rlp.List {
		return nil, errors.New("transaction list is not an RLP list")
	}
	var txs types.Transactions
	for len(items) > 0 {
		tx, rest, err := decodeEtnaTxListItem(items)
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", len(txs), err)
		}
		txs = append(txs, tx)
		items = rest
	}
	return txs, nil
}

// decodeEtnaTxListItem decodes the transaction list item at the start of b
// and returns the transaction and the bytes after the item.
func decodeEtnaTxListItem(b []byte) (*types.Transaction, []byte, error) {
	kind, content, rest, err := rlp.Split(b)
	if err != nil {
		return nil, nil, err
	}
	var typed []byte // the type byte, the payload list and everything after it
	switch kind {
	case rlp.List:
		payload := b[:len(b)-len(rest)]
		for _, txType := range etnaTxTypes {
			if tx, err := decodeEtnaTxPayload(txType, payload); err == nil {
				return tx, rest, nil
			}
		}
		return nil, nil, errors.New("transaction payload matches no transaction type")
	case rlp.Byte:
		typed = b
	default:
		typed = b[len(b)-len(content)-len(rest):]
	}
	if len(typed) == 0 {
		return nil, nil, errors.New("missing transaction type")
	}
	kind, _, rest, err = rlp.Split(typed[1:])
	if err != nil {
		return nil, nil, err
	}
	if kind != rlp.List {
		return nil, nil, errors.New("transaction payload is not an RLP list")
	}
	tx, err := decodeEtnaTxPayload(typed[0], typed[1:len(typed)-len(rest)])
	if err != nil {
		return nil, nil, err
	}
	return tx, rest, nil
}

// decodeEtnaTxPayload decodes payload, one RLP list, as the signed payload of
// a transaction of the given type and checks it against the grammar.
func decodeEtnaTxPayload(txType byte, payload []byte) (*types.Transaction, error) {
	var enc []byte
	switch txType {
	case types.LegacyTxType:
		enc = payload
	case types.AccessListTxType, types.DynamicFeeTxType, types.BlobTxType, types.SetCodeTxType:
		enc = append([]byte{txType}, payload...)
	default:
		return nil, fmt.Errorf("unsupported transaction type %d", txType)
	}
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(enc); err != nil {
		return nil, err
	}
	if err := validateEtnaTxEncoding(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// validateEtnaTxEncoding checks a decoded transaction against the shared
// execution transaction grammar:
//   - its type is legacy, access list, dynamic fee, blob or set code;
//   - maxFeePerGas and maxPriorityFeePerGas (the gas price of legacy and
//     access list transactions) fit in 128 bits, and the value and the
//     signature r and s fit in 256 bits, none of them negative;
//   - a legacy v is 27, 28, or an EIP-155 v whose chain ID fits in 64 bits;
//   - a typed transaction has a chain ID that fits in 64 bits and a y-parity
//     of 0 or 1;
//   - a blob transaction has a maxFeePerBlobGas that fits in 128 bits and
//     carries no blob sidecar.
//
// Signer recovery and execution validity are not part of the grammar: block
// building skips a transaction that fails them on its own.
func validateEtnaTxEncoding(tx *types.Transaction) error {
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType,
		types.BlobTxType, types.SetCodeTxType:
	default:
		return fmt.Errorf("unsupported transaction type %d", tx.Type())
	}
	for _, field := range []struct {
		name  string
		value *big.Int
		bits  int
	}{
		{"maxFeePerGas", tx.GasFeeCap(), 128},
		{"maxPriorityFeePerGas", tx.GasTipCap(), 128},
		{"value", tx.Value(), 256},
	} {
		if field.value.Sign() < 0 || field.value.BitLen() > field.bits {
			return fmt.Errorf("%s exceeds uint%d", field.name, field.bits)
		}
	}
	v, r, s := tx.RawSignatureValues()
	if r.Sign() < 0 || r.BitLen() > 256 || s.Sign() < 0 || s.BitLen() > 256 {
		return errors.New("signature scalar exceeds uint256")
	}
	if tx.Type() == types.LegacyTxType {
		if v.Cmp(big.NewInt(27)) == 0 || v.Cmp(big.NewInt(28)) == 0 {
			return nil
		}
		if v.Cmp(big.NewInt(35)) < 0 {
			return errors.New("invalid legacy signature v")
		}
		chainID := new(big.Int).Rsh(new(big.Int).Sub(v, big.NewInt(35)), 1)
		if !chainID.IsUint64() {
			return errors.New("legacy chain ID exceeds uint64")
		}
		return nil
	}
	if !tx.ChainId().IsUint64() {
		return errors.New("chain ID exceeds uint64")
	}
	if v.Sign() < 0 || v.BitLen() > 1 {
		return errors.New("invalid typed transaction parity")
	}
	if tx.Type() == types.BlobTxType {
		if fee := tx.BlobGasFeeCap(); fee.Sign() < 0 || fee.BitLen() > 128 {
			return errors.New("maxFeePerBlobGas exceeds uint128")
		}
		if tx.BlobTxSidecar() != nil {
			return errors.New("blob sidecar wrapper is not an execution transaction")
		}
	}
	return nil
}
