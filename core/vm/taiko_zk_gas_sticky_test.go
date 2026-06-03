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

func TestEVMSetZkGasErr_IdempotentWhenSlotEmpty(t *testing.T) {
	evm := &EVM{}
	evm.setZkGasErr()
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
}

func TestEVMSetZkGasErr_DoesNotClobberExistingError(t *testing.T) {
	evm := &EVM{zkGasErr: ErrZkGasLimitExceeded}
	evm.setZkGasErr()
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded preserved", evm.zkGasErr)
	}
}

func TestEVMResetZkGasErr_ClearsSlot(t *testing.T) {
	evm := &EVM{zkGasErr: ErrZkGasLimitExceeded}
	evm.ResetZkGasErr()
	if evm.zkGasErr != nil {
		t.Fatalf("zkGasErr = %v, want nil after reset", evm.zkGasErr)
	}
}

// stickySchedule returns a tiny zk-gas schedule sized so a single failed
// precompile call will exceed BlockLimit, exercising the over-limit path.
func stickySchedule() *ZkGasSchedule {
	s := &ZkGasSchedule{
		BlockLimit: 1_000,
		SpawnEstimates: SpawnEstimates{
			Call: 1, CallCode: 1, DelegateCall: 1, StaticCall: 1,
			Create: 1, Create2: 1,
		},
	}
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = 0
	}
	// Every canonical precompile (0x01..0x13) charges 1×; the failed-precompile
	// charge of startGas (100_000) alone overshoots BlockLimit=1_000.
	s.PrecompileMultipliers = make(map[common.Address]uint16)
	for b := byte(0x01); b <= 0x13; b++ {
		s.PrecompileMultipliers[common.Address{19: b}] = 1
	}
	return s
}

func TestEVMCall_PrecompileOverLimit_SetsStickyError(t *testing.T) {
	// STATICCALL to point_evaluation (0x0a) with bad input → precompile fails →
	// full call gas (100k) is charged, which overflows BlockLimit=1000.
	code := common.Hex2Bytes("6000600060c06000600a620186a0fa00")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(stickySchedule())
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, _ = evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))

	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded set after over-limit precompile", evm.zkGasErr)
	}
	if got := meter.TxZkGasUsed(); got != 0 {
		t.Fatalf("TxZkGasUsed = %d, want 0 (over-limit charge must be rejected, not committed)", got)
	}
}

func TestEVMCall_PrecompileOverLimit_RevertsCallSnapshot(t *testing.T) {
	// A zero-value CALL to an existing precompile may create the account before
	// the precompile runs. If the subsequent zk-gas charge exceeds the limit,
	// that account creation must be reverted with the call-frame snapshot.
	precompileAddr := common.HexToAddress("0x04")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.Finalise(true)

	schedule := &ZkGasSchedule{
		BlockLimit:            1,
		PrecompileMultipliers: map[common.Address]uint16{{19: 0x04}: 1},
	}
	meter := NewZkGasMeter(schedule)
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &precompileAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, precompileAddr, []byte{1, 2, 3, 4}, 100_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if statedb.Exist(precompileAddr) {
		t.Fatalf("precompile account exists after over-limit CALL; call snapshot was not reverted")
	}
}

func TestRun_FinishAndChargeOverLimit_SetsStickyError(t *testing.T) {
	// ADD opcode (0x01) with multiplier=1024 → first ADD at depth 0 will trip
	// BlockLimit=1000 immediately. Use schedule with non-trivial ADD multiplier
	// to force overflow on the first arithmetic step.
	schedule := stickySchedule()
	schedule.OpcodeMultipliers[0x01] = 1024 // ADD: cost = ADD_gas(3) * 1024 = 3072 > 1000

	// PUSH1 1 PUSH1 2 ADD STOP — overflows on the ADD step.
	code := common.Hex2Bytes("600160020100")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(schedule)
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded set after FinishAndCharge over-limit", evm.zkGasErr)
	}
}

func TestEVMResetZkGasErr_CalledPerTxClearsSlot(t *testing.T) {
	// Simulate the state_processor's per-tx pattern: tx 1 sets slot, executor
	// calls ResetZkGasErr, tx 2 sees a clean slot.
	evm := &EVM{}
	evm.setZkGasErr()
	if evm.zkGasErr == nil {
		t.Fatalf("setZkGasErr did not set slot")
	}
	evm.ResetZkGasErr()
	if evm.zkGasErr != nil {
		t.Fatalf("ResetZkGasErr did not clear slot; zkGasErr = %v", evm.zkGasErr)
	}
}
