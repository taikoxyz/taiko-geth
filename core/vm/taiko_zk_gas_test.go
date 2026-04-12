package vm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestZKGasMeterChargeOpcodeUsesMultiplier(t *testing.T) {
	meter := NewZKGasMeter()
	meter.StartTx()

	if err := meter.ChargeOpcode(ADD, 3, taikoZKGasInvocation{}); err != nil {
		t.Fatalf("ChargeOpcode returned error: %v", err)
	}
	if got, want := meter.TxUsed(), uint64(36); got != want {
		t.Fatalf("unexpected tx zk gas used, got %d want %d", got, want)
	}
}

func TestZKGasMeterChargeOpcodeUsesSpawnEstimateForCallPrecompile(t *testing.T) {
	meter := NewZKGasMeter()
	meter.StartTx()

	if err := meter.ChargeOpcode(CALL, 1, taikoZKGasInvocation{precompileInvoked: true}); err != nil {
		t.Fatalf("ChargeOpcode returned error: %v", err)
	}
	if got, want := meter.TxUsed(), uint64(312500); got != want {
		t.Fatalf("unexpected tx zk gas used, got %d want %d", got, want)
	}
}

func TestZKGasMeterOverflowReturnsErrZKGasLimitReached(t *testing.T) {
	meter := NewZKGasMeter()
	meter.StartTx()

	err := meter.ChargeOpcode(CALL, ^uint64(0), taikoZKGasInvocation{})
	if err != ErrZKGasLimitReached {
		t.Fatalf("unexpected error, got %v want %v", err, ErrZKGasLimitReached)
	}
}

func TestZKGasMeterBlockLimitReturnsErrZKGasLimitReached(t *testing.T) {
	meter := NewZKGasMeter()
	meter.blockUsed = BLOCK_ZK_GAS_LIMIT - 35
	meter.StartTx()

	err := meter.ChargeOpcode(ADD, 3, taikoZKGasInvocation{})
	if err != ErrZKGasLimitReached {
		t.Fatalf("unexpected error, got %v want %v", err, ErrZKGasLimitReached)
	}
}

func TestZKGasMeterUnknownEntriesFallbackToMaxUint16(t *testing.T) {
	meter := NewZKGasMeter()
	meter.StartTx()

	if err := meter.ChargeOpcode(OpCode(0xaa), 1, taikoZKGasInvocation{}); err != nil {
		t.Fatalf("ChargeOpcode returned error: %v", err)
	}
	if got, want := meter.TxUsed(), uint64(^uint16(0)); got != want {
		t.Fatalf("unexpected opcode fallback charge, got %d want %d", got, want)
	}

	meter.AbortTx()
	meter.StartTx()

	if err := meter.ChargePrecompile(common.BytesToAddress([]byte{0xfe}), 1); err != nil {
		t.Fatalf("ChargePrecompile returned error: %v", err)
	}
	if got, want := meter.TxUsed(), uint64(^uint16(0)); got != want {
		t.Fatalf("unexpected precompile fallback charge, got %d want %d", got, want)
	}
}

func TestInterpreterMetersOpcodeStepGas(t *testing.T) {
	evm := newZKGasTestEVM(t, []byte{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x02,
		byte(ADD),
		byte(STOP),
	})
	evm.zkGasMeter = NewZKGasMeter()
	evm.zkGasMeter.StartTx()

	_, _, err := evm.Call(common.Address{}, common.BytesToAddress([]byte("contract")), nil, 1_000_000, new(uint256.Int))
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if got, want := evm.zkGasMeter.TxUsed(), uint64(66); got != want {
		t.Fatalf("unexpected tx zk gas used, got %d want %d", got, want)
	}
}

func TestInterpreterUsesSpawnEstimateAndPrecompileCharge(t *testing.T) {
	evm := newZKGasTestEVM(t, []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x04,
		byte(PUSH1), 0xff,
		byte(CALL),
		byte(STOP),
	})
	evm.zkGasMeter = NewZKGasMeter()
	evm.zkGasMeter.StartTx()

	_, _, err := evm.Call(common.Address{}, common.BytesToAddress([]byte("contract")), nil, 1_000_000, new(uint256.Int))
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if got, want := evm.zkGasMeter.TxUsed(), uint64(312635); got != want {
		t.Fatalf("unexpected tx zk gas used, got %d want %d", got, want)
	}
}

func newZKGasTestEVM(t *testing.T, code []byte) *EVM {
	t.Helper()

	address := common.BytesToAddress([]byte("contract"))
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("state.New returned error: %v", err)
	}
	statedb.CreateAccount(address)
	statedb.SetCode(address, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	return NewEVM(BlockContext{
		BlockNumber: big.NewInt(1),
		Time:        1,
		Transfer: func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {
		},
	}, statedb, params.AllEthashProtocolChanges, Config{})
}
