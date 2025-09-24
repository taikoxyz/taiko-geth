package misc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
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
	// If the current block is the first EIP-4396 block, return the ShastaInitialBaseFee.
	if parent.Number.Cmp(new(big.Int).Add(config.ShastaBlock, common.Big2)) <= 0 {
		return new(big.Int).SetUint64(params.ShastaInitialBaseFee)
	}

	var (
		blockTimeTarget              uint64 = 2
		maxGasTargetTargetPercentage        = 95
	)
	parentGasTarget := parent.GasLimit / config.ElasticityMultiplier()
	parentAdjustedGasTarget := min(
		parentGasTarget*parentBlockTime/blockTimeTarget,
		parent.GasLimit*uint64(maxGasTargetTargetPercentage)/100,
	)

	log.Info(
		"Calculating EIP-4396 baseFee",
		"parentBaseFee", parent.BaseFee,
		"parentGasUsed", parent.GasUsed,
		"parentGasTarget", parentGasTarget,
		"parentAdjustedGasTarget", parentAdjustedGasTarget,
		"parentBlockTime", parentBlockTime,
	)

	// If the parent gasUsed is the same as the adjusted target, the baseFee remains unchanged.
	if parent.GasUsed == parentAdjustedGasTarget {
		return new(big.Int).Set(parent.BaseFee)
	}

	var (
		num   = new(big.Int)
		denom = new(big.Int)
	)

	log.Info("parent.GasUsed != parentAdjustedGasTarget", "parent.GasUsed", parent.GasUsed, "parentAdjustedGasTarget", parentAdjustedGasTarget)

	if parent.GasUsed > parentAdjustedGasTarget {
		// If the parent block used more gas than its target, the baseFee should increase.
		// max(1, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num.SetUint64(parent.GasUsed - parentAdjustedGasTarget)
		log.Info("parent.GasUsed > parentAdjustedGasTarget", num)
		num.Mul(num, parent.BaseFee)
		log.Info("parent.GasUsed > parentAdjustedGasTarget2", num)
		num.Div(num, denom.SetUint64(parentGasTarget))
		log.Info("parent.GasUsed > parentAdjustedGasTarget3", num)
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		log.Info("parent.GasUsed > parentAdjustedGasTarget4", num)
		if num.Cmp(common.Big1) < 0 {
			log.Info("parent.GasUsed > parentAdjustedGasTarget5", num)
			return num.Add(parent.BaseFee, common.Big1)
		}
		log.Info("EIP-4396 baseFee increased", "increase", num)
		return num.Add(parent.BaseFee, num)
	} else {
		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
		// max(0, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num.SetUint64(parentAdjustedGasTarget - parent.GasUsed)
		log.Info("parent.GasUsed <= parentAdjustedGasTarget", num)
		num.Mul(num, parent.BaseFee)
		log.Info("parent.GasUsed <= parentAdjustedGasTarget2", num)
		num.Div(num, denom.SetUint64(parentGasTarget))
		log.Info("parent.GasUsed <= parentAdjustedGasTarget3", num)
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		log.Info("parent.GasUsed <= parentAdjustedGasTarget4", num)

		baseFee := num.Sub(parent.BaseFee, num)
		if baseFee.Cmp(common.Big0) < 0 {
			log.Info("parent.GasUsed <= parentAdjustedGasTarget5", num)
			baseFee = common.Big0
		}
		return baseFee
	}
}
