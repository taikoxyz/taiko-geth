package vm

import (
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

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
// zk gas can charge spawn opcodes with consensus zk-gas semantics.
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

// CHANGE(taiko): zkGasDynamicOOGGasAfter mirrors REVM's callback-visible gas
// for dynamic-cost shortfalls caught before go-ethereum executes the opcode.
func zkGasDynamicOOGGasAfter(evm *EVM, op OpCode, stack *Stack, mem *Memory, memorySize, memoryLastGasCost, gasBefore, gasAfterStatic uint64) uint64 {
	preMemoryCost, gasBeforePreMemory, ok := zkGasPreMemoryCost(evm, op, stack, gasBefore, gasAfterStatic)
	if !ok {
		return 0
	}
	currentMemorySize := uint64(mem.Len())
	if op == CALL || op == CALLCODE {
		inSize, outSize, ok := zkGasCallMemorySizes(stack, 3, 4, 5, 6)
		if !ok {
			return 0
		}
		return zkGasMemoryOOGGasAfter(gasBeforePreMemory, preMemoryCost, currentMemorySize, memoryLastGasCost, inSize, outSize)
	}
	if op == DELEGATECALL || op == STATICCALL {
		inSize, outSize, ok := zkGasCallMemorySizes(stack, 2, 3, 4, 5)
		if !ok {
			return 0
		}
		return zkGasMemoryOOGGasAfter(gasBeforePreMemory, preMemoryCost, currentMemorySize, memoryLastGasCost, inSize, outSize)
	}
	return zkGasMemoryOOGGasAfter(gasBeforePreMemory, preMemoryCost, currentMemorySize, memoryLastGasCost, memorySize)
}

func zkGasPreMemoryCost(evm *EVM, op OpCode, stack *Stack, gasBefore, gasAfterStatic uint64) (uint64, uint64, bool) {
	switch {
	case op == MLOAD || op == MSTORE || op == MSTORE8 || op == RETURN || op == REVERT:
		return 0, gasAfterStatic, true
	case op == KECCAK256:
		cost, ok := zkGasWordCost(stack.Back(1), params.Keccak256WordGas)
		return cost, gasAfterStatic, ok
	case op == CALLDATACOPY || op == CODECOPY || op == MCOPY || op == RETURNDATACOPY:
		cost, ok := zkGasWordCost(stack.Back(2), params.CopyGas)
		return cost, gasAfterStatic, ok
	case op == EXTCODECOPY:
		cost, ok := zkGasWordCost(stack.Back(3), params.CopyGas)
		return cost, gasAfterStatic, ok
	case op >= LOG0 && op <= LOG4:
		cost, ok := zkGasLogPreMemoryCost(op, stack)
		return cost, gasAfterStatic, ok
	case op == CALL || op == CALLCODE || op == DELEGATECALL || op == STATICCALL:
		return 0, gasAfterStatic, true
	case op == CREATE || op == CREATE2:
		cost, ok := zkGasCreatePreMemoryCost(evm, stack)
		return cost, gasBefore, ok
	default:
		return 0, 0, false
	}
}

func zkGasMemoryOOGGasAfter(gasBeforePreMemory, preMemoryCost, currentMemorySize, memoryLastGasCost uint64, memorySizes ...uint64) uint64 {
	if gasBeforePreMemory < preMemoryCost {
		return 0
	}
	gas := gasBeforePreMemory - preMemoryCost
	for _, memorySize := range memorySizes {
		memoryCost, nextMemorySize, nextMemoryLastGasCost, ok := zkGasMemoryExpansionCost(currentMemorySize, memoryLastGasCost, memorySize)
		if !ok {
			return 0
		}
		if gas < memoryCost {
			return gas
		}
		gas -= memoryCost
		currentMemorySize = nextMemorySize
		memoryLastGasCost = nextMemoryLastGasCost
	}
	return 0
}

func zkGasMemoryExpansionCost(currentMemorySize, memoryLastGasCost, memorySize uint64) (uint64, uint64, uint64, bool) {
	if memorySize == 0 || memorySize <= currentMemorySize {
		return 0, currentMemorySize, memoryLastGasCost, true
	}
	if memorySize > 0x1FFFFFFFE0 {
		return 0, currentMemorySize, memoryLastGasCost, false
	}
	newMemoryWords := toWordSize(memorySize)
	newTotalFee := newMemoryWords*params.MemoryGas + newMemoryWords*newMemoryWords/params.QuadCoeffDiv
	if newTotalFee < memoryLastGasCost {
		return 0, currentMemorySize, memoryLastGasCost, false
	}
	return newTotalFee - memoryLastGasCost, memorySize, newTotalFee, true
}

func zkGasCallMemorySizes(stack *Stack, inOffset, inLen, outOffset, outLen int) (uint64, uint64, bool) {
	inputSize, overflow := calcMemSize64(stack.Back(inOffset), stack.Back(inLen))
	if overflow {
		return 0, 0, false
	}
	outputSize, overflow := calcMemSize64(stack.Back(outOffset), stack.Back(outLen))
	if overflow {
		return 0, 0, false
	}
	inputSize, overflow = zkGasWordAlignedMemorySize(inputSize)
	if overflow {
		return 0, 0, false
	}
	outputSize, overflow = zkGasWordAlignedMemorySize(outputSize)
	if overflow {
		return 0, 0, false
	}
	return inputSize, outputSize, true
}

func zkGasWordAlignedMemorySize(memorySize uint64) (uint64, bool) {
	if memorySize == 0 {
		return 0, false
	}
	return math.SafeMul(toWordSize(memorySize), 32)
}

func zkGasWordCost(value *uint256.Int, perWord uint64) (uint64, bool) {
	size, overflow := value.Uint64WithOverflow()
	if overflow {
		return 0, false
	}
	cost, overflow := math.SafeMul(toWordSize(size), perWord)
	return cost, !overflow
}

func zkGasLogPreMemoryCost(op OpCode, stack *Stack) (uint64, bool) {
	requestedSize, overflow := stack.Back(1).Uint64WithOverflow()
	if overflow {
		return 0, false
	}
	dataGas, overflow := math.SafeMul(requestedSize, params.LogDataGas)
	if overflow {
		return 0, false
	}
	topicGas, overflow := math.SafeMul(uint64(op-LOG0), params.LogTopicGas)
	if overflow {
		return 0, false
	}
	cost, overflow := math.SafeAdd(params.LogGas, topicGas)
	if overflow {
		return 0, false
	}
	cost, overflow = math.SafeAdd(cost, dataGas)
	return cost, !overflow
}

func zkGasCreatePreMemoryCost(evm *EVM, stack *Stack) (uint64, bool) {
	if !evm.chainRules.IsShanghai {
		return 0, true
	}
	return zkGasWordCost(stack.Back(2), params.InitCodeWordGas)
}

// CHANGE(taiko): zkGasStepGasAfter mirrors REVM's callback-visible gas delta
// for cases where go-ethereum performs validation after charging dynamic gas.
// Static LOG/CREATE write-protection values are derived from observed Rust
// reference EVM inspector callbacks, not from a direct source-level contract.
func zkGasStepGasAfter(op OpCode, err error, gasBefore, gasAfter uint64) uint64 {
	if err == ErrWriteProtection && (op == CREATE || op == CREATE2) {
		return gasBefore
	}
	if err == ErrWriteProtection && op >= LOG0 && op <= LOG4 && gasBefore >= params.LogGas {
		return gasBefore - params.LogGas
	}
	return gasAfter
}
