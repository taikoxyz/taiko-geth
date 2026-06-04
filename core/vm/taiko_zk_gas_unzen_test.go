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
	// Masaya stays at 85; modexp (0x05) went 1363 -> 154 while Masaya stays at 1363.
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
	if got := UnzenZkGasSchedule.OpcodeMultipliers[0x1e]; got != 14 {
		t.Fatalf("default CLZ (0x1e) = %d, want 14", got)
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
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x05}); got != 154 {
		t.Fatalf("default modexp (0x05) = %d, want 154", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x01}); got != 47 {
		t.Fatalf("default ecrecover (0x01) = %d, want 47", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x04}); got != 6 {
		t.Fatalf("default identity (0x04) = %d, want 6", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{18: 0x01}); got != 163 {
		t.Fatalf("default p256verify (0x100) = %d, want 163", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: 0x14}); got != math.MaxUint16 {
		t.Fatalf("default unlisted precompile (0x14) = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}

	// Frozen Masaya schedule.
	if got := MasayaUnzenZkGasSchedule.OpcodeMultipliers[0x20]; got != 85 {
		t.Fatalf("Masaya keccak256 (0x20) = %d, want frozen 85", got)
	}
	if got := MasayaUnzenZkGasSchedule.OpcodeMultipliers[0x1e]; got != math.MaxUint16 {
		t.Fatalf("Masaya CLZ (0x1e) = %d, want frozen failsafe %d", got, uint16(math.MaxUint16))
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

// TestHighRangePrecompileCollisionResolvesToFailsafe is the regression guard: a
// high-range precompile whose low byte collides with a canonical one must resolve
// to the failsafe, not the colliding canonical multiplier. No such precompile
// exists in the Unzen fork today.
func TestHighRangePrecompileCollisionResolvesToFailsafe(t *testing.T) {
	// L1Sload-style address: low byte 0x01 collides with ecrecover, upper bytes differ.
	collider := common.HexToAddress("0x1670000000000000000000000000000000010001")
	// identity-style collider: low byte 0x04.
	identityCollider := common.HexToAddress("0x1670000000000000000000000000000000010004")
	ecrecover := common.Address{19: 0x01}

	if got := UnzenZkGasSchedule.PrecompileMultiplier(ecrecover); got != 47 {
		t.Fatalf("default ecrecover = %d, want 47", got)
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(collider); got != math.MaxUint16 {
		t.Fatalf("default collider = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}
	if got := UnzenZkGasSchedule.PrecompileMultiplier(identityCollider); got != math.MaxUint16 {
		t.Fatalf("default identity collider = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}

	if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(ecrecover); got != 81 {
		t.Fatalf("Masaya ecrecover = %d, want frozen 81", got)
	}
	if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(collider); got != math.MaxUint16 {
		t.Fatalf("Masaya collider = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}
	if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(identityCollider); got != math.MaxUint16 {
		t.Fatalf("Masaya identity collider = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}
}

// TestFullAddressLookupPreservesCanonicalPrecompileMultipliers pins every precompile
// multiplier on both schedules to its exact value, so finalized blocks stay
// byte-identical — including Masaya, whose finalized block zk-gas total is committed
// to the header difficulty field. The default schedule keys BLS12 at the canonical
// Osaka addresses (0x0b..0x11); Masaya stays frozen at the pre-final EIP-2537 draft
// addresses (0x0b,0x0c,0x0e,0x0f,0x11,0x12,0x13). The len() assertions (default 18,
// Masaya 17) guard against a dropped or duplicated entry: either changes the count.
func TestFullAddressLookupPreservesCanonicalPrecompileMultipliers(t *testing.T) {
	defaultExpected := map[byte]uint16{
		0x01: 47, 0x02: 10, 0x03: 4, 0x04: 6, 0x05: 154, 0x06: 19, 0x07: 58,
		0x08: 54, 0x09: 166, 0x0a: 859, 0x0b: 201, 0x0c: 93, 0x0d: 230, 0x0e: 71,
		0x0f: 365, 0x10: 246, 0x11: 208,
	}
	for b, want := range defaultExpected {
		if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: b}); got != want {
			t.Errorf("default precompile %#04x = %d, want %d", b, got, want)
		}
	}
	// p256verify (RIP-7212) lives at the two-byte address 0x100, keyed as
	// {18: 0x01}; added to the default table in taiko-mono#21748.
	if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{18: 0x01}); got != 163 {
		t.Errorf("default p256verify (0x100) = %d, want 163", got)
	}
	if got := len(UnzenZkGasSchedule.PrecompileMultipliers); got != 18 {
		t.Errorf("default precompile table has %d entries, want 18", got)
	}

	masayaExpected := map[byte]uint16{
		0x01: 81, 0x02: 10, 0x03: 3, 0x04: 2, 0x05: 1363, 0x06: 38, 0x07: 87,
		0x08: 82, 0x09: 243, 0x0a: 398, 0x0b: 112, 0x0c: 52, 0x0e: 111, 0x0f: 39,
		0x11: 134, 0x12: 159, 0x13: 112,
	}
	for b, want := range masayaExpected {
		if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: b}); got != want {
			t.Errorf("Masaya precompile %#04x = %d, want %d", b, got, want)
		}
	}
	// Masaya stays frozen: p256verify was never in its finalized schedule, so it
	// must resolve to the failsafe, not 163. Adding it would break consensus on
	// already-finalized Masaya Unzen blocks.
	if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(common.Address{18: 0x01}); got != math.MaxUint16 {
		t.Errorf("Masaya p256verify (0x100) = %d, want failsafe %d", got, uint16(math.MaxUint16))
	}
	if got := len(MasayaUnzenZkGasSchedule.PrecompileMultipliers); got != 17 {
		t.Errorf("Masaya precompile table has %d entries, want 17", got)
	}

	// The default schedule keys BLS12 at the canonical Osaka addresses, so its gaps
	// are 0x12/0x13 (vacated by the re-keying) plus out-of-range 0x14.
	for _, b := range []byte{0x12, 0x13, 0x14} {
		if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: b}); got != math.MaxUint16 {
			t.Errorf("default unlisted %#04x = %d, want failsafe", b, got)
		}
	}
	// Masaya stays frozen at the pre-final draft addresses, so its gaps remain
	// 0x0d/0x10 plus out-of-range 0x14.
	for _, b := range []byte{0x0d, 0x10, 0x14} {
		if got := MasayaUnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: b}); got != math.MaxUint16 {
			t.Errorf("Masaya unlisted %#04x = %d, want failsafe", b, got)
		}
	}
}
