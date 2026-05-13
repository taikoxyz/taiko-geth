package vm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// TestUnzenBlockLimits pins the per-network Unzen block budgets:
// Masaya gets a 10× higher budget; everyone else stays at 100M.
func TestUnzenBlockLimits(t *testing.T) {
	if got := UnzenZkGasSchedule.BlockLimit; got != 100_000_000 {
		t.Fatalf("UnzenZkGasSchedule.BlockLimit = %d, want 100_000_000", got)
	}
	if got := MasayaUnzenZkGasSchedule.BlockLimit; got != 1_000_000_000 {
		t.Fatalf("MasayaUnzenZkGasSchedule.BlockLimit = %d, want 1_000_000_000", got)
	}
	if got := BlockZkGasLimit; got != 100_000_000 {
		t.Fatalf("BlockZkGasLimit = %d, want 100_000_000", got)
	}
	if got := MasayaBlockZkGasLimit; got != 1_000_000_000 {
		t.Fatalf("MasayaBlockZkGasLimit = %d, want 1_000_000_000", got)
	}
}

// TestUnzenSchedule_TxIntrinsicZkGas pins the per-network Unzen per-tx
// intrinsic zk-gas charges: Devnet/Internal/Hoodi/Mainnet at 243_000, Masaya
// at 0. Masaya's zero pin is consensus-critical — header difficulty on Unzen
// blocks encodes the finalized block zk gas, so changing the charge for
// already-finalized Masaya blocks would break their consensus.
func TestUnzenSchedule_TxIntrinsicZkGas(t *testing.T) {
	if got := TxIntrinsicZkGas; got != 243_000 {
		t.Fatalf("TxIntrinsicZkGas = %d, want 243_000", got)
	}
	if got := MasayaTxIntrinsicZkGas; got != 0 {
		t.Fatalf("MasayaTxIntrinsicZkGas = %d, want 0", got)
	}
	if got := UnzenZkGasSchedule.TxIntrinsicZkGas; got != 243_000 {
		t.Fatalf("UnzenZkGasSchedule.TxIntrinsicZkGas = %d, want 243_000", got)
	}
	if got := MasayaUnzenZkGasSchedule.TxIntrinsicZkGas; got != 0 {
		t.Fatalf("MasayaUnzenZkGasSchedule.TxIntrinsicZkGas = %d, want 0", got)
	}
}

// TestMasayaUnzenSchedule_SharesTablesWithDefault asserts byte-identity of
// every non-block-limit field. This guards against accidental drift between
// the two schedules — only the block budget should differ.
func TestMasayaUnzenSchedule_SharesTablesWithDefault(t *testing.T) {
	if MasayaUnzenZkGasSchedule.OpcodeMultipliers != UnzenZkGasSchedule.OpcodeMultipliers {
		t.Fatal("MasayaUnzenZkGasSchedule.OpcodeMultipliers differs from UnzenZkGasSchedule")
	}
	if MasayaUnzenZkGasSchedule.PrecompileMultipliers != UnzenZkGasSchedule.PrecompileMultipliers {
		t.Fatal("MasayaUnzenZkGasSchedule.PrecompileMultipliers differs from UnzenZkGasSchedule")
	}
	if MasayaUnzenZkGasSchedule.SpawnEstimates != UnzenZkGasSchedule.SpawnEstimates {
		t.Fatal("MasayaUnzenZkGasSchedule.SpawnEstimates differs from UnzenZkGasSchedule")
	}
}

// TestUnzenZkGasScheduleFor verifies that the selector routes Masaya (167011)
// to the 1B schedule and every other Taiko chain id to the default 100M
// schedule. Pointer comparison ensures the selector returned the package
// global, not a copy.
func TestUnzenZkGasScheduleFor(t *testing.T) {
	cases := []struct {
		name    string
		chainID *big.Int
		want    *ZkGasSchedule
	}{
		{"nil", nil, &UnzenZkGasSchedule},
		{"mainnet-167000", params.TaikoMainnetNetworkID, &UnzenZkGasSchedule},
		{"internal-167001", params.TaikoInternalNetworkID, &UnzenZkGasSchedule},
		{"hoodi-167013", params.TaikoHoodiNetworkID, &UnzenZkGasSchedule},
		{"masaya-167011", params.MasayaDevnetNetworkID, &MasayaUnzenZkGasSchedule},
		{"explicit-167011", big.NewInt(167011), &MasayaUnzenZkGasSchedule},
		{"unknown-1", big.NewInt(1), &UnzenZkGasSchedule},
	}
	for _, c := range cases {
		got := UnzenZkGasScheduleFor(c.chainID)
		if got != c.want {
			t.Fatalf("UnzenZkGasScheduleFor(%s) = %p, want %p", c.name, got, c.want)
		}
	}
}
