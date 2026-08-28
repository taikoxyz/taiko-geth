package core

import (
	"math/big"
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
