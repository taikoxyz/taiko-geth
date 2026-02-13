package misc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func TestCalcEIP4396BaseFee_MinClampTaikoMainnet(t *testing.T) {
	parent := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 20_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1_000_000),
	}
	config := &params.ChainConfig{ChainID: new(big.Int).Set(params.TaikoMainnetNetworkID)}

	have := CalcEIP4396BaseFee(config, parent, 2)
	want := big.NewInt(10_000_000)
	if have.Cmp(want) != 0 {
		t.Fatalf("unexpected base fee: have %s, want %s", have, want)
	}
}

func TestCalcEIP4396BaseFee_MinClampNonMainnetTaiko(t *testing.T) {
	parent := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 20_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1_000_000),
	}
	config := &params.ChainConfig{ChainID: new(big.Int).Set(params.TaikoHoodiNetworkID)}

	have := CalcEIP4396BaseFee(config, parent, 2)
	want := big.NewInt(5_000_000)
	if have.Cmp(want) != 0 {
		t.Fatalf("unexpected base fee: have %s, want %s", have, want)
	}
}

func TestCalcEIP4396BaseFee_MaxClampUnchanged(t *testing.T) {
	parent := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 20_000_000,
		GasUsed:  20_000_000,
		BaseFee:  big.NewInt(1_000_000_000),
	}

	tests := []struct {
		name   string
		chain  *big.Int
		expect *big.Int
	}{
		{
			name:   "taiko_mainnet",
			chain:  params.TaikoMainnetNetworkID,
			expect: big.NewInt(1_000_000_000),
		},
		{
			name:   "taiko_non_mainnet",
			chain:  params.TaikoHoodiNetworkID,
			expect: big.NewInt(1_000_000_000),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := &params.ChainConfig{ChainID: new(big.Int).Set(tc.chain)}
			have := CalcEIP4396BaseFee(config, parent, 2)
			if have.Cmp(tc.expect) != 0 {
				t.Fatalf("unexpected base fee: have %s, want %s", have, tc.expect)
			}
		})
	}
}
