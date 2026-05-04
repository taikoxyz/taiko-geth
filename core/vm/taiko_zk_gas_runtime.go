package vm

import "github.com/ethereum/go-ethereum/params"

// CHANGE(taiko): zkGasPendingStep stores the in-flight opcode state for one EVM depth.
type zkGasPendingStep struct {
	// opcode is the opcode byte captured before the step executes.
	opcode byte
	// gasBefore is the frame gas remaining before the opcode executes.
	gasBefore uint64
	// spawned records whether the opcode should use the fixed spawn estimate.
	spawned bool
}

// CHANGE(taiko): ZkGasStepTracker keeps pending per-depth opcode steps so Unzen
// zk gas can charge spawn opcodes with exact alethia-reth semantics.
type ZkGasStepTracker struct {
	// meter owns the fork schedule and accumulated zk gas totals.
	meter *ZkGasMeter
	// pending stores the in-flight opcode step for each active call depth.
	pending []*zkGasPendingStep
}

// CHANGE(taiko): NewZkGasStepTracker creates a per-depth zk gas tracker for a block execution.
func NewZkGasStepTracker(meter *ZkGasMeter) *ZkGasStepTracker {
	return &ZkGasStepTracker{meter: meter}
}

// CHANGE(taiko): Begin records the opcode and pre-step gas at the current depth.
func (t *ZkGasStepTracker) Begin(depth int, opcode byte, gasBefore uint64) {
	t.ensureDepth(depth)
	t.pending[depth] = &zkGasPendingStep{
		opcode:    opcode,
		gasBefore: gasBefore,
	}
}

// CHANGE(taiko): MarkCallSpawn marks the pending CALL-family opcode at this depth as spawned.
func (t *ZkGasStepTracker) MarkCallSpawn(depth int) {
	t.markSpawn(depth, func(op byte) bool {
		return op == byte(CALL) || op == byte(CALLCODE) || op == byte(DELEGATECALL) || op == byte(STATICCALL)
	})
}

// CHANGE(taiko): MarkCreateSpawn marks the pending CREATE-family opcode at this depth as spawned.
func (t *ZkGasStepTracker) MarkCreateSpawn(depth int) {
	t.markSpawn(depth, func(op byte) bool {
		return op == byte(CREATE) || op == byte(CREATE2)
	})
}

// CHANGE(taiko): FinishAndCharge applies the canonical raw gas rule for the pending opcode.
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

// CHANGE(taiko): ensureDepth grows the pending-step slice so the requested depth is addressable.
func (t *ZkGasStepTracker) ensureDepth(depth int) {
	for len(t.pending) <= depth {
		t.pending = append(t.pending, nil)
	}
}

// CHANGE(taiko): markSpawn marks the matching pending opcode at the current depth.
func (t *ZkGasStepTracker) markSpawn(depth int, matches func(byte) bool) {
	if depth < 0 || depth >= len(t.pending) || t.pending[depth] == nil {
		return
	}
	if matches(t.pending[depth].opcode) {
		t.pending[depth].spawned = true
	}
}

// CHANGE(taiko): zkGasStepGasAfter mirrors REVM's callback-visible gas delta
// for cases where go-ethereum performs validation after charging dynamic gas.
func zkGasStepGasAfter(op OpCode, err error, gasBefore, gasAfter uint64) uint64 {
	if err == ErrWriteProtection && (op == CREATE || op == CREATE2) {
		return gasBefore
	}
	if err == ErrWriteProtection && op >= LOG0 && op <= LOG4 && gasBefore >= params.LogGas {
		return gasBefore - params.LogGas
	}
	return gasAfter
}
