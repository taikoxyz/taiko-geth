package vm

import (
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestUnzenBlockLimit pins the single Unzen block budget used by every Taiko chain.
func TestUnzenBlockLimit(t *testing.T) {
	if got := UnzenZkGasSchedule.BlockLimit; got != 100_000_000 {
		t.Fatalf("UnzenZkGasSchedule.BlockLimit = %d, want 100_000_000", got)
	}
	if got := BlockZkGasLimit; got != 100_000_000 {
		t.Fatalf("BlockZkGasLimit = %d, want 100_000_000", got)
	}
}

// TestUnzenSchedule_TxIntrinsicZkGas pins the single Unzen per-tx intrinsic
// zk-gas charge used by every Taiko chain.
func TestUnzenSchedule_TxIntrinsicZkGas(t *testing.T) {
	if got := TxIntrinsicZkGas; got != 243_000 {
		t.Fatalf("TxIntrinsicZkGas = %d, want 243_000", got)
	}
	if got := UnzenZkGasSchedule.TxIntrinsicZkGas; got != 243_000 {
		t.Fatalf("UnzenZkGasSchedule.TxIntrinsicZkGas = %d, want 243_000", got)
	}
}

// TestUnzenSchedule_Multipliers pins representative recalibrated default
// multipliers so accidental drift is caught.
func TestUnzenSchedule_Multipliers(t *testing.T) {
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
}

// TestFullAddressLookupPreservesCanonicalPrecompileMultipliers pins every
// standard Unzen precompile multiplier to its exact value. The len() assertion
// guards against a dropped or duplicated entry.
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
	for _, b := range []byte{0x12, 0x13, 0x14} {
		if got := UnzenZkGasSchedule.PrecompileMultiplier(common.Address{19: b}); got != math.MaxUint16 {
			t.Errorf("default unlisted %#04x = %d, want failsafe", b, got)
		}
	}
}
