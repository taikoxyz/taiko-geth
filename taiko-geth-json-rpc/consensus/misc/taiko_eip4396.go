package misc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// The number of blocks after the Shasta hardfork where the initial base fee is used.
// This is set to 3 since if the first Shasta block is genesis block, its timestamp may
// will be very different from the second block, causing large base fee change.
const ShastaInitialBaseFeeBlocks uint64 = 3

// EIP-4396 calculation parameters.
const blockTimeTarget uint64 = 2
const maxGasTargetTargetPercentage uint64 = 95

// VerifyEIP4396Header verifies some header attributes which were changed in EIP-4396,
func VerifyEIP4396Header(
	config *params.ChainConfig,
	parent *types.Header,
	parentBlockTime uint64,
	header *types.Header,
) error {
	// Verify the header is not malformed
	if header.BaseFee == nil {
		return errors.New("header is missing baseFee")
	}
	// Verify the baseFee is correct based on the parent header.
	expectedBaseFee := CalcEIP4396BaseFee(config, parent, parentBlockTime)
	if header.BaseFee.Cmp(expectedBaseFee) != 0 {
		return fmt.Errorf("invalid baseFee: have %s, want %s, parentBaseFee %s, parentGasUsed %d, parentBlockTime %d",
			header.BaseFee, expectedBaseFee, parent.BaseFee, parent.GasUsed, parentBlockTime)
	}
	return nil
}

// CalcEIP4396BaseFee calculates the EIP-4396 basefee of the header.
func CalcEIP4396BaseFee(config *params.ChainConfig, parent *types.Header, parentBlockTime uint64) *big.Int {
	// If the current block is one of the first three EIP-4396 blocks, return the ShastaInitialBaseFee.
	if parent.Number.Uint64()+1 < config.ShastaBlock.Uint64()+ShastaInitialBaseFeeBlocks {
		return new(big.Int).SetUint64(params.ShastaInitialBaseFee)
	}

	parentGasTarget := parent.GasLimit / config.ElasticityMultiplier()
	parentAdjustedGasTarget := min(
		parentGasTarget*parentBlockTime/blockTimeTarget,
		parent.GasLimit*maxGasTargetTargetPercentage/100,
	)

	// If the parent gasUsed is the same as the adjusted target, the baseFee remains unchanged.
	if parent.GasUsed == parentAdjustedGasTarget {
		return new(big.Int).Set(parent.BaseFee)
	}

	var (
		num   = new(big.Int)
		denom = new(big.Int)
	)

	if parent.GasUsed > parentAdjustedGasTarget {
		// If the parent block used more gas than its target, the baseFee should increase.
		// max(1, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num.SetUint64(parent.GasUsed - parentAdjustedGasTarget)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		if num.Cmp(common.Big1) < 0 {
			return num.Add(parent.BaseFee, common.Big1)
		}
		return num.Add(parent.BaseFee, num)
	} else {
		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
		// max(0, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num.SetUint64(parentAdjustedGasTarget - parent.GasUsed)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))

		baseFee := num.Sub(parent.BaseFee, num)
		if baseFee.Cmp(common.Big0) < 0 {
			baseFee = common.Big0
		}
		return baseFee
	}
}
