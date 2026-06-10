package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

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

func TestZkGasStepTracker_OrdinaryOpcodeUsesNetStepGas(t *testing.T) {
	meter := NewZkGasMeter(testSchedule())
	tracker := NewZkGasStepTracker(meter)

	tracker.Begin(0, byte(ADD), 800)
	if err := tracker.FinishAndCharge(0, 700); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}

	if got, want := meter.TxZkGasUsed(), uint64(1_200); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestZkGasStepTracker_CreateSpawnUsesFixedEstimate(t *testing.T) {
	meter := NewZkGasMeter(testSchedule())
	tracker := NewZkGasStepTracker(meter)

	tracker.Begin(0, byte(CREATE), 10_000)
	tracker.MarkCreateSpawn(0)
	if err := tracker.FinishAndCharge(0, 100); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}

	if got, want := meter.TxZkGasUsed(), uint64(37_000); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestEVMSetZkGasMeterInitializesLateBoundTracker(t *testing.T) {
	meter := NewZkGasMeter(testSchedule())
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	code := common.Hex2Bytes("600160010100")

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{})

	if evm.zkGasTracker != nil {
		t.Fatalf("zkGasTracker initialized unexpectedly")
	}

	evm.SetZkGasMeter(meter)
	if evm.Config.ZkGasMeter != meter {
		t.Fatalf("ZkGasMeter was not installed on Config")
	}
	if evm.zkGasTracker == nil {
		t.Fatalf("zkGasTracker was not initialized")
	}

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)
	if _, _, err := evm.Call(common.Address{}, contractAddr, nil, 100_000, new(uint256.Int)); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}

	if got := meter.TxZkGasUsed(); got == 0 {
		t.Fatalf("TxZkGasUsed = %d, want non-zero after late meter attachment", got)
	}
}

