package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// taikoStorageCollision reports whether a CREATE onto addr must fail with
// ErrContractAddressCollision because the account already has non-empty
// storage. Taiko chains keep this EIP-7610 storage rule, which upstream
// dropped; non-Taiko chains never read the storage root here.
func taikoStorageCollision(evm *EVM, addr common.Address) bool {
	if !evm.chainConfig.Taiko {
		return false
	}
	root := evm.StateDB.GetStorageRoot(addr)
	return root != (common.Hash{}) && root != types.EmptyRootHash
}
