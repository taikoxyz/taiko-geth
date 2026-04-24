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

func TestUnzenZkGasParity_EmptyCodeCallUsesCurrentAletheiaSpawnSemantics(t *testing.T) {
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

func TestUnzenZkGasParity_CreateOutOfFundsUsesCurrentAletheiaSpawnSemantics(t *testing.T) {
	code := common.Hex2Bytes("60016000600060006000f06000")
	got, want := executeUnzenZkGasParityCase(t, code, nil, func(_ common.Address, _ common.Address, value *uint256.Int) bool {
		return value.IsZero()
	})
	if got != want {
		t.Fatalf("TxZkGasUsed = %d, want %d", got, want)
	}
}

func TestUnzenZkGasParity_DepthExceededCallUsesCurrentAletheiaSpawnSemantics(t *testing.T) {
	meter := NewZkGasMeter(&UnzenZkGasSchedule)
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	blockCtx := BlockContext{
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
	}
	evm := NewEVM(blockCtx, statedb, params.MergedTestChainConfig, Config{ZkGasMeter: meter})

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
		if err := meter.ChargePrecompile(frame.to[19], frame.startGas-frame.leftOverGas); err != nil {
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
