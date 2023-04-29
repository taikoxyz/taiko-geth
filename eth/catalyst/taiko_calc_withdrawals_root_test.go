package catalyst

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
					Address: common.HexToAddress("0x388C818CA8B9251b393131C08a736A67ccB19297"),
					Amount:  100000000,
				},
				{
					Address: common.HexToAddress("0xeEE27662c2B8EBa3CD936A23F039F3189633e4C8"),
					Amount:  184938493,
				},
			},
			common.HexToHash("0xc3f16b87d5d286399c3d4daa4e7e3ae75840d66d0560863a2bdb4eb1bfaff229"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcWithdrawalsRootTaiko(tt.withdrawals)
			assert.Equal(t, tt.want, got)
		})
	}
}
