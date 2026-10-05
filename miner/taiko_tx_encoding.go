package miner

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// decodeEtnaTxList decodes the transaction list of an Etna block with the
// shared execution transaction grammar. Only the first RLP value of txList is
// the list, and any bytes after it are ignored. One transaction outside the
// grammar makes the whole list undecodable.
func decodeEtnaTxList(txList []byte) (types.Transactions, error) {
	var txs types.Transactions
	if err := rlp.NewStream(bytes.NewReader(txList), uint64(len(txList))).Decode(&txs); err != nil {
		return nil, err
	}
	for i, tx := range txs {
		if err := validateEtnaTxEncoding(tx); err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
	}
	return txs, nil
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
