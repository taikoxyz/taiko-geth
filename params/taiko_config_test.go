package params

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNetworkIDToChainConfigOrDefault(t *testing.T) {
	tests := []struct {
		name            string
		networkID       *big.Int
		wantChainConfig *ChainConfig
	}{
		{
			"taikoMainnetNetworkID",
			TaikoMainnetNetworkID,
			TaikoChainConfig,
		},
		{
			"taikoInternalNetworkId",
			TaikoInternalNetworkID,
			TaikoChainConfig,
		},
		{
			"mainnet",
			MainnetChainConfig.ChainID,
			MainnetChainConfig,
		},
		{
			"sepolia",
			SepoliaChainConfig.ChainID,
			SepoliaChainConfig,
		},
		{
			"doesntExist",
			big.NewInt(89390218390),
			AllEthashProtocolChanges,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if config := NetworkIDToChainConfigOrDefault(tt.networkID); config != tt.wantChainConfig {
				t.Fatalf("expected %v, got %v", config, tt.wantChainConfig)
			}
		})
	}
}

func TestIsUzenTimestampFork(t *testing.T) {
	cfg := *TaikoChainConfig
	uzenTime := uint64(1_780_000_000)
	cfg.UzenTime = &uzenTime

	if cfg.IsUzen(uzenTime - 1) {
		t.Fatal("expected Uzen to be inactive before UzenTime")
	}
	if !cfg.IsUzen(uzenTime) {
		t.Fatal("expected Uzen to activate exactly at UzenTime")
	}
	if !cfg.Rules(common.Big1, true, uzenTime).IsUzen {
		t.Fatal("expected Rules.IsUzen to mirror ChainConfig.IsUzen")
	}
}
