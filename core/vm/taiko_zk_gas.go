package vm

import (
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
)

// CHANGE(taiko): cap per-block zk proving work with a protocol-level limit.
const BLOCK_ZK_GAS_LIMIT uint64 = 100_000_000

type taikoZKGasInvocation struct {
	childFrameCreated bool
	precompileInvoked bool
	precompileAddress common.Address
	precompileGasUsed uint64
}

type taikoZKGasFrame struct {
	invocation taikoZKGasInvocation
}

// ZKGasMeter tracks per-tx and per-block zk gas usage.
type ZKGasMeter struct {
	blockUsed uint64
	txUsed    uint64
	txActive  bool
}

func NewZKGasMeter() *ZKGasMeter {
	return &ZKGasMeter{}
}

func (m *ZKGasMeter) StartTx() {
	m.txUsed = 0
	m.txActive = true
}

func (m *ZKGasMeter) AbortTx() {
	m.txUsed = 0
	m.txActive = false
}

func (m *ZKGasMeter) CommitTx() error {
	if !m.txActive {
		return nil
	}
	blockUsed, overflow := safeAddUint64(m.blockUsed, m.txUsed)
	if overflow || blockUsed > BLOCK_ZK_GAS_LIMIT {
		return ErrZKGasLimitReached
	}
	m.blockUsed = blockUsed
	m.txUsed = 0
	m.txActive = false
	return nil
}

func (m *ZKGasMeter) TxUsed() uint64 {
	return m.txUsed
}

func (m *ZKGasMeter) BlockUsed() uint64 {
	return m.blockUsed
}

func (m *ZKGasMeter) ChargeOpcode(op OpCode, stepGas uint64, invocation taikoZKGasInvocation) error {
	if !m.txActive {
		return nil
	}
	rawGas := stepGas
	if taikoZKGasSpawnEstimates[op] != 0 && (invocation.childFrameCreated || invocation.precompileInvoked) {
		rawGas = taikoZKGasSpawnEstimates[op]
	}
	return m.charge(rawGas, taikoZKGasOpcodeMultipliers[byte(op)])
}

func (m *ZKGasMeter) ChargePrecompile(addr common.Address, gasUsed uint64) error {
	if !m.txActive {
		return nil
	}
	return m.charge(gasUsed, taikoZKGasPrecompileMultipliers[addr[common.AddressLength-1]])
}

func (m *ZKGasMeter) charge(rawGas uint64, multiplier uint16) error {
	charge, overflow := safeMulUint64(rawGas, uint64(multiplier))
	if overflow {
		return ErrZKGasLimitReached
	}
	txUsed, overflow := safeAddUint64(m.txUsed, charge)
	if overflow {
		return ErrZKGasLimitReached
	}
	blockTotal, overflow := safeAddUint64(m.blockUsed, txUsed)
	if overflow || blockTotal > BLOCK_ZK_GAS_LIMIT {
		return ErrZKGasLimitReached
	}
	m.txUsed = txUsed
	return nil
}

func safeAddUint64(x, y uint64) (uint64, bool) {
	sum, carry := bits.Add64(x, y, 0)
	return sum, carry != 0
}

func safeMulUint64(x, y uint64) (uint64, bool) {
	hi, lo := bits.Mul64(x, y)
	return lo, hi != 0
}

func (evm *EVM) taikoZKGasEnabled() bool {
	return evm.zkGasMeter != nil && evm.zkGasMeter.txActive
}

func (evm *EVM) taikoZKGasPushFrame() {
	if !evm.taikoZKGasEnabled() {
		return
	}
	evm.zkGasFrames = append(evm.zkGasFrames, taikoZKGasFrame{})
}

func (evm *EVM) taikoZKGasPopFrame() {
	if !evm.taikoZKGasEnabled() || len(evm.zkGasFrames) == 0 {
		return
	}
	evm.zkGasFrames = evm.zkGasFrames[:len(evm.zkGasFrames)-1]
}

func (evm *EVM) taikoZKGasBeginOpcode() {
	frame := evm.taikoZKGasCurrentFrame()
	if frame == nil {
		return
	}
	frame.invocation = taikoZKGasInvocation{}
}

func (evm *EVM) taikoZKGasChargeOpcode(op OpCode, stepGas uint64) error {
	if !evm.taikoZKGasEnabled() {
		return nil
	}
	frame := evm.taikoZKGasCurrentFrame()
	if frame == nil {
		return nil
	}
	invocation := frame.invocation
	if err := evm.zkGasMeter.ChargeOpcode(op, stepGas, invocation); err != nil {
		return err
	}
	if invocation.precompileInvoked {
		return evm.zkGasMeter.ChargePrecompile(invocation.precompileAddress, invocation.precompileGasUsed)
	}
	return nil
}

func (evm *EVM) taikoZKGasRecordChildFrame() {
	frame := evm.taikoZKGasCurrentFrame()
	if frame == nil {
		return
	}
	frame.invocation.childFrameCreated = true
}

func (evm *EVM) taikoZKGasRecordPrecompile(addr common.Address, gasUsed uint64) {
	frame := evm.taikoZKGasCurrentFrame()
	if frame == nil {
		return
	}
	frame.invocation.precompileInvoked = true
	frame.invocation.precompileAddress = addr
	frame.invocation.precompileGasUsed = gasUsed
}

func (evm *EVM) taikoZKGasCurrentFrame() *taikoZKGasFrame {
	if !evm.taikoZKGasEnabled() || len(evm.zkGasFrames) == 0 {
		return nil
	}
	return &evm.zkGasFrames[len(evm.zkGasFrames)-1]
}

// CHANGE(taiko): attach the block-scoped zk gas meter used by Uzen execution.
func (evm *EVM) SetZKGasMeter(meter *ZKGasMeter) {
	evm.zkGasMeter = meter
	evm.zkGasFrames = nil
}
