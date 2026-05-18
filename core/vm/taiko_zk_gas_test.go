package vm

import (
	"math"
	"testing"
)

func testSchedule() *ZkGasSchedule {
	s := &ZkGasSchedule{
		BlockLimit: 1_000_000,
		SpawnEstimates: SpawnEstimates{
			Call:         12500,
			CallCode:     12500,
			DelegateCall: 3500,
			StaticCall:   3500,
			Create:       37000,
			Create2:      44500,
		},
	}
	// Fill all opcode multipliers with a failsafe default.
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = math.MaxUint16
	}
	// Set a few known opcodes for testing.
	s.OpcodeMultipliers[0x01] = 12 // ADD
	s.OpcodeMultipliers[0x02] = 21 // MUL
	s.OpcodeMultipliers[0x00] = 0  // STOP
	s.OpcodeMultipliers[0xf1] = 25 // CALL
	s.OpcodeMultipliers[0xf0] = 1  // CREATE

	// Set a precompile multiplier for testing.
	for i := range s.PrecompileMultipliers {
		s.PrecompileMultipliers[i] = math.MaxUint16
	}
	s.PrecompileMultipliers[0x01] = 81 // ecrecover
	s.PrecompileMultipliers[0x02] = 10 // sha256
	return s
}

func TestZkGasMeter_ChargeOpcode(t *testing.T) {
	m := NewZkGasMeter(testSchedule())

	// ADD opcode: rawGas=100, multiplier=12, cost=1200
	if err := m.ChargeOpcode(0x01, 100); err != nil {
		t.Fatalf("unexpected error charging ADD: %v", err)
	}
	if m.TxZkGasUsed() != 1200 {
		t.Fatalf("expected txZkGasUsed=1200, got %d", m.TxZkGasUsed())
	}

	// MUL opcode: rawGas=50, multiplier=21, cost=1050, total=2250
	if err := m.ChargeOpcode(0x02, 50); err != nil {
		t.Fatalf("unexpected error charging MUL: %v", err)
	}
	if m.TxZkGasUsed() != 2250 {
		t.Fatalf("expected txZkGasUsed=2250, got %d", m.TxZkGasUsed())
	}

	// STOP opcode: multiplier=0, cost=0
	if err := m.ChargeOpcode(0x00, 999); err != nil {
		t.Fatalf("unexpected error charging STOP: %v", err)
	}
	if m.TxZkGasUsed() != 2250 {
		t.Fatalf("expected txZkGasUsed=2250 after STOP, got %d", m.TxZkGasUsed())
	}
}

func TestZkGasMeter_CommitAndReset(t *testing.T) {
	m := NewZkGasMeter(testSchedule())

	if err := m.ChargeOpcode(0x01, 100); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitTransaction(); err != nil {
		t.Fatalf("unexpected error committing: %v", err)
	}
	if m.BlockZkGasUsed() != 1200 {
		t.Fatalf("expected blockZkGasUsed=1200, got %d", m.BlockZkGasUsed())
	}
	if m.TxZkGasUsed() != 0 {
		t.Fatalf("expected txZkGasUsed=0 after commit, got %d", m.TxZkGasUsed())
	}

	// Charge again and reset
	if err := m.ChargeOpcode(0x02, 50); err != nil {
		t.Fatal(err)
	}
	m.ResetTransaction()
	if m.TxZkGasUsed() != 0 {
		t.Fatalf("expected txZkGasUsed=0 after reset, got %d", m.TxZkGasUsed())
	}
	// Block total should still be 1200
	if m.BlockZkGasUsed() != 1200 {
		t.Fatalf("expected blockZkGasUsed=1200, got %d", m.BlockZkGasUsed())
	}
}

func TestZkGasMeter_BlockLimitExceeded(t *testing.T) {
	m := NewZkGasMeter(testSchedule()) // BlockLimit = 1_000_000

	// Charge close to the limit: ADD(rawGas=80000, multiplier=12) = 960000
	if err := m.ChargeOpcode(0x01, 80000); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitTransaction(); err != nil {
		t.Fatalf("unexpected error committing: %v", err)
	}

	// Now charge more than remaining: MUL(rawGas=2000, multiplier=21) = 42000
	// Projected = 960000 + 42000 = 1002000 > 1000000 -> error
	if err := m.ChargeOpcode(0x02, 2000); err == nil {
		t.Fatal("expected block limit exceeded error")
	}
}

func TestZkGasMeter_Overflow(t *testing.T) {
	m := NewZkGasMeter(testSchedule())

	// Failsafe multiplier is MaxUint16 = 65535
	// rawGas = MaxUint64 -> overflow in safeMul
	err := m.ChargeOpcode(0xAA, math.MaxUint64)
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("expected ErrZkGasLimitExceeded, got %v", err)
	}
}

