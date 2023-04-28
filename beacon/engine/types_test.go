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
			common.HexToHash("0x3c9f08cf64c28ef038bdc06f1e2f417ff850e2c1d1bda29a187a04062ca8dcd2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcWithdrawalsRootTaiko(tt.withdrawals)
			assert.Equal(t, tt.want, got)
		})
	}
}
