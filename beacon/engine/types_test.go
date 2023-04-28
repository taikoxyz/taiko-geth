package engine

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

func Test_calcWithdrawalsRootTaiko(t *testing.T) {
	tests := []struct {
		name        string
		withdrawals []*types.Withdrawal
		want        common.Hash
	}{
		{
			"empty",
			nil,
			types.EmptyWithdrawalsHash,
		},
		{
			"withWithdrawals",
			[]*types.Withdrawal{
				{
					Address: common.HexToAddress("0xDAFEA492D9c6733ae3d56b7Ed1ADB60692c98Bc5"),
					Amount:  1000000000,
				},
				{
					Address: common.HexToAddress("0xeEE27662c2B8EBa3CD936A23F039F3189633e4C8"),
					Amount:  184938493,
				},
			},
			// TODO: this is not the correct hash to be getting i dont believe
			common.HexToHash("0x725fc75831c1a5f07cd11e9b3b1b36ccd42d18ed66228e04a71496f90f39decb"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcWithdrawalsRootTaiko(tt.withdrawals)
			assert.Equal(t, tt.want, got)
		})
	}
}