func TestZkGasMeter_ChargePrecompile(t *testing.T) {
	m := NewZkGasMeter(testSchedule())

	// ecrecover: gasUsed=3000, multiplier=81, cost=243000
	if err := m.ChargePrecompile(0x01, 3000); err != nil {
		t.Fatalf("unexpected error charging ecrecover: %v", err)
	}
	if m.TxZkGasUsed() != 243000 {
		t.Fatalf("expected txZkGasUsed=243000, got %d", m.TxZkGasUsed())
	}

	// sha256: gasUsed=100, multiplier=10, cost=1000, total=244000
	if err := m.ChargePrecompile(0x02, 100); err != nil {
		t.Fatalf("unexpected error charging sha256: %v", err)
	}
	if m.TxZkGasUsed() != 244000 {
		t.Fatalf("expected txZkGasUsed=244000, got %d", m.TxZkGasUsed())
	}
}

func TestZkGasMeter_ChargeOpcode_AddsOverheadOnDefaultUnzen(t *testing.T) {
	m := NewZkGasMeter(&UnzenZkGasSchedule)

	// ADD opcode: rawGas=3, multiplier=12, +overhead=90 -> cost=126.
	if err := m.ChargeOpcode(0x01, 3); err != nil {
		t.Fatalf("unexpected error charging ADD: %v", err)
	}
	want := 3*uint64(UnzenZkGasSchedule.OpcodeMultipliers[0x01]) + ZkGasMeteringOverhead
	if got := m.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d (raw*mult + overhead)", got, want)
	}
}

func TestZkGasMeter_ChargeOpcode_NoOverheadOnMasaya(t *testing.T) {
	m := NewZkGasMeter(&MasayaUnzenZkGasSchedule)

	// ADD opcode on Masaya: overhead is pinned at 0, so cost is raw*mult only.
	if err := m.ChargeOpcode(0x01, 3); err != nil {
		t.Fatalf("unexpected error charging ADD: %v", err)
	}
	want := 3 * uint64(MasayaUnzenZkGasSchedule.OpcodeMultipliers[0x01])
	if got := m.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d (raw*mult, no overhead)", got, want)
	}
}

func TestZkGasMeter_ChargePrecompile_AddsOverheadOnDefaultUnzen(t *testing.T) {
	m := NewZkGasMeter(&UnzenZkGasSchedule)

	// identity precompile (0x04): gasUsed=18, multiplier=2, +overhead=90 -> cost=126.
	if err := m.ChargePrecompile(0x04, 18); err != nil {
		t.Fatalf("unexpected error charging identity: %v", err)
	}
	want := 18*uint64(UnzenZkGasSchedule.PrecompileMultipliers[0x04]) + ZkGasMeteringOverhead
	if got := m.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d (gasUsed*mult + overhead)", got, want)
	}
}

func TestZkGasMeter_ChargePrecompile_NoOverheadOnMasaya(t *testing.T) {
	m := NewZkGasMeter(&MasayaUnzenZkGasSchedule)

	// identity precompile on Masaya: overhead pinned at 0.
	if err := m.ChargePrecompile(0x04, 18); err != nil {
		t.Fatalf("unexpected error charging identity: %v", err)
	}
	want := 18 * uint64(MasayaUnzenZkGasSchedule.PrecompileMultipliers[0x04])
	if got := m.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d (gasUsed*mult, no overhead)", got, want)
	}
}

func TestZkGasMeter_ChargeTxIntrinsic_AddsToInFlight(t *testing.T) {
	s := testSchedule()
	s.TxIntrinsicZkGas = 243_000
	m := NewZkGasMeter(s)

	if err := m.ChargeTxIntrinsic(); err != nil {
		t.Fatalf("unexpected error charging tx intrinsic: %v", err)
	}
	if m.TxZkGasUsed() != 243_000 {
		t.Fatalf("expected txZkGasUsed=243000, got %d", m.TxZkGasUsed())
	}
	if m.BlockZkGasUsed() != 0 {
		t.Fatalf("expected blockZkGasUsed=0 before commit, got %d", m.BlockZkGasUsed())
	}

	if err := m.CommitTransaction(); err != nil {
		t.Fatalf("unexpected error committing: %v", err)
	}
	if m.BlockZkGasUsed() != 243_000 {
		t.Fatalf("expected blockZkGasUsed=243000 after commit, got %d", m.BlockZkGasUsed())
	}
}