func TestUnzenZkGasParity_EmptyCodeCallUsesConsensusSpawnSemantics(t *testing.T) {
	code := common.Hex2Bytes("60006000600060006000731111111111111111111111111111111111111111612710f100")
	got, want := executeUnzenZkGasParityCase(t, code, nil, nil)
	if got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_PrecompileCallMatchesCanonicalMeter(t *testing.T) {
	code := common.Hex2Bytes("63deadbeef600052600460006004601c60006004612710f100")
	got, want := executeUnzenZkGasParityCase(t, code, nil, nil)
	if got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_FailedPrecompileCallMatchesCanonicalMeter(t *testing.T) {
	// STATICCALL point_evaluation(0x0a) with 100k gas and zeroed 192-byte input.
	// The precompile fails, but the caller only observes ok=false and continues.
	code := common.Hex2Bytes("6000600060c06000600a620186a0fa00")
	got, want := executeUnzenZkGasParityCase(t, code, nil, nil)
	if got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_StaticLogWriteProtectionUsesRevmStaticGas(t *testing.T) {
	schedule := &ZkGasSchedule{BlockLimit: 1_000_000}
	schedule.OpcodeMultipliers[byte(LOG1)] = 7

	meter := NewZkGasMeter(schedule)
	targetAddr := common.HexToAddress("0x2000000000000000000000000000000000000000")
	outerAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	// STATICCALL(gas=100000, target=targetAddr, in=0:0, out=0:0), then STOP.
	outerCode := append(common.Hex2Bytes("600060006000600073"), targetAddr.Bytes()...)
	outerCode = append(outerCode, common.Hex2Bytes("620186a0fa00")...)
	// PUSH1 0, PUSH1 0, PUSH1 0, LOG1. In a static frame this halts before
	// REVM charges LOG1 topic/data gas, leaving only LogGas as the raw step gas.
	targetCode := common.Hex2Bytes("600060006000a1")

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(outerAddr)
	statedb.SetCode(outerAddr, outerCode, tracing.CodeChangeUnspecified)
	statedb.CreateAccount(targetAddr)
	statedb.SetCode(targetAddr, targetCode, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
	statedb.Prepare(rules, common.Address{}, common.Address{}, &outerAddr, ActivePrecompiles(rules), nil)

	if _, _, err := evm.Call(common.Address{}, outerAddr, nil, 200_000, new(uint256.Int)); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if got, want := meter.TxZkGasUsed(), params.LogGas*7; got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_StaticCreateWriteProtectionUsesRevmStaticGas(t *testing.T) {
	for _, tt := range []struct {
		name       string
		opcode     OpCode
		targetCode []byte
	}{
		{
			name:       "create",
			opcode:     CREATE,
			targetCode: common.Hex2Bytes("600060006000f0"),
		},
		{
			name:       "create2",
			opcode:     CREATE2,
			targetCode: common.Hex2Bytes("6000600060006000f5"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			schedule := &ZkGasSchedule{BlockLimit: 1_000_000}
			schedule.OpcodeMultipliers[byte(tt.opcode)] = 1

			meter := NewZkGasMeter(schedule)
			targetAddr := common.HexToAddress("0x2000000000000000000000000000000000000000")
			outerAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
			// STATICCALL(gas=100000, target=targetAddr, in=0:0, out=0:0), then STOP.
			outerCode := append(common.Hex2Bytes("600060006000600073"), targetAddr.Bytes()...)
			outerCode = append(outerCode, common.Hex2Bytes("620186a0fa00")...)

			rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			statedb.CreateAccount(outerAddr)
			statedb.SetCode(outerAddr, outerCode, tracing.CodeChangeUnspecified)
			statedb.CreateAccount(targetAddr)
			statedb.SetCode(targetAddr, tt.targetCode, tracing.CodeChangeUnspecified)
			statedb.Finalise(true)

			evm := NewEVM(BlockContext{
				CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
				Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
				BlockNumber: big.NewInt(1),
				Time:        1,
				Random:      &common.Hash{},
			}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
			statedb.Prepare(rules, common.Address{}, common.Address{}, &outerAddr, ActivePrecompiles(rules), nil)

			if _, _, err := evm.Call(common.Address{}, outerAddr, nil, 200_000, new(uint256.Int)); err != nil {
				t.Fatalf("Call returned error: %v", err)
			}
			if got, want := meter.TxZkGasUsed(), uint64(0); got != want {
				t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
			}
		})
	}
}

func TestUnzenZkGasParity_NestedCallDoesNotLeakSpawnState(t *testing.T) {
	innerAddr := common.HexToAddress("0x2000000000000000000000000000000000000000")
	outerCode := common.Hex2Bytes("60006000600060006000732000000000000000000000000000000000000000612710f100")
	innerCode := common.Hex2Bytes("60006000600060006000732222222222222222222222222222222222222222612710f100")

	got, want := executeUnzenZkGasParityCase(t, outerCode, map[common.Address][]byte{
		innerAddr: innerCode,
	}, nil)
	if got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_CreateOutOfFundsUsesConsensusSpawnSemantics(t *testing.T) {
	code := common.Hex2Bytes("60016000600060006000f06000")
	got, want := executeUnzenZkGasParityCase(t, code, nil, func(_ common.Address, _ common.Address, value *uint256.Int) bool {
		return value.IsZero()
	})
	if got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_EmptyCodeCallShortCircuitUsesSpawnEstimate(t *testing.T) {
	evm, meter := newUnzenShortCircuitEVM(t, nil)

	evm.zkGasTracker.Begin(0, byte(CALL), 1_000)
	_, gasLeft, err := evm.Call(common.Address{}, common.Address{0x11}, nil, 1_000, new(uint256.Int))
	if err != nil {
		t.Fatalf("Call error = %v, want nil", err)
	}
	if gasLeft != 1_000 {
		t.Fatalf("gasLeft = %d, want %d", gasLeft, 1_000)
	}
	if err := evm.zkGasTracker.FinishAndCharge(0, gasLeft); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}
	want := UnzenZkGasSchedule.SpawnEstimates.Call * uint64(UnzenZkGasSchedule.OpcodeMultipliers[byte(CALL)])
	if got := meter.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_EmptyCodeStaticCallShortCircuitUsesSpawnEstimate(t *testing.T) {
	evm, meter := newUnzenShortCircuitEVM(t, nil)

	evm.zkGasTracker.Begin(0, byte(STATICCALL), 1_000)
	_, gasLeft, err := evm.StaticCall(common.Address{}, common.Address{0xfe}, nil, 1_000)
	if err != nil {
		t.Fatalf("StaticCall error = %v, want nil", err)
	}
	if gasLeft != 1_000 {
		t.Fatalf("gasLeft = %d, want %d", gasLeft, 1_000)
	}
	if err := evm.zkGasTracker.FinishAndCharge(0, gasLeft); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}
	want := UnzenZkGasSchedule.SpawnEstimates.StaticCall * uint64(UnzenZkGasSchedule.OpcodeMultipliers[byte(STATICCALL)])
	if got := meter.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_CallOutOfFundsShortCircuitUsesSpawnEstimate(t *testing.T) {
	evm, meter := newUnzenShortCircuitEVM(t, func(StateDB, common.Address, *uint256.Int) bool {
		return false
	})

	evm.zkGasTracker.Begin(0, byte(CALL), 1_000)
	_, gasLeft, err := evm.Call(common.Address{}, common.Address{0x11}, nil, 1_000, uint256.NewInt(1))
	if err != ErrInsufficientBalance {
		t.Fatalf("Call error = %v, want %v", err, ErrInsufficientBalance)
	}
	if gasLeft != 1_000 {
		t.Fatalf("gasLeft = %d, want %d", gasLeft, 1_000)
	}
	if err := evm.zkGasTracker.FinishAndCharge(0, gasLeft); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}
	want := UnzenZkGasSchedule.SpawnEstimates.Call * uint64(UnzenZkGasSchedule.OpcodeMultipliers[byte(CALL)])
	if got := meter.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_CreateOutOfFundsShortCircuitUsesSpawnEstimate(t *testing.T) {
	evm, meter := newUnzenShortCircuitEVM(t, func(StateDB, common.Address, *uint256.Int) bool {
		return false
	})

	evm.zkGasTracker.Begin(0, byte(CREATE), 1_000)
	_, _, gasLeft, err := evm.Create(common.Address{}, []byte{byte(STOP)}, 1_000, uint256.NewInt(1))
	if err != ErrInsufficientBalance {
		t.Fatalf("Create error = %v, want %v", err, ErrInsufficientBalance)
	}
	if gasLeft != 1_000 {
		t.Fatalf("gasLeft = %d, want %d", gasLeft, 1_000)
	}
	if err := evm.zkGasTracker.FinishAndCharge(0, gasLeft); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}
	want := UnzenZkGasSchedule.SpawnEstimates.Create * uint64(UnzenZkGasSchedule.OpcodeMultipliers[byte(CREATE)])
	if got := meter.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_DepthExceededCallShortCircuitUsesSpawnEstimate(t *testing.T) {
	// Depth-exceeded charging is extrapolated from the Rust reference EVM's
	// deferred spawn-callback model because the source path does not expose a
	// direct callback contract for this short-circuit.
	evm, meter := newUnzenShortCircuitEVM(t, nil)
	depth := int(params.CallCreateDepth) + 1
	evm.depth = depth
	evm.zkGasTracker.Begin(depth, byte(CALL), 1_000)

	_, gasLeft, err := evm.Call(common.Address{}, common.Address{0x11}, nil, 1_000, new(uint256.Int))
	if err != ErrDepth {
		t.Fatalf("Call error = %v, want %v", err, ErrDepth)
	}
	if gasLeft != 1_000 {
		t.Fatalf("gasLeft = %d, want %d", gasLeft, 1_000)
	}
	if err := evm.zkGasTracker.FinishAndCharge(depth, gasLeft); err != nil {
		t.Fatalf("FinishAndCharge returned error: %v", err)
	}
	want := UnzenZkGasSchedule.SpawnEstimates.Call * uint64(UnzenZkGasSchedule.OpcodeMultipliers[byte(CALL)])
	if got := meter.TxZkGasUsed(); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGas_StaticGasOutOfGasChargesFullPreStepGas(t *testing.T) {
	// Static-gas OOG spends all remaining frame gas, so zk accounting must
	// charge the full pre-step gas. ADD has constantGas=3; with only 2 gas
	// remaining the static check fails before any deduction.
	schedule := &ZkGasSchedule{BlockLimit: 1_000_000}
	schedule.OpcodeMultipliers[byte(ADD)] = 5
	meter := NewZkGasMeter(schedule)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	// PUSH1 1 PUSH1 2 ADD STOP. PUSH1 costs 3 gas each; with 8 gas the second
	// PUSH1 leaves 2 gas, then ADD's static cost of 3 trips static-gas OOG.
	code := common.Hex2Bytes("600160020100")

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	if _, _, err := evm.Call(common.Address{}, contractAddr, nil, 8, new(uint256.Int)); err != ErrOutOfGas {
		t.Fatalf("Call error = %v, want %v", err, ErrOutOfGas)
	}
	if got, want := meter.TxZkGasUsed(), uint64(2*5); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGas_MemoryExpansionOutOfGasChargesOnlyStaticGas(t *testing.T) {
	// REVM memory-resize OOG does not spend all remaining gas: MSTORE has
	// staticGas=3, then memory expansion needs another 3 gas; with only 5 gas
	// before MSTORE, the dynamic check fails after static gas is deducted, and
	// the reference inspector observes 2 gas remaining in step_end.
	schedule := &ZkGasSchedule{BlockLimit: 1_000_000}
	schedule.OpcodeMultipliers[byte(MSTORE)] = 7
	meter := NewZkGasMeter(schedule)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	// PUSH1 0xff PUSH1 0x00 MSTORE STOP. With 11 gas, the two PUSH1 opcodes
	// leave 5 gas for MSTORE, which then trips dynamic memory-expansion OOG.
	code := common.Hex2Bytes("60ff60005200")

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	if _, _, err := evm.Call(common.Address{}, contractAddr, nil, 11, new(uint256.Int)); err != ErrOutOfGas {
		t.Fatalf("Call error = %v, want %v", err, ErrOutOfGas)
	}
	if got, want := meter.TxZkGasUsed(), uint64(3*7); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGas_Block4796MstoreOverflowChargesStaticGas(t *testing.T) {
	// Block 4796 tx 1 fails at pc=2451 on MSTORE with InvalidOperandOOG.
	// alethia-reth observes the MSTORE step_end after static gas is spent, so
	// the block difficulty includes one extra MSTORE static charge: 3 * 22.
	schedule := &ZkGasSchedule{BlockLimit: 1_000_000}
	schedule.OpcodeMultipliers[byte(MSTORE)] = 22
	meter := NewZkGasMeter(schedule)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	// PUSH1 0x21 PUSH32 0x8000...0000 MSTORE STOP. The offset cannot fit in
	// uint64, so go-ethereum reports ErrGasUintOverflow before resizing memory.
	code := common.Hex2Bytes("60217f80000000000000000000000000000000000000000000000000000000000000005200")

	rules := params.MergedTestChainConfig.Rules(big.NewInt(4796), true, 1777923078)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(4796),
		Time:        1777923078,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	if _, _, err := evm.Call(common.Address{}, contractAddr, nil, 100_000, new(uint256.Int)); err != ErrGasUintOverflow {
		t.Fatalf("Call error = %v, want %v", err, ErrGasUintOverflow)
	}
	if got, want := meter.TxZkGasUsed(), uint64(3*22); got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGas_KeccakMemoryExpansionOutOfGasChargesPreResizeGas(t *testing.T) {
	// KECCAK256 charges static gas and per-word hash gas before memory resize.
	// With 45 gas before KECCAK256, REVM charges 30 static + 12 hash gas, then
	// memory resize needs another 6 gas and fails with 3 gas still remaining.
	schedule := &ZkGasSchedule{BlockLimit: 1_000_000}
	schedule.OpcodeMultipliers[byte(KECCAK256)] = 7
	meter := NewZkGasMeter(schedule)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	// PUSH1 0x40 PUSH1 0x00 KECCAK256 STOP. The two PUSH1 opcodes leave 45 gas
	// for KECCAK256.
	code := common.Hex2Bytes("604060002000")

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	if _, _, err := evm.Call(common.Address{}, contractAddr, nil, 51, new(uint256.Int)); err != ErrOutOfGas {
		t.Fatalf("Call error = %v, want %v", err, ErrOutOfGas)
	}
	if got, want := meter.TxZkGasUsed(), (params.Keccak256Gas+2*params.Keccak256WordGas)*7; got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGas_CallMemoryExpansionOutOfGasChargesOnlyStaticGas(t *testing.T) {
	// CALL resolves input/output memory before account/call gas. The target is
	// a warm precompile, but memory resize fails before dispatch, so the CALL is
	// charged as a non-spawn step with only its static warm-access gas.
	schedule := &ZkGasSchedule{BlockLimit: 1_000_000}
	schedule.OpcodeMultipliers[byte(CALL)] = 7
	meter := NewZkGasMeter(schedule)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	// CALL(gas=0, to=0x01, value=0, in=0:32, out=0:0). The seven PUSH1
	// opcodes leave 102 gas for CALL: enough for the 100 static gas, but not
	// enough for the 3 gas input-memory expansion.
	code := common.Hex2Bytes("6000600060206000600060016000f100")

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	if _, _, err := evm.Call(common.Address{}, contractAddr, nil, 123, new(uint256.Int)); !errors.Is(err, ErrOutOfGas) {
		t.Fatalf("Call error = %v, want %v", err, ErrOutOfGas)
	}
	if got, want := meter.TxZkGasUsed(), params.WarmStorageReadCostEIP2929*7; got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func newUnzenShortCircuitEVM(t *testing.T, canTransfer func(StateDB, common.Address, *uint256.Int) bool) (*EVM, *ZkGasMeter) {
	t.Helper()

	if canTransfer == nil {
		canTransfer = func(StateDB, common.Address, *uint256.Int) bool { return true }
	}
	meter := NewZkGasMeter(&UnzenZkGasSchedule)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	blockCtx := BlockContext{
		CanTransfer: canTransfer,
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}
	evm := NewEVM(blockCtx, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
	return evm, meter
}

func TestUnzenZkGas_FailedPrecompileExceedingBlockLimit_StickyError(t *testing.T) {
	// PUSH1 0 PUSH1 0 PUSH1 0xc0 PUSH1 0 PUSH1 0x0a PUSH3 0x0186a0 STATICCALL
	// STOP STOP — second STOP marks "did the loop continue past the failing
	// STATICCALL?". With the sticky check in place, only the first STOP could
	// run, but actually neither STOP runs because the loop exits at top-of-loop.
	code := common.Hex2Bytes("6000600060c06000600a620186a0fa0000")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(stickySchedule())
	var observedOps []byte
	tracer := &tracing.Hooks{
		OnOpcode: func(_ uint64, op byte, _ uint64, _ uint64, _ tracing.OpContext, _ []byte, _ int, _ error) {
			observedOps = append(observedOps, op)
		},
	}
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter, Tracer: tracer})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
	// The STATICCALL itself ran (it's the opcode that triggered the over-limit
	// precompile charge), but the post-STATICCALL STOP must NOT have run — the
	// top-of-loop sticky check exits the frame before the next dispatch.
	var sawStaticCall bool
	for _, op := range observedOps {
		if op == 0xfa { // STATICCALL
			sawStaticCall = true
		}
		if op == 0x00 { // STOP
			t.Fatalf("STOP after over-limit STATICCALL was dispatched; sticky check failed to short-circuit. ops=%v", observedOps)
		}
	}
	if !sawStaticCall {
		t.Fatalf("STATICCALL was not dispatched; test would pass for the wrong reason. ops=%v", observedOps)
	}
}

func TestUnzenZkGas_SuccessfulPrecompileExceedingBlockLimit_StickyError(t *testing.T) {
	// STATICCALL to identity (0x04) with zero-length input. Identity always
	// succeeds, so this exercises the success path of ChargePrecompile.
	// PUSH1 0 PUSH1 32 PUSH1 0 PUSH1 0 PUSH1 0x04 PUSH3 0x018680 STATICCALL STOP
	code := common.Hex2Bytes("6000602060006000600462018680fa00")

	schedule := stickySchedule()
	// Identity precompile multiplier sized so even minimal gas use overshoots BlockLimit=1000.
	schedule.PrecompileMultipliers[common.Address{19: 0x04}] = 10_000

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
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
}

func TestUnzenZkGas_InnerFrameOpcodeExceedingBlockLimit_StickyError(t *testing.T) {
	// Outer contract CALLs into innerAddr. Inner code does ADD which trips
	// FinishAndCharge inside the inner frame. The outer opCall swallows the
	// Go error (ok=false on stack), but the sticky slot survives and aborts
	// the outer frame at the next top-of-loop check.
	innerAddr := common.HexToAddress("0x2000000000000000000000000000000000000000")

	// Outer: PUSH1 0 PUSH1 0 PUSH1 0 PUSH1 0 PUSH1 0 PUSH20 inner PUSH3 0x0186a0 CALL STOP
	outerCode := common.Hex2Bytes("60006000600060006000732000000000000000000000000000000000000000620186a0f100")
	// Inner: PUSH1 1 PUSH1 2 ADD STOP — ADD trips the over-limit FinishAndCharge.
	// Use the corrected 6-byte form (no stray ADD).
	innerCode := common.Hex2Bytes("600160020100")

	schedule := stickySchedule()
	schedule.OpcodeMultipliers[0x01] = 1024 // ADD overshoots BlockLimit=1000.
	schedule.OpcodeMultipliers[byte(CALL)] = 1

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, outerCode, tracing.CodeChangeUnspecified)
	statedb.CreateAccount(innerAddr)
	statedb.SetCode(innerAddr, innerCode, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := NewZkGasMeter(schedule)
	type opEvent struct {
		op    byte
		depth int
	}
	var observed []opEvent
	tracer := &tracing.Hooks{
		OnOpcode: func(_ uint64, op byte, _ uint64, _ uint64, _ tracing.OpContext, _ []byte, depth int, _ error) {
			observed = append(observed, opEvent{op: op, depth: depth})
		},
	}
	evm := NewEVM(BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter, Tracer: tracer})

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	_, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int))
	if err != ErrZkGasLimitExceeded {
		t.Fatalf("Call err = %v, want ErrZkGasLimitExceeded", err)
	}
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
	if got := meter.TxZkGasUsed(); got != 0 {
		t.Fatalf("TxZkGasUsed = %d, want 0 after inner-frame zk-gas failure", got)
	}
	// Defense-in-depth: prove the test exercises the right path.
	// (1) The inner ADD must have dispatched — otherwise the over-limit charge
	// never came from FinishAndCharge in the inner frame.
	// (2) The outer STOP must NOT have dispatched — otherwise the outer
	// frame ran past CALL, which would mean the sticky check failed to fire.
	var sawInnerAdd bool
	for _, ev := range observed {
		if ev.op == 0x01 && ev.depth >= 2 { // ADD inside the inner frame
			sawInnerAdd = true
		}
		if ev.op == 0x00 && ev.depth == 1 { // STOP at outer-frame depth
			t.Fatalf("outer STOP dispatched after CALL; sticky check failed to short-circuit. ops=%v", observed)
		}
	}
	if !sawInnerAdd {
		t.Fatalf("inner ADD was not dispatched; over-limit charge did not originate in inner frame. ops=%v", observed)
	}
}

func executeUnzenZkGasParityCase(t *testing.T, code []byte, extraContracts map[common.Address][]byte, canTransfer func(common.Address, common.Address, *uint256.Int) bool) (uint64, uint64) {
	t.Helper()

	rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
	collector := newZkGasTraceCollector(activePrecompiledContracts(rules))
	meter := NewZkGasMeter(&UnzenZkGasSchedule)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	for addr, contractCode := range extraContracts {
		statedb.CreateAccount(addr)
		statedb.SetCode(addr, contractCode, tracing.CodeChangeUnspecified)
	}
	statedb.Finalise(true)

	if canTransfer == nil {
		canTransfer = func(common.Address, common.Address, *uint256.Int) bool { return true }
	}
	blockCtx := BlockContext{
		CanTransfer: func(db StateDB, caller common.Address, value *uint256.Int) bool {
			return canTransfer(caller, common.Address{}, value)
		},
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}
	evm := NewEVM(blockCtx, statedb, params.MergedTestChainConfig, Config{
		Tracer:     collector.Hooks(),
		ZkGasMeter: meter,
	})
	statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

	if _, _, err := evm.Call(common.Address{}, contractAddr, nil, 200_000, new(uint256.Int)); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}

	want, err := collector.CanonicalTxZkGas(&UnzenZkGasSchedule)
	if err != nil {
		t.Fatalf("CanonicalTxZkGas returned error: %v", err)
	}
	return meter.TxZkGasUsed(), want
}

type zkGasTraceFrame struct {
	parent      int
	depth       int
	startGas    uint64
	leftOverGas uint64
	enterSeq    int
	exitSeq     int
	to          common.Address
	precompile  bool
	opcodeCount int
}

type zkGasTraceOp struct {
	frame     int
	opcode    byte
	gasBefore uint64
	seq       int
}

type zkGasTraceCollector struct {
	frames      []zkGasTraceFrame
	ops         []zkGasTraceOp
	stack       []int
	seq         int
	precompiles map[common.Address]PrecompiledContract
}

func newZkGasTraceCollector(precompiles map[common.Address]PrecompiledContract) *zkGasTraceCollector {
	return &zkGasTraceCollector{precompiles: precompiles}
}

func (c *zkGasTraceCollector) Hooks() *tracing.Hooks {
	return &tracing.Hooks{
		OnEnter: func(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
			parent := -1
			if len(c.stack) > 0 {
				parent = c.stack[len(c.stack)-1]
			}
			c.frames = append(c.frames, zkGasTraceFrame{
				parent:     parent,
				depth:      depth,
				startGas:   gas,
				enterSeq:   c.seq,
				to:         to,
				precompile: c.isPrecompile(to),
			})
			c.stack = append(c.stack, len(c.frames)-1)
			c.seq++
		},
		OnExit: func(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
			frameID := c.stack[len(c.stack)-1]
			c.stack = c.stack[:len(c.stack)-1]

			frame := &c.frames[frameID]
			frame.leftOverGas = frame.startGas - gasUsed
			frame.exitSeq = c.seq
			c.seq++
		},
		OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			frameID := c.stack[len(c.stack)-1]
			c.frames[frameID].opcodeCount++
			c.ops = append(c.ops, zkGasTraceOp{
				frame:     frameID,
				opcode:    op,
				gasBefore: gas,
				seq:       c.seq,
			})
			c.seq++
		},
	}
}

func (c *zkGasTraceCollector) CanonicalTxZkGas(schedule *ZkGasSchedule) (uint64, error) {
	meter := NewZkGasMeter(schedule)
	for idx, op := range c.ops {
		gasAfter := c.gasAfter(idx)
		rawGas := op.gasBefore - gasAfter
		if IsSpawnOpcode(OpCode(op.opcode)) && c.spawned(idx) {
			rawGas = meter.SpawnEstimate(op.opcode)
		}
		if err := meter.ChargeOpcode(op.opcode, rawGas); err != nil {
			return 0, err
		}
	}
	for _, frame := range c.frames {
		if !frame.precompile {
			continue
		}
		if err := meter.ChargePrecompile(frame.to, frame.startGas-frame.leftOverGas); err != nil {
			return 0, err
		}
	}
	return meter.TxZkGasUsed(), nil
}

func (c *zkGasTraceCollector) gasAfter(idx int) uint64 {
	current := c.ops[idx]
	for next := idx + 1; next < len(c.ops); next++ {
		if c.ops[next].frame == current.frame {
			return c.ops[next].gasBefore
		}
	}
	return c.frames[current.frame].leftOverGas
}

func (c *zkGasTraceCollector) spawned(idx int) bool {
	current := c.ops[idx]
	frame := c.frames[current.frame]
	boundary := frame.exitSeq
	for next := idx + 1; next < len(c.ops); next++ {
		if c.ops[next].frame == current.frame {
			boundary = c.ops[next].seq
			break
		}
	}
	for _, child := range c.frames {
		if child.parent != current.frame {
			continue
		}
		if child.enterSeq > current.seq && child.enterSeq < boundary {
			return true
		}
	}
	return false
}

func (c *zkGasTraceCollector) isPrecompile(addr common.Address) bool {
	_, ok := c.precompiles[addr]
	return ok
}

// CHANGE(taiko): TestUnzenZkGas_PreExecutionFailuresMirrorRevmStaticGas pins
// the zk gas charged for opcodes that fail before go-ethereum deducts any
// gas (stack underflow/overflow and REVM's not-activated opcodes). REVM
// deducts its instruction-table static gas in step() before the instruction
// body runs, so these steps must still charge static-gas zk gas. Every
// expected value below was probed against alethia-reth under the Unzen
// schedule.
func TestUnzenZkGas_PreExecutionFailuresMirrorRevmStaticGas(t *testing.T) {
	push0Overflow := make([]byte, 1025)
	for i := range push0Overflow {
		push0Overflow[i] = byte(PUSH0)
	}

	tests := []struct {
		name      string
		code      []byte
		gas       uint64
		wantZkGas uint64
	}{
		// 3 static * 19 multiplier.
		{"add stack underflow", []byte{byte(ADD)}, 100_000, 57},
		// REVM SLOAD static gas is 100 post-Berlin (go-ethereum carries it as dynamic gas): 100 * 3.
		{"sload stack underflow", []byte{byte(SLOAD)}, 100_000, 300},
		// REVM LOG0 static gas is 375 (go-ethereum carries it as dynamic gas): 375 * 3.
		{"log0 stack underflow", []byte{byte(LOG0)}, 100_000, 1125},
		// REVM EXP static gas is 10 (go-ethereum carries it as dynamic gas): 10 * 21.
		{"exp stack underflow", []byte{byte(EXP)}, 100_000, 210},
		// REVM CREATE static gas is 0 (the 32000 is charged inside the instruction body).
		{"create stack underflow", []byte{byte(CREATE)}, 100_000, 0},
		// 3 static * 31 multiplier.
		{"swap1 stack underflow", []byte{byte(SWAP1)}, 100_000, 93},
		// Static gas exceeds the 2 gas remaining, so REVM hits OOG first and
		// spends everything: 2 * 19.
		{"add stack underflow with unpayable static gas", []byte{byte(ADD)}, 2, 38},
		// DUPN halts NotActivated in REVM after its 3 static gas is deducted;
		// the fail-safe multiplier applies: 3 * 65535.
		{"dupn not activated", []byte{0xe6}, 100_000, 196_605},
		// SLOTNUM static gas is 2: 2 * 65535.
		{"slotnum not activated", []byte{0x4b}, 100_000, 131_070},
		// 1024 successful PUSH0 steps plus the overflowing one all charge 2 * 13.
		{"push0 stack overflow", push0Overflow, 1_000_000, 26_650},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meter := NewZkGasMeter(&UnzenZkGasSchedule)
			contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")

			rules := params.MergedTestChainConfig.Rules(big.NewInt(1), true, 1)
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			statedb.CreateAccount(contractAddr)
			statedb.SetCode(contractAddr, tt.code, tracing.CodeChangeUnspecified)
			statedb.Finalise(true)

			evm := NewEVM(BlockContext{
				CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
				Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
				BlockNumber: big.NewInt(1),
				Time:        1,
				Random:      &common.Hash{},
			}, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})
			statedb.Prepare(rules, common.Address{}, common.Address{}, &contractAddr, ActivePrecompiles(rules), nil)

			if _, _, err := evm.Call(common.Address{}, contractAddr, nil, tt.gas, new(uint256.Int)); err == nil {
				t.Fatalf("Call succeeded, want a pre-execution failure")
			}
			if got := meter.TxZkGasUsed(); got != tt.wantZkGas {
				t.Fatalf("TxZkGasUsed = %d, want %d", got, tt.wantZkGas)
			}
		})
	}
}
