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

// TestIsPostMergeHeader pins that a Taiko header counts as post-merge whatever its
// difficulty (Unzen stores zk gas there), while other chains keep the upstream
// difficulty == 0 rule.
func TestIsPostMergeHeader(t *testing.T) {
	taiko := &ChainConfig{Taiko: true}
	eth := &ChainConfig{}
	if !taiko.IsPostMergeHeader(big.NewInt(12345)) {
		t.Fatal("taiko header with non-zero difficulty must be post-merge")
	}
	if !taiko.IsPostMergeHeader(common.Big0) {
		t.Fatal("taiko header with zero difficulty must be post-merge")
	}
	if eth.IsPostMergeHeader(big.NewInt(12345)) {
		t.Fatal("non-taiko header with non-zero difficulty must be pre-merge")
	}
	if !eth.IsPostMergeHeader(common.Big0) {
		t.Fatal("non-taiko header with zero difficulty must be post-merge")
	}
}
