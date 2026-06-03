package vm

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

// TestMasayaUnzenSchedule_FreezesPreRecalibrationMultipliers asserts that the
// recalibration changed the default opcode and precompile tables while Masaya
// stays frozen, so the two schedules' tables must now differ. Spawn estimates
// were not recalibrated and remain identical across networks.
func TestMasayaUnzenSchedule_FreezesPreRecalibrationMultipliers(t *testing.T) {
	if MasayaUnzenZkGasSchedule.OpcodeMultipliers == UnzenZkGasSchedule.OpcodeMultipliers {
		t.Fatal("MasayaUnzenZkGasSchedule.OpcodeMultipliers must differ from the recalibrated default")
	}
	if MasayaUnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x01}) == UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x01}) {
		t.Fatal("MasayaUnzenZkGasSchedule precompile table must differ from the recalibrated default")
	}
	// Spot-check known recalibrated entries so this guard fails if the two
	// tables ever realign: keccak256 (0x20) went 85 -> 31 for the default while
	// Masaya stays at 85; modexp (0x05) went 1363 -> 923 while Masaya stays at 1363.
	if MasayaUnzenZkGasSchedule.OpcodeMultipliers[0x20] == UnzenZkGasSchedule.OpcodeMultipliers[0x20] {
		t.Fatal("keccak256 (0x20) opcode multiplier must differ between Masaya and default")
	}
	if MasayaUnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x05}) == UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x05}) {
		t.Fatal("modexp (0x05) precompile multiplier must differ between Masaya and default")
	}
	if MasayaUnzenZkGasSchedule.SpawnEstimates != UnzenZkGasSchedule.SpawnEstimates {
		t.Fatal("MasayaUnzenZkGasSchedule.SpawnEstimates differs from UnzenZkGasSchedule")
	}
}

// TestUnzenSchedule_Multipliers pins representative recalibrated default
// multipliers and the corresponding frozen Masaya values, so any accidental
// drift in either table is caught.
func TestUnzenSchedule_Multipliers(t *testing.T) {
	// Recalibrated default schedule.
	if got := UnzenZkGasSchedule.OpcodeMultipliers[0x20]; got != 31 {
		t.Fatalf("default keccak256 (0x20) = %d, want 31", got)
	}
	if got := UnzenZkGasSchedule.OpcodeMultipliers[0xf1]; got != 20 {
		t.Fatalf("default CALL (0xf1) = %d, want 20", got)
	}
	if got := UnzenZkGasSchedule.OpcodeMultipliers[0xfe]; got != 0 {
		t.Fatalf("default INVALID (0xfe) = %d, want 0", got)
	}
	if got := UnzenZkGasSchedule.OpcodeMultipliers[0xac]; got != math.MaxUint16 {
		t.Fatalf("default unlisted (0xac) = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x05}); got != 923 {
		t.Fatalf("default modexp (0x05) = %d, want 923", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x01}); got != 47 {
		t.Fatalf("default ecrecover (0x01) = %d, want 47", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x04}); got != 6 {
		t.Fatalf("default identity (0x04) = %d, want 6", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x14}); got != math.MaxUint16 {
		t.Fatalf("default unlisted precompile (0x14) = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}

	// Frozen Masaya schedule.
	if got := MasayaUnzenZkGasSchedule.OpcodeMultipliers[0x20]; got != 85 {
		t.Fatalf("Masaya keccak256 (0x20) = %d, want frozen 85", got)
	}
	if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x05}); got != 1363 {
		t.Fatalf("Masaya modexp (0x05) = %d, want frozen 1363", got)
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
