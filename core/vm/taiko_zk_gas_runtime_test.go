package vm

import "testing"

func TestZkGasStepTracker_FinishUsesNetStepGasWhenNotSpawned(t *testing.T) {
	meter := NewZkGasMeter(testSchedule())
	tracker := NewZkGasStepTracker(meter)

	tracker.Begin(0, byte(CALL), 1_000)
	if err := tracker.FinishAndCharge(0, 900); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}

	if got, want := meter.TxZkGasUsed(), uint64(2_500); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestZkGasStepTracker_FinishUsesSpawnEstimateWhenMarked(t *testing.T) {
	meter := NewZkGasMeter(testSchedule())
	tracker := NewZkGasStepTracker(meter)

	tracker.Begin(0, byte(CALL), 1_000)
	tracker.MarkCallSpawn(0)
	if err := tracker.FinishAndCharge(0, 900); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}

	if got, want := meter.TxZkGasUsed(), uint64(312_500); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestZkGasStepTracker_MarkSpawnDoesNotLeakAcrossDepths(t *testing.T) {
	meter := NewZkGasMeter(testSchedule())
	meter.Schedule().OpcodeMultipliers[byte(DELEGATECALL)] = 21

	tracker := NewZkGasStepTracker(meter)

	tracker.Begin(0, byte(CALL), 5_000)
	tracker.Begin(1, byte(DELEGATECALL), 3_000)
	tracker.MarkCallSpawn(1)

	if err := tracker.FinishAndCharge(1, 2_900); err != nil {
		t.Fatalf("child FinishAndCharge returned error: %v", err)
	}
	if err := tracker.FinishAndCharge(0, 4_700); err != nil {
		t.Fatalf("parent FinishAndCharge returned error: %v", err)
	}

	if got, want := meter.TxZkGasUsed(), uint64(81_000); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}
