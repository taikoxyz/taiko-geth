package core

import (
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// CHANGE(taiko): Retired network IDs use the internal-devnet fallback.
func TestTaikoGenesisBlock_RemovedNetworkDefaultsToInternal(t *testing.T) {
	const removedNetworkID uint64 = 167_011

	cfg := TaikoGenesisBlock(removedNetworkID).Config
	if cfg.ChainID.Cmp(big.NewInt(167_001)) != 0 {
		t.Fatalf("ChainID = %v, want 167001", cfg.ChainID)
	}
}

func TestTaikoGenesisBlock_HoodiActivatesUnzenAtScheduledTime(t *testing.T) {
	genesis := TaikoGenesisBlock(params.TaikoHoodiNetworkID.Uint64())
	cfg := genesis.Config
	if cfg.ChainID.Cmp(params.TaikoHoodiNetworkID) != 0 {
		t.Fatalf("ChainID = %v, want %v", cfg.ChainID, params.TaikoHoodiNetworkID)
	}

	const hoodiUnzenTime uint64 = 1_781_787_600
	if cfg.UnzenTime == nil {
		t.Fatalf("UnzenTime is nil, want %d", hoodiUnzenTime)
	}
	if got := *cfg.UnzenTime; got != hoodiUnzenTime {
		t.Fatalf("UnzenTime = %d, want %d", got, hoodiUnzenTime)
	}

	zeroBlock := big.NewInt(0)
	before := hoodiUnzenTime - 1
	if cfg.IsUnzen(before) {
		t.Fatalf("Hoodi Unzen fork is active at timestamp %d", before)
	}
	if !cfg.IsUnzen(hoodiUnzenTime) {
		t.Fatalf("Hoodi Unzen fork is not active at timestamp %d", hoodiUnzenTime)
	}
	if cfg.IsCancun(zeroBlock, before) {
		t.Fatalf("Hoodi Cancun fork is active at timestamp %d", before)
	}
	if !cfg.IsCancun(zeroBlock, hoodiUnzenTime) {
		t.Fatalf("Hoodi Cancun fork is not active at timestamp %d", hoodiUnzenTime)
	}
	if cfg.IsPrague(zeroBlock, before) {
		t.Fatalf("Hoodi Prague fork is active at timestamp %d", before)
	}
	if !cfg.IsPrague(zeroBlock, hoodiUnzenTime) {
		t.Fatalf("Hoodi Prague fork is not active at timestamp %d", hoodiUnzenTime)
	}
	if cfg.IsOsaka(zeroBlock, before) {
		t.Fatalf("Hoodi Osaka fork is active at timestamp %d", before)
	}
	if !cfg.IsOsaka(zeroBlock, hoodiUnzenTime) {
		t.Fatalf("Hoodi Osaka fork is not active at timestamp %d", hoodiUnzenTime)
	}
	t.Logf("Taiko Hoodi chain ID: %d", params.TaikoHoodiNetworkID.Uint64())
	t.Logf("Taiko Hoodi genesis hash: %s", genesis.ToBlock().Hash())
}

func TestTaikoGenesisBlock_MainnetActivatesUnzenAtScheduledTime(t *testing.T) {
	genesis := TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())
	cfg := genesis.Config
	if cfg.ChainID.Cmp(params.TaikoMainnetNetworkID) != 0 {
		t.Fatalf("ChainID = %v, want %v", cfg.ChainID, params.TaikoMainnetNetworkID)
	}

	const mainnetUnzenTime uint64 = 1_786_021_200 // 2026-08-06 13:00:00 UTC
	if cfg.UnzenTime == nil {
		t.Fatalf("UnzenTime is nil, want %d", mainnetUnzenTime)
	}
	if got := *cfg.UnzenTime; got != mainnetUnzenTime {
		t.Fatalf("UnzenTime = %d, want %d", got, mainnetUnzenTime)
	}

	zeroBlock := big.NewInt(0)
	before := mainnetUnzenTime - 1
	if cfg.IsUnzen(before) {
		t.Fatalf("Mainnet Unzen fork is active at timestamp %d", before)
	}
	if !cfg.IsUnzen(mainnetUnzenTime) {
		t.Fatalf("Mainnet Unzen fork is not active at timestamp %d", mainnetUnzenTime)
	}
	if cfg.IsCancun(zeroBlock, before) {
		t.Fatalf("Mainnet Cancun fork is active at timestamp %d", before)
	}
	if !cfg.IsCancun(zeroBlock, mainnetUnzenTime) {
		t.Fatalf("Mainnet Cancun fork is not active at timestamp %d", mainnetUnzenTime)
	}
	if cfg.IsPrague(zeroBlock, before) {
		t.Fatalf("Mainnet Prague fork is active at timestamp %d", before)
	}
	if !cfg.IsPrague(zeroBlock, mainnetUnzenTime) {
		t.Fatalf("Mainnet Prague fork is not active at timestamp %d", mainnetUnzenTime)
	}
	if cfg.IsOsaka(zeroBlock, before) {
		t.Fatalf("Mainnet Osaka fork is active at timestamp %d", before)
	}
	if !cfg.IsOsaka(zeroBlock, mainnetUnzenTime) {
		t.Fatalf("Mainnet Osaka fork is not active at timestamp %d", mainnetUnzenTime)
	}
}

