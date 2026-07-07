package vm

import (
	"errors"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// CHANGE(taiko): zkGasRevmStaticGas mirrors the static gas REVM's instruction
// table deducts in Interpreter::step before the instruction body runs
// (revm-interpreter 35.0.1 instruction_table_gas_changes_spec, with the
// BERLIN repricing applied; Unzen maps to OSAKA). go-ethereum's own
// constantGas values cannot stand in for these: go-ethereum classifies
// several base costs as dynamic gas (SLOAD, EXP, LOG0-LOG4) or as constant
// gas REVM charges inside the instruction body (CREATE/CREATE2 carry 32000
// here but 0 in REVM's table), and REVM additionally ships Amsterdam-gated
// opcodes (SLOTNUM, DUPN, SWAPN, EXCHANGE) that go-ethereum treats as
// undefined. Opcodes unknown to REVM's table stay at 0.
// AI prompt: When upgrading revm, regenerate this table from the new
// revm-interpreter instruction table for the fork Unzen maps to.
var zkGasRevmStaticGas = [256]uint64{
	ADD:        3,
	MUL:        5,
	SUB:        3,
	DIV:        5,
	SDIV:       5,
	MOD:        5,
	SMOD:       5,
	ADDMOD:     8,
	MULMOD:     8,
	EXP:        10,
	SIGNEXTEND: 5,

	LT:     3,
	GT:     3,
	SLT:    3,
	SGT:    3,
	EQ:     3,
	ISZERO: 3,
	AND:    3,
	OR:     3,
	XOR:    3,
	NOT:    3,
	BYTE:   3,
	SHL:    3,
	SHR:    3,
	SAR:    3,
	CLZ:    5,

	KECCAK256: 30,

	ADDRESS:        2,
	BALANCE:        100,
	ORIGIN:         2,
	CALLER:         2,
	CALLVALUE:      2,
	CALLDATALOAD:   3,
	CALLDATASIZE:   2,
	CALLDATACOPY:   3,
	CODESIZE:       2,
	CODECOPY:       3,
	GASPRICE:       2,
	EXTCODESIZE:    100,
	EXTCODECOPY:    100,
	RETURNDATASIZE: 2,
	RETURNDATACOPY: 3,
	EXTCODEHASH:    100,

	BLOCKHASH:   20,
	COINBASE:    2,
	TIMESTAMP:   2,
	NUMBER:      2,
	DIFFICULTY:  2,
	GASLIMIT:    2,
	CHAINID:     2,
	SELFBALANCE: 5,
	BASEFEE:     2,
	BLOBHASH:    3,
	BLOBBASEFEE: 2,
	0x4b:        2, // SLOTNUM (Amsterdam, EIP-7843)

	POP:      2,
	MLOAD:    3,
	MSTORE:   3,
	MSTORE8:  3,
	SLOAD:    100,
	JUMP:     8,
	JUMPI:    10,
	PC:       2,
	MSIZE:    2,
	GAS:      2,
	JUMPDEST: 1,
	TLOAD:    100,
	TSTORE:   100,
	MCOPY:    3,
	PUSH0:    2,

	PUSH1: 3, PUSH2: 3, PUSH3: 3, PUSH4: 3, PUSH5: 3, PUSH6: 3, PUSH7: 3, PUSH8: 3,
	PUSH9: 3, PUSH10: 3, PUSH11: 3, PUSH12: 3, PUSH13: 3, PUSH14: 3, PUSH15: 3, PUSH16: 3,
	PUSH17: 3, PUSH18: 3, PUSH19: 3, PUSH20: 3, PUSH21: 3, PUSH22: 3, PUSH23: 3, PUSH24: 3,
	PUSH25: 3, PUSH26: 3, PUSH27: 3, PUSH28: 3, PUSH29: 3, PUSH30: 3, PUSH31: 3, PUSH32: 3,

	DUP1: 3, DUP2: 3, DUP3: 3, DUP4: 3, DUP5: 3, DUP6: 3, DUP7: 3, DUP8: 3,
	DUP9: 3, DUP10: 3, DUP11: 3, DUP12: 3, DUP13: 3, DUP14: 3, DUP15: 3, DUP16: 3,

	SWAP1: 3, SWAP2: 3, SWAP3: 3, SWAP4: 3, SWAP5: 3, SWAP6: 3, SWAP7: 3, SWAP8: 3,
	SWAP9: 3, SWAP10: 3, SWAP11: 3, SWAP12: 3, SWAP13: 3, SWAP14: 3, SWAP15: 3, SWAP16: 3,

	LOG0: 375,
	LOG1: 375,
	LOG2: 375,
	LOG3: 375,
	LOG4: 375,

	0xe6: 3, // DUPN (Amsterdam, EIP-663)
	0xe7: 3, // SWAPN (Amsterdam, EIP-663)
	0xe8: 3, // EXCHANGE (Amsterdam, EIP-663)

	CALL:         100,
	CALLCODE:     100,
	DELEGATECALL: 100,
	STATICCALL:   100,
	SELFDESTRUCT: 5000,
}

// CHANGE(taiko): zkGasPreExecutionGasAfter mirrors REVM's callback-visible gas
// for opcodes that fail before go-ethereum deducts any gas. REVM charges the
// instruction-table static gas in step() before the instruction body runs, so
// a stack underflow/overflow (or the not-activated halt of an opcode REVM
// ships but Unzen does not enable) still surfaces a net delta of the table's
// static gas — or all remaining frame gas when even the static charge cannot
// be paid, because REVM's halt_oog spends everything.
func zkGasPreExecutionGasAfter(op OpCode, gasBefore uint64) uint64 {
	staticGas := zkGasRevmStaticGas[op]
	if gasBefore < staticGas {
		return 0
	}
	return gasBefore - staticGas
}

// CHANGE(taiko): isRevmNotActivatedOpcode reports opcodes that REVM ships in
// its instruction table behind a fork gate Unzen (OSAKA) does not enable.
// go-ethereum treats them as undefined opcodes and charges no gas, but REVM
// deducts their table static gas in step() before the instruction halts with
// NotActivated, so zk gas metering must charge them the same way.
// AI prompt: When upgrading revm, update this list from the fork-gated opcodes
// present in REVM's instruction table but not activated for Unzen.
func isRevmNotActivatedOpcode(op OpCode) bool {
	switch byte(op) {
	case 0x4b, 0xe6, 0xe7, 0xe8: // SLOTNUM, DUPN, SWAPN, EXCHANGE (Amsterdam)
		return true
	default:
		return false
	}
}

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

// CHANGE(taiko): ChargePendingSpawn charges a marked CALL/CREATE-family step
// before child execution can run. It clears the pending step on success so the
// interpreter's post-op FinishAndCharge path does not charge the same spawn twice.
func (t *ZkGasStepTracker) ChargePendingSpawn(depth int) error {
	if depth < 0 || depth >= len(t.pending) || t.pending[depth] == nil {
		return nil
	}
	step := t.pending[depth]
	if !step.spawned || !IsSpawnOpcode(OpCode(step.opcode)) {
		return nil
	}
	if err := t.meter.ChargeOpcode(step.opcode, t.meter.SpawnEstimate(step.opcode)); err != nil {
		return err
	}
	t.pending[depth] = nil
	return nil
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

// CHANGE(taiko): zkGasStaticContextHaltsBeforeBody reports opcodes whose REVM
// instruction body halts on its static-context check before any in-body
// charge, while go-ethereum surfaces the same failure only after computing
// memory sizes or dynamic gas. SSTORE, TSTORE, SELFDESTRUCT, and CALL with
// value fail earlier here — in the dynamic-gas functions with
// ErrWriteProtection or in opcode execution — and never reach the paths this
// predicate guards.
func zkGasStaticContextHaltsBeforeBody(op OpCode) bool {
	switch {
	case op == CREATE || op == CREATE2:
		return true
	case op >= LOG0 && op <= LOG4:
		return true
	}
	return false
}

// CHANGE(taiko): zkGasDynamicUintOverflowGasAfter resolves the REVM-mirroring
// charge when a dynamic gas function fails with a uint64 overflow. go-ethereum
// caps memory sizes inside memoryGasCost, while REVM prices any size with
// saturating arithmetic and fails the resulting charge in its in-body order:
// pre-memory word costs spend all remaining gas when unpayable, and the memory
// expansion charge halts preserving gas. The boolean reports whether the
// opcode has a mirrored reconstruction.
func zkGasDynamicUintOverflowGasAfter(evm *EVM, op OpCode, stack *Stack, mem *Memory, gasBefore, gasAfterStatic uint64) (uint64, bool) {
	switch {
	case op == CREATE || op == CREATE2:
		return zkGasCreateShortfallGasAfter(evm, stack, mem, gasBefore), true
	case op >= LOG0 && op <= LOG4:
		return zkGasLogShortfallGasAfter(evm, op, stack, mem, gasBefore), true
	case op == KECCAK256 || op == CALLDATACOPY || op == CODECOPY || op == RETURNDATACOPY || op == MCOPY || op == EXTCODECOPY:
		return zkGasCopyShortfallGasAfter(evm, op, stack, mem, gasBefore), true
	case op == CALL || op == CALLCODE || op == DELEGATECALL || op == STATICCALL:
		return zkGasCallShortfallGasAfter(evm, op, stack, mem, gasBefore), true
	case op == MLOAD || op == MSTORE || op == MSTORE8 || op == RETURN || op == REVERT:
		// No in-body charge precedes REVM's gas-preserving memory halt for
		// these opcodes, so only the table static gas is visible.
		return zkGasPreExecutionGasAfter(op, gasBefore), true
	}
	return gasAfterStatic, false
}

// CHANGE(taiko): zkGasCallShortfallGasAfter mirrors REVM's callback-visible
// gas for CALL-family steps whose memory ranges fail before go-ethereum
// charges dynamic gas. REVM resizes and charges the input range before the
// output range, and every operand or expansion failure in either range halts
// preserving gas, so an output-side failure keeps the input expansion charge
// visible on top of the table static. A value-bearing CALL in a static frame
// halts right after the three leading pops, before either memory range.
func zkGasCallShortfallGasAfter(evm *EVM, op OpCode, stack *Stack, mem *Memory, gasBefore uint64) uint64 {
	staticGas := zkGasRevmStaticGas[op]
	if gasBefore < staticGas {
		return 0
	}
	gas := gasBefore - staticGas
	if op == CALL && evm.readOnly && !stack.Back(2).IsZero() {
		return gas
	}
	inOffset, inSize, outOffset, outSize := 3, 4, 5, 6
	if op == DELEGATECALL || op == STATICCALL {
		inOffset, inSize, outOffset, outSize = 2, 3, 4, 5
	}
	currentMemorySize := uint64(mem.Len())
	memoryLastGasCost := mem.lastGasCost
	for _, memRange := range [2][2]int{{inOffset, inSize}, {outOffset, outSize}} {
		memSize, overflow := calcMemSize64(stack.Back(memRange[0]), stack.Back(memRange[1]))
		if overflow {
			return gas
		}
		alignedMemSize, overflow := math.SafeMul(toWordSize(memSize), 32)
		if overflow {
			return gas
		}
		memoryCost, nextMemorySize, nextMemoryLastGasCost, ok := zkGasMemoryExpansionCost(currentMemorySize, memoryLastGasCost, alignedMemSize)
		if !ok || gas < memoryCost {
			return gas
		}
		gas -= memoryCost
		currentMemorySize, memoryLastGasCost = nextMemorySize, nextMemoryLastGasCost
	}
	return gas
}

// CHANGE(taiko): zkGasDynamicOOGGasAfter mirrors REVM's callback-visible gas
// for dynamic-cost shortfalls caught before go-ethereum executes the opcode.
func zkGasDynamicOOGGasAfter(evm *EVM, op OpCode, stack *Stack, mem *Memory, memorySize, memoryLastGasCost, gasBefore, gasAfterStatic uint64) uint64 {
	// REVM's static-context check for these opcodes halts before any in-body
	// charge, so a dynamic-gas shortfall go-ethereum catches first must meter
	// only the table static gas.
	if evm.readOnly && zkGasStaticContextHaltsBeforeBody(op) {
		return zkGasPreExecutionGasAfter(op, gasBefore)
	}
	// RETURNDATACOPY validates its source range before REVM's copy charge, so
	// its shortfall reconstruction must apply that bounds check first.
	if op == RETURNDATACOPY {
		return zkGasCopyShortfallGasAfter(evm, op, stack, mem, gasBefore)
	}
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

// CHANGE(taiko): zkGasCreateBodyGasAfter reconstructs REVM's in-body
// CREATE/CREATE2 charge order up to, but not including, the instruction tail
// that follows the initcode and memory charges. The static-context check and
// operand/size validation halt before any charge, the EIP-3860 initcode word
// cost spends all remaining gas when it cannot be paid, and memory expansion
// halts preserving remaining gas when it cannot be paid. The boolean reports
// whether REVM reaches that tail with the returned gas still remaining.
func zkGasCreateBodyGasAfter(evm *EVM, stack *Stack, mem *Memory, gasBefore uint64) (uint64, bool) {
	if evm.readOnly {
		return gasBefore, false
	}
	size, overflow := stack.Back(2).Uint64WithOverflow()
	if overflow {
		return gasBefore, false
	}
	if size == 0 {
		return gasBefore, true
	}
	var initcodeCost uint64
	if evm.chainRules.IsShanghai {
		// Track the fork-aware EIP-3860 limit the dynamic gas functions
		// enforce, so this reconstruction can never drift from them.
		if CheckMaxInitCodeSize(&evm.chainRules, size) != nil {
			return gasBefore, false
		}
		initcodeCost = toWordSize(size) * params.InitCodeWordGas
	}
	if gasBefore < initcodeCost {
		return 0, false
	}
	gasAfterInitcode := gasBefore - initcodeCost
	memSize, overflow := calcMemSize64(stack.Back(1), stack.Back(2))
	if overflow {
		return gasAfterInitcode, false
	}
	alignedMemSize, overflow := math.SafeMul(toWordSize(memSize), 32)
	if overflow {
		return gasAfterInitcode, false
	}
	memoryCost, _, _, ok := zkGasMemoryExpansionCost(uint64(mem.Len()), mem.lastGasCost, alignedMemSize)
	if !ok || gasAfterInitcode < memoryCost {
		return gasAfterInitcode, false
	}
	return gasAfterInitcode - memoryCost, true
}

// CHANGE(taiko): zkGasCreateShortfallGasAfter mirrors REVM's callback-visible
// gas for CREATE/CREATE2 steps that fail before go-ethereum reaches the
// instruction body. go-ethereum fronts the 32000 base cost as constant gas and
// validates memory sizes before dynamic gas, while REVM charges in instruction
// order. On every path that funnels here the instruction tail is the trailing
// base (+ CREATE2 hashing) charge, which cannot be paid and spends all
// remaining gas.
func zkGasCreateShortfallGasAfter(evm *EVM, stack *Stack, mem *Memory, gasBefore uint64) uint64 {
	gasAfter, reachedTail := zkGasCreateBodyGasAfter(evm, stack, mem, gasBefore)
	if reachedTail {
		return 0
	}
	return gasAfter
}

// CHANGE(taiko): zkGasCreate2SaltUnderflowGasAfter mirrors REVM's
// callback-visible gas for a CREATE2 that underflows on its fourth stack
// operand. REVM pops value, offset, and length first, charges the EIP-3860
// initcode cost and memory expansion, and only then pops the salt, so this
// late underflow halts preserving the gas left by those in-body charges and
// never reaches the trailing base (+ hashing) charge. go-ethereum's up-front
// stack check fires before any of that and would otherwise meter the step as
// zero.
func zkGasCreate2SaltUnderflowGasAfter(evm *EVM, stack *Stack, mem *Memory, gasBefore uint64) uint64 {
	gasAfter, _ := zkGasCreateBodyGasAfter(evm, stack, mem, gasBefore)
	return gasAfter
}

// CHANGE(taiko): zkGasLogShortfallGasAfter mirrors REVM's callback-visible gas
// for LOG steps that fail before go-ethereum charges dynamic gas. go-ethereum
// fronts no constant gas for LOG, while REVM deducts the 375 table static in
// step() and then charges in instruction order: the static-context check and a
// length operand beyond 64 bits halt before the topic+data charge, the
// saturating topic+data cost spends all remaining gas when it cannot be paid,
// and offset-operand or memory-expansion failures halt preserving the gas left
// after that charge.
func zkGasLogShortfallGasAfter(evm *EVM, op OpCode, stack *Stack, mem *Memory, gasBefore uint64) uint64 {
	if evm.readOnly {
		return zkGasPreExecutionGasAfter(op, gasBefore)
	}
	staticGas := zkGasRevmStaticGas[op]
	if gasBefore < staticGas {
		return 0
	}
	gasAfterStatic := gasBefore - staticGas
	size, overflow := stack.Back(1).Uint64WithOverflow()
	if overflow {
		return gasAfterStatic
	}
	dataGas, overflow := math.SafeMul(size, params.LogDataGas)
	if overflow {
		return 0
	}
	bodyCost, overflow := math.SafeAdd(uint64(op-LOG0)*params.LogTopicGas, dataGas)
	if overflow || gasAfterStatic < bodyCost {
		return 0
	}
	gasAfterBody := gasAfterStatic - bodyCost
	if size == 0 {
		return gasAfterBody
	}
	memSize, overflow := calcMemSize64(stack.Back(0), stack.Back(1))
	if overflow {
		return gasAfterBody
	}
	alignedMemSize, overflow := math.SafeMul(toWordSize(memSize), 32)
	if overflow {
		return gasAfterBody
	}
	memoryCost, _, _, ok := zkGasMemoryExpansionCost(uint64(mem.Len()), mem.lastGasCost, alignedMemSize)
	if !ok || gasAfterBody < memoryCost {
		return gasAfterBody
	}
	return gasAfterBody - memoryCost
}

// CHANGE(taiko): zkGasCopyShortfallGasAfter mirrors REVM's callback-visible
// gas for KECCAK256 and copy-family steps that fail before go-ethereum charges
// dynamic gas. REVM charges in instruction order: the table static in step(),
// a gas-preserving halt on a length operand beyond 64 bits (and, for
// RETURNDATACOPY, on a source range outside the return buffer), the saturating
// per-word cost (spending all remaining gas when it cannot be paid), then
// gas-preserving halts on offset operands and memory expansion. EXTCODECOPY's
// trailing cold-access surcharge spends all remaining gas when it cannot be
// paid.
func zkGasCopyShortfallGasAfter(evm *EVM, op OpCode, stack *Stack, mem *Memory, gasBefore uint64) uint64 {
	staticGas := zkGasRevmStaticGas[op]
	if gasBefore < staticGas {
		return 0
	}
	gasAfterStatic := gasBefore - staticGas
	lenIndex, perWordGas := 2, params.CopyGas
	switch op {
	case KECCAK256:
		lenIndex, perWordGas = 1, params.Keccak256WordGas
	case EXTCODECOPY:
		lenIndex = 3
	}
	size, overflow := stack.Back(lenIndex).Uint64WithOverflow()
	if overflow {
		return gasAfterStatic
	}
	if op == RETURNDATACOPY {
		dataOffset, overflow := stack.Back(1).Uint64WithOverflow()
		if overflow {
			dataOffset = ^uint64(0)
		}
		dataEnd, overflow := math.SafeAdd(dataOffset, size)
		if overflow || dataEnd > uint64(len(evm.returnData)) {
			return gasAfterStatic
		}
	}
	copyCost, ok := zkGasWordCost(stack.Back(lenIndex), perWordGas)
	if !ok || gasAfterStatic < copyCost {
		return 0
	}
	gasAfterCopy := gasAfterStatic - copyCost
	memOffsetIndex := 0
	if op == EXTCODECOPY {
		memOffsetIndex = 1
	}
	memSize, overflow := calcMemSize64(stack.Back(memOffsetIndex), stack.Back(lenIndex))
	if overflow {
		return gasAfterCopy
	}
	if op == MCOPY {
		srcSize, srcOverflow := calcMemSize64(stack.Back(1), stack.Back(lenIndex))
		if srcOverflow {
			return gasAfterCopy
		}
		if srcSize > memSize {
			memSize = srcSize
		}
	}
	alignedMemSize, overflow := math.SafeMul(toWordSize(memSize), 32)
	if overflow {
		return gasAfterCopy
	}
	memoryCost, _, _, ok := zkGasMemoryExpansionCost(uint64(mem.Len()), mem.lastGasCost, alignedMemSize)
	if !ok || gasAfterCopy < memoryCost {
		return gasAfterCopy
	}
	if op == EXTCODECOPY {
		return 0
	}
	return gasAfterCopy - memoryCost
}

// CHANGE(taiko): zkGasMemorySizeOverflowGasAfter picks the REVM-mirroring
// charge for memory-size operand overflows. Most opcodes surface these after
// REVM already deducted its table static gas (matching go-ethereum's
// constant-gas deduction), but CREATE-family, LOG, KECCAK256, and copy-family
// operand overflows halt REVM around their in-body charges (initcode cost,
// topic+data cost, per-word cost) instead of after a fronted constant.
func zkGasMemorySizeOverflowGasAfter(evm *EVM, op OpCode, stack *Stack, mem *Memory, gasBefore, gasAfterStatic uint64) uint64 {
	switch {
	case op == CREATE || op == CREATE2:
		return zkGasCreateShortfallGasAfter(evm, stack, mem, gasBefore)
	case op >= LOG0 && op <= LOG4:
		return zkGasLogShortfallGasAfter(evm, op, stack, mem, gasBefore)
	case op == KECCAK256 || op == CALLDATACOPY || op == CODECOPY || op == RETURNDATACOPY || op == MCOPY || op == EXTCODECOPY:
		return zkGasCopyShortfallGasAfter(evm, op, stack, mem, gasBefore)
	case op == CALL || op == CALLCODE || op == DELEGATECALL || op == STATICCALL:
		return zkGasCallShortfallGasAfter(evm, op, stack, mem, gasBefore)
	}
	return gasAfterStatic
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
	// REVM validates the return-data source range before charging its copy and
	// memory costs, so only the table static gas is visible.
	if err == ErrReturnDataOutOfBounds && op == RETURNDATACOPY {
		return zkGasPreExecutionGasAfter(op, gasBefore)
	}
	// Opcodes REVM ships behind a fork gate Unzen does not enable execute as
	// undefined opcodes here (no gas movement), but REVM deducts their table
	// static gas before halting with NotActivated.
	var invalidOpcode *ErrInvalidOpCode
	if errors.As(err, &invalidOpcode) && isRevmNotActivatedOpcode(op) {
		return zkGasPreExecutionGasAfter(op, gasBefore)
	}
	return gasAfter
}
