package vm

type zkGasPendingStep struct {
	opcode    byte
	gasBefore uint64
	spawned   bool
}

// CHANGE(taiko): ZkGasStepTracker keeps pending per-depth opcode steps so Uzen
// zk gas can charge spawn opcodes with exact alethia-reth semantics.
type ZkGasStepTracker struct {
	meter   *ZkGasMeter
	pending []*zkGasPendingStep
}

func NewZkGasStepTracker(meter *ZkGasMeter) *ZkGasStepTracker {
	return &ZkGasStepTracker{meter: meter}
}

func (t *ZkGasStepTracker) Begin(depth int, opcode byte, gasBefore uint64) {
	t.ensureDepth(depth)
	t.pending[depth] = &zkGasPendingStep{
		opcode:    opcode,
		gasBefore: gasBefore,
	}
}

func (t *ZkGasStepTracker) MarkCallSpawn(depth int) {
	t.markSpawn(depth, func(op byte) bool {
		return op == byte(CALL) || op == byte(CALLCODE) || op == byte(DELEGATECALL) || op == byte(STATICCALL)
	})
}

func (t *ZkGasStepTracker) MarkCreateSpawn(depth int) {
	t.markSpawn(depth, func(op byte) bool {
		return op == byte(CREATE) || op == byte(CREATE2)
	})
}

func (t *ZkGasStepTracker) FinishAndCharge(depth int, gasAfter uint64) error {
	if depth >= len(t.pending) || t.pending[depth] == nil {
		return nil
	}
	step := t.pending[depth]
	t.pending[depth] = nil

	if gasAfter > step.gasBefore {
		return ErrZkGasLimitExceeded
	}
	rawGas := step.gasBefore - gasAfter
	if IsSpawnOpcode(OpCode(step.opcode)) && step.spawned {
		rawGas = t.meter.SpawnEstimate(step.opcode)
	}
	return t.meter.ChargeOpcode(step.opcode, rawGas)
}

func (t *ZkGasStepTracker) ensureDepth(depth int) {
	for len(t.pending) <= depth {
		t.pending = append(t.pending, nil)
	}
}

func (t *ZkGasStepTracker) markSpawn(depth int, matches func(byte) bool) {
	for _, idx := range []int{depth, depth - 1} {
		if idx < 0 || idx >= len(t.pending) || t.pending[idx] == nil {
			continue
		}
		if matches(t.pending[idx].opcode) {
			t.pending[idx].spawned = true
			return
		}
	}
}
