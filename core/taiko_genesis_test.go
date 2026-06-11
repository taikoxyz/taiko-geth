package core

import (
	"math"
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

func TestTaikoGenesisBlock_MasayaDoesNotContaminateMainnetForkTimes(t *testing.T) {
	TaikoGenesisBlock(params.MasayaDevnetNetworkID.Uint64())

	genesis := TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())
	cfg := genesis.Config
	if cfg.UnzenTime == nil {
		t.Fatal("UnzenTime is nil, want MaxUint64")
	}
	if got := *cfg.UnzenTime; got != math.MaxUint64 {
		t.Fatalf("UnzenTime = %d, want %d", got, uint64(math.MaxUint64))
	}
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