func TestZkGasMeter_ChargeTxIntrinsic_NoopWhenScheduleZero(t *testing.T) {
	s := testSchedule()
	s.TxIntrinsicZkGas = 0
	m := NewZkGasMeter(s)

	if err := m.ChargeTxIntrinsic(); err != nil {
		t.Fatalf("unexpected error charging zero tx intrinsic: %v", err)
	}
	if m.TxZkGasUsed() != 0 {
		t.Fatalf("expected txZkGasUsed=0 after zero-intrinsic charge, got %d", m.TxZkGasUsed())
	}
}

func TestZkGasMeter_ChargeTxIntrinsic_ReturnsLimitExceeded(t *testing.T) {
	// Build a schedule where the block limit can be filled to (limit -
	// intrinsic + 1) using an opcode with multiplier 1 (CREATE 0xf0 in the
	// shared Unzen tables). The next ChargeTxIntrinsic alone then exceeds
	// the remaining block budget.
	s := testSchedule()
	s.TxIntrinsicZkGas = 243_000
	if s.OpcodeMultipliers[0xf0] != 1 {
		t.Fatalf("test prerequisite: CREATE opcode multiplier must be 1, got %d", s.OpcodeMultipliers[0xf0])
	}
	m := NewZkGasMeter(s)

	prefill := s.BlockLimit - s.TxIntrinsicZkGas + 1
	if err := m.ChargeOpcode(0xf0, prefill); err != nil {
		t.Fatalf("prefill charge failed: %v", err)
	}
	if err := m.CommitTransaction(); err != nil {
		t.Fatalf("prefill commit failed: %v", err)
	}

	if err := m.ChargeTxIntrinsic(); err != ErrZkGasLimitExceeded {
		t.Fatalf("expected ErrZkGasLimitExceeded, got %v", err)
	}
	// In-flight tx total must remain unchanged when the charge is rejected.
	if m.TxZkGasUsed() != 0 {
		t.Fatalf("expected txZkGasUsed=0 after rejected intrinsic charge, got %d", m.TxZkGasUsed())
	}
}

func TestZkGasMeter_CommitExceedsLimit(t *testing.T) {
	s := &ZkGasSchedule{
		BlockLimit: 500,
	}
	s.OpcodeMultipliers[0x01] = 1
	m := NewZkGasMeter(s)

	// First tx: charge 1*400 = 400, commit -> blockTotal = 400
	if err := m.ChargeOpcode(0x01, 400); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitTransaction(); err != nil {
		t.Fatal(err)
	}
	if m.BlockZkGasUsed() != 400 {
		t.Fatalf("expected blockZkGasUsed=400, got %d", m.BlockZkGasUsed())
	}

	// Second tx: charge 1*101 = 101, projected = 400+101 = 501 > 500 -> error at charge
	err := m.ChargeOpcode(0x01, 101)
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("expected ErrZkGasLimitExceeded at charge, got %v", err)
	}
	// txZkGasUsed should not have been updated
	if m.TxZkGasUsed() != 0 {
		t.Fatalf("expected txZkGasUsed=0 after failed charge, got %d", m.TxZkGasUsed())
	}

	// A charge that just fits: 1*100 = 100, projected = 400+100 = 500 == limit -> ok
	if err := m.ChargeOpcode(0x01, 100); err != nil {
		t.Fatalf("expected charge to succeed at limit boundary: %v", err)
	}
	if err := m.CommitTransaction(); err != nil {
		t.Fatalf("expected commit to succeed at exact limit: %v", err)
	}
	if m.BlockZkGasUsed() != 500 {
		t.Fatalf("expected blockZkGasUsed=500, got %d", m.BlockZkGasUsed())
	}
}

func TestZkGasMeter_CommitDoesNotRecheckBlockLimit(t *testing.T) {
	s := &ZkGasSchedule{BlockLimit: 500}
	m := NewZkGasMeter(s)
	m.blockZkGasUsed = 400
	m.txZkGasUsed = 101

	if err := m.CommitTransaction(); err != nil {
		t.Fatalf("CommitTransaction returned error: %v", err)
	}
	if got := m.BlockZkGasUsed(); got != 501 {
		t.Fatalf("BlockZkGasUsed = %d, want 501", got)
	}
	if got := m.TxZkGasUsed(); got != 0 {
		t.Fatalf("TxZkGasUsed = %d, want 0", got)
	}
}

// --- Unzen schedule spot-check tests ---

