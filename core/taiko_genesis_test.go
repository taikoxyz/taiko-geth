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
