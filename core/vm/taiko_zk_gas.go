package vm

import (
	"errors"
	"math"
)

// CHANGE(taiko): ErrZkGasLimitExceeded is returned when zk gas arithmetic overflows or exceeds the block limit.
var ErrZkGasLimitExceeded = errors.New("zk gas limit exceeded")

// CHANGE(taiko): SpawnEstimates holds fixed raw-gas estimates for spawn opcodes.
type SpawnEstimates struct {
	Call         uint64
	CallCode     uint64
	DelegateCall uint64
	StaticCall   uint64
	Create       uint64
	Create2      uint64
}

// CHANGE(taiko): ZkGasSchedule defines consensus-owned zk gas parameters for a Taiko fork.
type ZkGasSchedule struct {
	BlockLimit            uint64
	OpcodeMultipliers     [256]uint16
	PrecompileMultipliers [256]uint16
	SpawnEstimates        SpawnEstimates
}

// CHANGE(taiko): ZkGasMeter provides checked zk gas accounting for a single block execution.
type ZkGasMeter struct {
	schedule       *ZkGasSchedule
	blockZkGasUsed uint64
	txZkGasUsed    uint64
}

// CHANGE(taiko): NewZkGasMeter creates a meter for the provided consensus schedule.
func NewZkGasMeter(schedule *ZkGasSchedule) *ZkGasMeter {
	return &ZkGasMeter{schedule: schedule}
}

// ChargeOpcode charges zk gas for a single opcode execution: rawGas * multiplier.
func (m *ZkGasMeter) ChargeOpcode(opcode byte, rawGas uint64) error {
	multiplier := uint64(m.schedule.OpcodeMultipliers[opcode])
	return m.charge(rawGas, multiplier)
}

// ChargePrecompile charges zk gas for a precompile execution: gasUsed * multiplier.
func (m *ZkGasMeter) ChargePrecompile(addrLowByte byte, gasUsed uint64) error {
	multiplier := uint64(m.schedule.PrecompileMultipliers[addrLowByte])
	return m.charge(gasUsed, multiplier)
}

// CommitTransaction promotes the current tx zk gas into the finalized block total.
func (m *ZkGasMeter) CommitTransaction() error {
	next, overflow := safeAdd(m.blockZkGasUsed, m.txZkGasUsed)
	if overflow {
		m.txZkGasUsed = 0
		return ErrZkGasLimitExceeded
	}
	m.blockZkGasUsed = next
	m.txZkGasUsed = 0
	return nil
}

// ResetTransaction discards in-flight tx zk gas.
func (m *ZkGasMeter) ResetTransaction() {
	m.txZkGasUsed = 0
}

// BlockZkGasUsed returns the finalized block total.
func (m *ZkGasMeter) BlockZkGasUsed() uint64 {
	return m.blockZkGasUsed
}

// TxZkGasUsed returns the in-flight tx total.
func (m *ZkGasMeter) TxZkGasUsed() uint64 {
	return m.txZkGasUsed
}

// Schedule returns the meter's consensus schedule.
func (m *ZkGasMeter) Schedule() *ZkGasSchedule {
	return m.schedule
}

// SpawnEstimate returns the fixed raw-gas estimate for the given spawn opcode.
func (m *ZkGasMeter) SpawnEstimate(opcode byte) uint64 {
	switch opcode {
	case 0xf1: // CALL
		return m.schedule.SpawnEstimates.Call
	case 0xf2: // CALLCODE
		return m.schedule.SpawnEstimates.CallCode
	case 0xf4: // DELEGATECALL
		return m.schedule.SpawnEstimates.DelegateCall
	case 0xfa: // STATICCALL
		return m.schedule.SpawnEstimates.StaticCall
	case 0xf0: // CREATE
		return m.schedule.SpawnEstimates.Create
	case 0xf5: // CREATE2
		return m.schedule.SpawnEstimates.Create2
	default:
		return 0
	}
}

// IsSpawnOpcode returns true for CALL/CALLCODE/DELEGATECALL/STATICCALL/CREATE/CREATE2.
func IsSpawnOpcode(op OpCode) bool {
	switch op {
	case CALL, CALLCODE, DELEGATECALL, STATICCALL, CREATE, CREATE2:
		return true
	default:
		return false
	}
}

// charge applies a checked zk gas charge against the current transaction and block budget.
func (m *ZkGasMeter) charge(base, multiplier uint64) error {
	cost, overflow := safeMul(base, multiplier)
	if overflow {
		return ErrZkGasLimitExceeded
	}
	nextTx, overflow := safeAdd(m.txZkGasUsed, cost)
	if overflow {
		return ErrZkGasLimitExceeded
	}
	projected, overflow := safeAdd(m.blockZkGasUsed, nextTx)
	if overflow || projected > m.schedule.BlockLimit {
		return ErrZkGasLimitExceeded
	}
	m.txZkGasUsed = nextTx
	return nil
}

func safeMul(a, b uint64) (uint64, bool) {
	if a == 0 || b == 0 {
		return 0, false
	}
	if a > math.MaxUint64/b {
		return 0, true
	}
	return a * b, false
}

func safeAdd(a, b uint64) (uint64, bool) {
	if a > math.MaxUint64-b {
		return 0, true
	}
	return a + b, false
}
