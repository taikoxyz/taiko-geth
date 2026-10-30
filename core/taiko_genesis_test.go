package core

import (
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

// TestTaikoGenesisHashesUnchanged pins the genesis block hash of every Taiko
// network as computed on the go-ethereum v1.17.2 base. A node whose genesis
// hash changes no longer recognises its own chain. With TAIKO_PRINT_PARITY=1
// it prints the observed hashes instead of comparing them.
func TestTaikoGenesisHashesUnchanged(t *testing.T) {
	printMode := os.Getenv("TAIKO_PRINT_PARITY") == "1"
	for _, tt := range []struct {
		id   *big.Int
		want common.Hash
	}{
		{params.TaikoMainnetNetworkID, common.HexToHash("0x90bc60466882de9637e269e87abab53c9108cf9113188bc4f80bcfcb10e489b9")},
		{params.TaikoHoodiNetworkID, common.HexToHash("0x8e3d16acf3ecc1fbe80309b04e010b90c9ccb3da14e98536cfe66bb93407d228")},
		{params.TaikoInternalNetworkID, common.HexToHash("0x0ab9db72570aa79341b50c888abaf2e64395d5ce31d3753b738b7e5abec132c4")},
	} {
		got := TaikoGenesisBlock(tt.id.Uint64()).ToBlock().Hash()
		if printMode {
			fmt.Printf("%d: common.HexToHash(%q),\n", tt.id, got.Hex())
			continue
		}
		if got != tt.want {
			t.Fatalf("network %d genesis = %v, want %v", tt.id, got, tt.want)
		}
	}
}
