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
					Address: common.HexToAddress("0x388C818CA8B9251b393131C08a736A67ccB19297"),
					Amount:  100000000,
				},
				{
					Address: common.HexToAddress("0xeEE27662c2B8EBa3CD936A23F039F3189633e4C8"),
					Amount:  184938493,
				},
			},
			// TODO: this is not the correct hash to be getting i dont believe
			common.HexToHash("0xba0fecdc368edfa83ce965a3c92e57418bbd710dfa5e55ac14404a58952729ad"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calcWithdrawalsRootTaiko(tt.withdrawals)
			assert.Nil(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
