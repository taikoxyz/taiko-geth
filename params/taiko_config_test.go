package params

import (
	"math/big"
	"testing"
)

func TestNetworkIDToChainConfigOrDefault(t *testing.T) {
	tests := []struct {
		name            string
		networkID       *big.Int
		wantChainConfig *ChainConfig
	}{
		{
			name:            "taikoMainnetNetworkID",
			networkID:       TaikoMainnetNetworkID,
			wantChainConfig: TaikoChainConfig,
		},
		{
			name:            "taikoInternalNetworkId",
			networkID:       TaikoInternalNetworkID,
			wantChainConfig: TaikoChainConfig,
		},
		// Taiko network aliases must all resolve to the shared Taiko chain config.
		{
			name:            "taikoHoodiNetworkID",
			networkID:       TaikoHoodiNetworkID,
			wantChainConfig: TaikoChainConfig,
		},
		{
			name:            "masayaDevnetNetworkID",
			networkID:       MasayaDevnetNetworkID,
			wantChainConfig: TaikoChainConfig,
		},
		{
			name:            "mainnet",
			networkID:       MainnetChainConfig.ChainID,
			wantChainConfig: MainnetChainConfig,
		},
		{
			name:            "sepolia",
			networkID:       SepoliaChainConfig.ChainID,
			wantChainConfig: SepoliaChainConfig,
		},
		{
			name:            "doesntExist",
			networkID:       big.NewInt(89390218390),
			wantChainConfig: AllEthashProtocolChanges,
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

func TestTaikoChainConfigPreservesShanghaiOnlyExecution(t *testing.T) {
	if TaikoChainConfig.ShanghaiTime == nil || *TaikoChainConfig.ShanghaiTime != 0 {
		t.Fatalf("expected ShanghaiTime=0, got %v", TaikoChainConfig.ShanghaiTime)
	}
	if TaikoChainConfig.CancunTime != nil {
		t.Fatalf("expected CancunTime=nil, got %v", *TaikoChainConfig.CancunTime)
	}
	if TaikoChainConfig.PragueTime != nil {
		t.Fatalf("expected PragueTime=nil, got %v", *TaikoChainConfig.PragueTime)
	}
	if TaikoChainConfig.OsakaTime != nil {
		t.Fatalf("expected OsakaTime=nil, got %v", *TaikoChainConfig.OsakaTime)
	}
	if TaikoChainConfig.VerkleTime != nil {
		t.Fatalf("expected VerkleTime=nil, got %v", *TaikoChainConfig.VerkleTime)
	}
	if !TaikoChainConfig.Taiko {
		t.Fatal("expected Taiko chain flag to remain enabled")
	}
}
