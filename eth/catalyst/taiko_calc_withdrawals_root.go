package catalyst

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// CHANGE(taiko): calc withdrawals root by hashing deposits with keccak256
func calcWithdrawalsRootTaiko(withdrawals []*types.Withdrawal) common.Hash {
	if len(withdrawals) == 0 {
		return types.EmptyWithdrawalsHash
	}
	var result []byte
	for _, withdrawal := range withdrawals {
		amountBytes := new(big.Int).SetUint64(withdrawal.Amount).Bytes()
		paddedAmountBytes := make([]byte, 12)
		copy(paddedAmountBytes[12-len(amountBytes):], amountBytes)

		result = append(result, withdrawal.Address.Bytes()...)
		result = append(result, paddedAmountBytes...)
	}

	return crypto.Keccak256Hash(result)
}