// CHANGE(taiko): Network-specific configs must remain isolated between calls.
func TestTaikoGenesisBlock_ConfigIsIsolatedPerCall(t *testing.T) {
	hoodi := TaikoGenesisBlock(params.TaikoHoodiNetworkID.Uint64())
	TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())

	if hoodi.Config.ChainID.Cmp(params.TaikoHoodiNetworkID) != 0 {
		t.Fatalf("Hoodi ChainID = %v, want %v", hoodi.Config.ChainID, params.TaikoHoodiNetworkID)
	}
	if hoodi.Config.UnzenTime == nil {
		t.Fatalf("Hoodi UnzenTime is nil, want %d", HoodiUnzenTime)
	}
	if got := *hoodi.Config.UnzenTime; got != HoodiUnzenTime {
		t.Fatalf("Hoodi UnzenTime = %d, want %d", got, HoodiUnzenTime)
	}
}

// CHANGE(taiko): Etna is unscheduled on public networks, active from genesis on
// the internal devnet, and never activates before Unzen.
func TestTaikoGenesisBlock_EtnaSchedule(t *testing.T) {
	cases := []struct {
		name      string
		networkID uint64
		want      uint64
	}{
		{"mainnet", params.TaikoMainnetNetworkID.Uint64(), math.MaxUint64},
		{"hoodi", params.TaikoHoodiNetworkID.Uint64(), math.MaxUint64},
		{"internal", params.TaikoInternalNetworkID.Uint64(), 0},
	}
	for _, c := range cases {
		cfg := TaikoGenesisBlock(c.networkID).Config
		if cfg.EtnaTime == nil {
			t.Fatalf("%s EtnaTime is nil, want %d", c.name, c.want)
		}
		if got := *cfg.EtnaTime; got != c.want {
			t.Fatalf("%s EtnaTime = %d, want %d", c.name, got, c.want)
		}
		if cfg.UnzenTime == nil || *cfg.EtnaTime < *cfg.UnzenTime {
			t.Fatalf("%s EtnaTime %d precedes UnzenTime %v", c.name, *cfg.EtnaTime, cfg.UnzenTime)
		}
	}
}

// CHANGE(taiko): TaikoChainConfig must return the genesis chain config of every
// network, including the internal-devnet fallback for unknown network IDs.
func TestTaikoChainConfig_MatchesGenesisConfig(t *testing.T) {
	cases := []struct {
		name      string
		networkID uint64
	}{
		{"mainnet", params.TaikoMainnetNetworkID.Uint64()},
		{"hoodi", params.TaikoHoodiNetworkID.Uint64()},
		{"internal", params.TaikoInternalNetworkID.Uint64()},
		{"unknown", 167_011},
	}
	for _, c := range cases {
		got := TaikoChainConfig(c.networkID)
		want := TaikoGenesisBlock(c.networkID).Config
		if !reflect.DeepEqual(got, want) {
			// ChainConfig.String omits the Taiko fork fields, so dump JSON instead.
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			t.Fatalf("%s TaikoChainConfig = %s, want %s", c.name, gotJSON, wantJSON)
		}
	}
}

// CHANGE(taiko): Fork-time variable overrides made after TaikoChainConfig
// returns must still apply to the returned config.
func TestTaikoChainConfig_ForkTimeOverrideAppliesAfterConstruction(t *testing.T) {
	cfg := TaikoChainConfig(params.TaikoInternalNetworkID.Uint64())

	original := DevnetEtnaTime
	t.Cleanup(func() { DevnetEtnaTime = original })
	DevnetEtnaTime = 100

	if cfg.IsEtna(99) || !cfg.IsEtna(100) {
		t.Fatalf("IsEtna(99) = %t, IsEtna(100) = %t, want false and true", cfg.IsEtna(99), cfg.IsEtna(100))
	}
}

// CHANGE(taiko): Mutating one TaikoChainConfig result must not affect another.
func TestTaikoChainConfig_ConfigIsIsolatedPerCall(t *testing.T) {
	first := TaikoChainConfig(params.TaikoHoodiNetworkID.Uint64())
	first.ChainID = big.NewInt(1) // Replace the field; never mutate the shared big.Int.

	second := TaikoChainConfig(params.TaikoHoodiNetworkID.Uint64())
	if second.ChainID.Cmp(params.TaikoHoodiNetworkID) != 0 {
		t.Fatalf("second ChainID = %v, want %v", second.ChainID, params.TaikoHoodiNetworkID)
	}
}
