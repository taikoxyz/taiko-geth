package core

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/forkid"
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

// setDevnetEtnaTime overrides DevnetEtnaTime for the duration of the test.
func setDevnetEtnaTime(t *testing.T, etnaTime *uint64) {
	t.Helper()
	original := DevnetEtnaTime
	t.Cleanup(func() { DevnetEtnaTime = original })
	DevnetEtnaTime = etnaTime
}

// Etna is unscheduled (nil) on every built-in network, and on the unknown
// network IDs that fall back to the internal devnet, until a fork time is set.
func TestTaikoGenesisBlock_EtnaUnscheduled(t *testing.T) {
	setDevnetEtnaTime(t, nil)
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
		cfg := TaikoGenesisBlock(c.networkID).Config
		if cfg.EtnaTime != nil {
			t.Fatalf("%s EtnaTime = %d, want nil", c.name, *cfg.EtnaTime)
		}
		if cfg.IsEtna(math.MaxUint64) {
			t.Fatalf("%s Etna is active at the maximum timestamp", c.name)
		}
	}
}

// The internal devnet always activates Unzen, Cancun, Prague and Osaka at
// genesis, and Etna at DevnetEtnaTime: 0 is genesis, N is N. The override never
// reaches mainnet or Hoodi.
func TestTaikoGenesisBlock_DevnetEtnaTime(t *testing.T) {
	for _, etnaTime := range []uint64{0, 100} {
		setDevnetEtnaTime(t, &etnaTime)
		for _, networkID := range []uint64{params.TaikoInternalNetworkID.Uint64(), 167_011} {
			cfg := TaikoGenesisBlock(networkID).Config
			if cfg.EtnaTime == nil || *cfg.EtnaTime != etnaTime {
				t.Fatalf("network %d EtnaTime = %v, want %d", networkID, cfg.EtnaTime, etnaTime)
			}
			if etnaTime > 0 && cfg.IsEtna(etnaTime-1) {
				t.Fatalf("network %d Etna is active at %d, before its fork time %d", networkID, etnaTime-1, etnaTime)
			}
			if !cfg.IsEtna(etnaTime) {
				t.Fatalf("network %d Etna is not active at its fork time %d", networkID, etnaTime)
			}
			for name, forked := range map[string]bool{
				"Unzen":  cfg.IsUnzen(0),
				"Cancun": cfg.IsCancun(common.Big0, 0),
				"Prague": cfg.IsPrague(common.Big0, 0),
				"Osaka":  cfg.IsOsaka(common.Big0, 0),
			} {
				if !forked {
					t.Fatalf("network %d %s is not active at genesis", networkID, name)
				}
			}
		}
		for _, networkID := range []uint64{params.TaikoMainnetNetworkID.Uint64(), params.TaikoHoodiNetworkID.Uint64()} {
			if cfg := TaikoGenesisBlock(networkID).Config; cfg.EtnaTime != nil {
				t.Fatalf("network %d EtnaTime = %d, want nil", networkID, *cfg.EtnaTime)
			}
		}
	}
}

// An unscheduled Etna adds no fork to the EIP-2124 fork ID. The expected IDs
// are the ones these networks had before Etna existed, once every scheduled
// fork has passed: an Etna fork would change Next.
func TestTaikoGenesisBlock_ForkIDWithoutEtna(t *testing.T) {
	setDevnetEtnaTime(t, nil)
	const head, time = 1 << 40, 1 << 62
	cases := []struct {
		name      string
		networkID uint64
		want      forkid.ID
	}{
		{"mainnet", params.TaikoMainnetNetworkID.Uint64(), forkid.ID{Hash: [4]byte{0x0a, 0x87, 0x85, 0xdc}, Next: 0}},
		{"hoodi", params.TaikoHoodiNetworkID.Uint64(), forkid.ID{Hash: [4]byte{0xcf, 0x42, 0x40, 0x61}, Next: 0}},
		{"internal", params.TaikoInternalNetworkID.Uint64(), forkid.ID{Hash: [4]byte{0xae, 0x83, 0x94, 0x71}, Next: 0}},
	}
	for _, c := range cases {
		genesis := TaikoGenesisBlock(c.networkID)
		if got := forkid.NewID(genesis.Config, genesis.ToBlock(), head, time); got != c.want {
			t.Fatalf("%s fork ID = {%#x, %d}, want {%#x, %d}", c.name, got.Hash, got.Next, c.want.Hash, c.want.Next)
		}
	}
}
