package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// stickyExecutorSchedule is a small-budget zk-gas schedule sized so a
// single failed precompile call overshoots the block limit.
func stickyExecutorSchedule() *vm.ZkGasSchedule {
	s := &vm.ZkGasSchedule{
		BlockLimit: 1_000,
		SpawnEstimates: vm.SpawnEstimates{
			Call: 1, CallCode: 1, DelegateCall: 1, StaticCall: 1,
			Create: 1, Create2: 1,
		},
	}
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = 0
	}
	// point_evaluation (0x0a) charges 1×; the failed-precompile charge of
	// startGas (100_000) alone overshoots BlockLimit=1_000.
	s.PrecompileMultipliers = map[common.Address]uint16{{19: 0x0a}: 1}
	return s
}

// TestUnzenZkGas_StickyError_ApplyTransactionRevertsAndPropagates proves the
// sticky-error mechanism survives end-to-end through ApplyTransactionWithEVM:
// a tx whose precompile charge exceeds the zk-gas block limit must surface
// vm.ErrZkGasLimitExceeded as a Go error and the statedb must be reverted to
// the pre-tx intermediate root.
func TestUnzenZkGas_StickyError_ApplyTransactionRevertsAndPropagates(t *testing.T) {
	// Contract bytecode: STATICCALL to point_evaluation (0x0a) with 192 zero
	// bytes input and 100k gas. The precompile fails (bad proof), and the
	// full call gas is charged against the meter — that overshoots
	// BlockLimit=1000 and trips ErrZkGasLimitExceeded.
	contractCode := common.Hex2Bytes("6000600060c06000600a620186a0fa00")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	senderAddr := common.HexToAddress("0xaaaa000000000000000000000000000000000000")

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode, tracing.CodeChangeUnspecified)
	statedb.CreateAccount(senderAddr)
	statedb.AddBalance(senderAddr, uint256.NewInt(1_000_000_000_000_000_000), tracing.BalanceChangeUnspecified)
	statedb.Finalise(true)
	preStateRoot := statedb.IntermediateRoot(true)

	meter := vm.NewZkGasMeter(stickyExecutorSchedule())
	cfg := vm.Config{ZkGasMeter: meter}

	blockCtx := vm.BlockContext{
		CanTransfer: func(_ vm.StateDB, _ common.Address, _ *uint256.Int) bool { return true },
		Transfer:    func(_ vm.StateDB, _ common.Address, _ common.Address, _ *uint256.Int, _ *params.Rules) {},
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(0),
		GasLimit:    10_000_000,
	}
	evm := vm.NewEVM(blockCtx, statedb, params.MergedTestChainConfig, cfg)

	tx := types.NewTransaction(0, contractAddr, new(big.Int), 200_000, big.NewInt(1), nil)
	msg := &Message{
		From:                  senderAddr,
		To:                    &contractAddr,
		Nonce:                 0,
		Value:                 new(uint256.Int),
		GasLimit:              200_000,
		GasPrice:              uint256.NewInt(1),
		GasFeeCap:             uint256.NewInt(1),
		GasTipCap:             uint256.NewInt(1),
		Data:                  nil,
		SkipNonceChecks:       true,
		SkipTransactionChecks: true,
	}

	gp := NewGasPool(10_000_000)
	receipt, err := ApplyTransactionWithEVM(msg, gp, statedb, big.NewInt(1), common.Hash{}, 1, tx, evm)
	if err != vm.ErrZkGasLimitExceeded {
		t.Fatalf("ApplyTransactionWithEVM err = %v, want vm.ErrZkGasLimitExceeded", err)
	}
	if receipt != nil {
		t.Fatalf("expected nil receipt on zk-gas exhaustion, got %+v", receipt)
	}

	// Statedb must have been reverted: intermediate root matches the pre-tx root.
	postStateRoot := statedb.IntermediateRoot(true)
	if preStateRoot != postStateRoot {
		t.Fatalf("statedb not reverted: pre=%x post=%x", preStateRoot, postStateRoot)
	}
}

func TestUnzenZkGas_StickyError_DoesNotPoisonSystemCallAfterTruncation(t *testing.T) {
	contractCode := common.Hex2Bytes("6000600060c06000600a620186a0fa00")
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000000")
	senderAddr := common.HexToAddress("0xaaaa000000000000000000000000000000000000")

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode, tracing.CodeChangeUnspecified)
	statedb.CreateAccount(senderAddr)
	statedb.AddBalance(senderAddr, uint256.NewInt(1_000_000_000_000_000_000), tracing.BalanceChangeUnspecified)
	statedb.CreateAccount(params.WithdrawalQueueAddress)
	statedb.SetCode(params.WithdrawalQueueAddress, params.WithdrawalQueueCode, tracing.CodeChangeUnspecified)
	statedb.Finalise(true)

	meter := vm.NewZkGasMeter(stickyExecutorSchedule())
	evm := vm.NewEVM(vm.BlockContext{
		CanTransfer: func(_ vm.StateDB, _ common.Address, _ *uint256.Int) bool { return true },
		Transfer:    func(_ vm.StateDB, _ common.Address, _ common.Address, _ *uint256.Int, _ *params.Rules) {},
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		BlockNumber: big.NewInt(1),
		Time:        1,
		Random:      &common.Hash{},
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(0),
		GasLimit:    10_000_000,
	}, statedb, params.MergedTestChainConfig, vm.Config{ZkGasMeter: meter})

	tx := types.NewTransaction(0, contractAddr, new(big.Int), 200_000, big.NewInt(1), nil)
	msg := &Message{
		From:                  senderAddr,
		To:                    &contractAddr,
		Nonce:                 0,
		Value:                 new(uint256.Int),
		GasLimit:              200_000,
		GasPrice:              uint256.NewInt(1),
		GasFeeCap:             uint256.NewInt(1),
		GasTipCap:             uint256.NewInt(1),
		SkipNonceChecks:       true,
		SkipTransactionChecks: true,
	}

	if _, err := ApplyTransactionWithEVM(msg, NewGasPool(10_000_000), statedb, big.NewInt(1), common.Hash{}, 1, tx, evm); err != vm.ErrZkGasLimitExceeded {
		t.Fatalf("ApplyTransactionWithEVM err = %v, want vm.ErrZkGasLimitExceeded", err)
	}

	// This mirrors the truncation path before post-execution request collection:
	// the failed tx's in-flight meter state is discarded, and system calls reuse
	// the same EVM instance.
	meter.ResetTransaction()
	var requests [][]byte
	if err := ProcessWithdrawalQueue(&requests, evm); err != nil {
		t.Fatalf("ProcessWithdrawalQueue after zk-gas truncation returned error: %v", err)
	}
}
