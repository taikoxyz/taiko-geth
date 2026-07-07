package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// CHANGE(taiko): Masaya is reset to activate Unzen from genesis. Unzen also
// carries the Cancun/Prague/Osaka timestamp fields because genesis becomes an
// Osaka-era block.
func TestTaikoGenesisBlock_MasayaActivatesUnzenAtGenesis(t *testing.T) {
	genesis := TaikoGenesisBlock(params.MasayaDevnetNetworkID.Uint64())
	cfg := genesis.Config
	if cfg.ChainID.Cmp(params.MasayaDevnetNetworkID) != 0 {
		t.Fatalf("ChainID = %v, want %v", cfg.ChainID, params.MasayaDevnetNetworkID)
	}
	if cfg.UnzenTime == nil {
		t.Fatal("UnzenTime is nil, want 0")
	}
	if got := *cfg.UnzenTime; got != 0 {
		t.Fatalf("UnzenTime = %d, want 0", got)
	}
	zeroBlock := big.NewInt(0)
	if !cfg.IsUnzen(0) {
		t.Fatal("Masaya Unzen fork is not active at timestamp 0")
	}
	if !cfg.IsCancun(zeroBlock, 0) {
		t.Fatal("Masaya Cancun fork is not active at genesis")
	}
	if !cfg.IsPrague(zeroBlock, 0) {
		t.Fatal("Masaya Prague fork is not active at genesis")
	}
	if !cfg.IsOsaka(zeroBlock, 0) {
		t.Fatal("Masaya Osaka fork is not active at genesis")
	}
	t.Logf("Masaya chain ID: %d", params.MasayaDevnetNetworkID.Uint64())
	t.Logf("Masaya genesis hash: %s", genesis.ToBlock().Hash())
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

func TestTaikoGenesisBlock_MasayaDoesNotContaminateMainnetForkTimes(t *testing.T) {
	TaikoGenesisBlock(params.MasayaDevnetNetworkID.Uint64())

	genesis := TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())
	cfg := genesis.Config

	const mainnetUnzenTime uint64 = 1_786_021_200 // 2026-08-06 13:00:00 UTC
	if cfg.UnzenTime == nil {
		t.Fatalf("UnzenTime is nil, want %d", mainnetUnzenTime)
	}
	if got := *cfg.UnzenTime; got != mainnetUnzenTime {
		t.Fatalf("UnzenTime = %d, want %d", got, mainnetUnzenTime)
	}
	// Masaya activates Cancun/Prague/Osaka at genesis; constructing it must not
	// leak those timestamps into the Mainnet config, which stays inactive until
	// the scheduled Unzen time.
	zeroBlock := big.NewInt(0)
	if cfg.IsCancun(zeroBlock, 0) {
		t.Fatal("Mainnet Cancun fork is active at genesis after constructing Masaya")
	}
	if cfg.IsPrague(zeroBlock, 0) {
		t.Fatal("Mainnet Prague fork is active at genesis after constructing Masaya")
	}
	if cfg.IsOsaka(zeroBlock, 0) {
		t.Fatal("Mainnet Osaka fork is active at genesis after constructing Masaya")
	}
}
