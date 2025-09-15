package common

import "github.com/ethereum/go-ethereum/common/hexutil"

// BlockEnv The block environment.
type BlockEnv struct {
	// The number of ancestor blocks of this block (block height).
	Number *hexutil.Big `json:"number"`

	// Coinbase or miner or address that created and signed the block.
	// This is the receiver address of all the gas spent in the block.
	Coinbase Address `json:"coinbase"`

	// The timestamp of the block in seconds since the UNIX epoch.
	Timestamp *hexutil.Big `json:"timestamp"`

	// The gas limit of the block.
	GasLimit *hexutil.Big `json:"gas_limit"`

	// The base fee per gas, added in the London upgrade with [EIP-1559].
	// [EIP-1559]: https://eips.ethereum.org/EIPS/eip-1559
	BaseFee *hexutil.Big `json:"basefee"`

	// The difficulty of the block.
	///Unused after the Paris (AKA the merge) upgrade, and replaced by `prevrandao`.
	Difficulty *hexutil.Big `json:"difficulty"`

	// Change: this value is MixDigest. We just use PrevRandao to not change the struct
	PrevRandao *Hash `json:"prevrandao,omitempty"`

	///Excess blob gas and blob gasprice.
	// Incorporated as part of the Cancun upgrade via [EIP-4844].
	// [EIP-4844]: https://eips.ethereum.org/EIPS/eip-4844
	BlobExcessGasAndPrice *BlobExcessGasAndPrice `json:"blob_excess_gas_and_price,omitempty"`
}

// BlobExcessGasAndPrice holding block blob excess gas and it calculates blob fee.
// Incorporated as part of the Cancun upgrade via [EIP-4844].
//
// [EIP-4844]: https://eips.ethereum.org/EIPS/eip-4844
type BlobExcessGasAndPrice struct {
	ExcessGas *hexutil.Big `json:"excess_gas"`
	GasPrice  *hexutil.Big `json:"gas_price"`
}