func TestUnzenSchedule_SpotChecks(t *testing.T) {
	s := &UnzenZkGasSchedule

	tests := []struct {
		name  string
		index int
		arr   [256]uint16
		want  uint16
	}{
		{"ADD opcode", 0x01, s.OpcodeMultipliers, 12},
		{"MUL opcode", 0x02, s.OpcodeMultipliers, 21},
		{"MULMOD opcode", 0x09, s.OpcodeMultipliers, 152},
		{"DIV opcode", 0x04, s.OpcodeMultipliers, 110},
		{"KECCAK256 opcode", 0x20, s.OpcodeMultipliers, 85},
		{"SELFBALANCE opcode", 0x47, s.OpcodeMultipliers, 85},
		{"STOP opcode", 0x00, s.OpcodeMultipliers, 0},
		{"RETURN opcode", 0xf3, s.OpcodeMultipliers, 0},
		{"REVERT opcode", 0xfd, s.OpcodeMultipliers, 0},
		{"CALL opcode", 0xf1, s.OpcodeMultipliers, 25},
		{"CREATE opcode", 0xf0, s.OpcodeMultipliers, 1},
		{"CREATE2 opcode", 0xf5, s.OpcodeMultipliers, 1},
		{"TLOAD opcode", 0x5c, s.OpcodeMultipliers, 1},
		{"EQ opcode", 0x14, s.OpcodeMultipliers, 35},
		{"JUMP opcode", 0x56, s.OpcodeMultipliers, 3},
		// Failsafe: unassigned opcode should be MaxUint16
		{"unassigned opcode 0xB0", 0xB0, s.OpcodeMultipliers, math.MaxUint16},
		// Precompile checks
		{"ecrecover precompile", 0x01, s.PrecompileMultipliers, 81},
		{"modexp precompile", 0x05, s.PrecompileMultipliers, 1363},
		{"bn128_mul precompile", 0x07, s.PrecompileMultipliers, 87},
		{"point_evaluation precompile", 0x0a, s.PrecompileMultipliers, 398},
		{"blake2f precompile", 0x09, s.PrecompileMultipliers, 243},
		{"identity precompile", 0x04, s.PrecompileMultipliers, 2},
		// Failsafe: unassigned precompile should be MaxUint16
		{"unassigned precompile 0xFF", 0xFF, s.PrecompileMultipliers, math.MaxUint16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.arr[tt.index]; got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}

	// Block limit check
	if s.BlockLimit != 100_000_000 {
		t.Errorf("expected BlockLimit=100000000, got %d", s.BlockLimit)
	}
}

func TestUnzenSchedule_SpawnEstimates(t *testing.T) {
	s := &UnzenZkGasSchedule

	tests := []struct {
		name  string
		field uint64
		want  uint64
	}{
		{"Call", s.SpawnEstimates.Call, 12500},
		{"CallCode", s.SpawnEstimates.CallCode, 12500},
		{"DelegateCall", s.SpawnEstimates.DelegateCall, 3500},
		{"StaticCall", s.SpawnEstimates.StaticCall, 3500},
		{"Create", s.SpawnEstimates.Create, 37000},
		{"Create2", s.SpawnEstimates.Create2, 44500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.field != tt.want {
				t.Errorf("got %d, want %d", tt.field, tt.want)
			}
		})
	}
}

func TestZkGasMeter_IntegrationWithIsSpawnOpcode(t *testing.T) {
	spawnOps := []OpCode{CALL, CALLCODE, DELEGATECALL, STATICCALL, CREATE, CREATE2}
	for _, op := range spawnOps {
		if !IsSpawnOpcode(op) {
			t.Errorf("expected IsSpawnOpcode(0x%02x) = true", byte(op))
		}
	}

	nonSpawnOps := []OpCode{ADD, MUL, STOP, JUMP, SLOAD, SSTORE, MLOAD, PUSH1, DUP1, SWAP1, LOG0, RETURN, REVERT}
	for _, op := range nonSpawnOps {
		if IsSpawnOpcode(op) {
			t.Errorf("expected IsSpawnOpcode(0x%02x) = false", byte(op))
		}
	}
}

func TestZkGasMeter_SpawnEstimateLookup(t *testing.T) {
	m := NewZkGasMeter(&UnzenZkGasSchedule)

	tests := []struct {
		opcode byte
		want   uint64
	}{
		{0xf1, 12500}, // CALL
		{0xf2, 12500}, // CALLCODE
		{0xf4, 3500},  // DELEGATECALL
		{0xfa, 3500},  // STATICCALL
		{0xf0, 37000}, // CREATE
		{0xf5, 44500}, // CREATE2
		{0x01, 0},     // ADD (not a spawn opcode)
		{0x00, 0},     // STOP (not a spawn opcode)
	}
	for _, tt := range tests {
		got := m.SpawnEstimate(tt.opcode)
		if got != tt.want {
			t.Errorf("SpawnEstimate(0x%02x) = %d, want %d", tt.opcode, got, tt.want)
		}
	}
}
