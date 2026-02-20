package misc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// EIP-4396 calculation parameters.
const blockTimeTarget uint64 = 2
const maxGasTargetTargetPercentage uint64 = 95

// Min and Max base fee for Shasta blocks.
var (
	minBaseFeeShastaDefault = new(big.Int).SetUint64(5_000_000)     // 0.005 Gwei
	minBaseFeeShastaMainnet = new(big.Int).SetUint64(10_000_000)    // 0.01 Gwei
	maxBaseFeeShasta        = new(big.Int).SetUint64(1_000_000_000) // 1 Gwei
)

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
	// If the parent is genesis, use the initial base fee for the first post-genesis block.
	if parent.Number.Cmp(common.Big0) == 0 {
		return new(big.Int).SetUint64(params.ShastaInitialBaseFee)
	}

	parentGasTarget := parent.GasLimit / config.ElasticityMultiplier()
	parentAdjustedGasTarget := min(
		parentGasTarget*parentBlockTime/blockTimeTarget,
		parent.GasLimit*maxGasTargetTargetPercentage/100,
	)

	// If the parent gasUsed is the same as the adjusted target, the baseFee remains unchanged.
	if parent.GasUsed == parentAdjustedGasTarget {
		return parent.BaseFee
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
			return clampEIP4396BaseFeeShasta(config, num.Add(parent.BaseFee, common.Big1))
		}
		return clampEIP4396BaseFeeShasta(config, num.Add(parent.BaseFee, num))
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
		return clampEIP4396BaseFeeShasta(config, baseFee)
	}
}

// clampEIP4396BaseFeeShasta clamps the base fee to be within the min and max limits for Shasta blocks.
func clampEIP4396BaseFeeShasta(config *params.ChainConfig, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return nil
	}
	minBaseFee := minBaseFeeShastaDefault
	if isTaikoMainnet(config) {
		minBaseFee = minBaseFeeShastaMainnet
	}
	if baseFee.Cmp(minBaseFee) < 0 {
		return minBaseFee
	}
	if baseFee.Cmp(maxBaseFeeShasta) > 0 {
		return maxBaseFeeShasta
	}
	return baseFee
}

// isTaikoMainnet checks if the chain config corresponds to Taiko Mainnet.
func isTaikoMainnet(config *params.ChainConfig) bool {
	return config != nil &&
		config.ChainID != nil &&
		config.ChainID.Cmp(params.TaikoMainnetNetworkID) == 0
}
