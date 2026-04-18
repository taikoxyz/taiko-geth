// Copyright 2026 The Taiko Authors
// This file is part of the go-ethereum library.
//
// Licensed under the GNU Lesser General Public License v3 or later.

package core

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// CHANGE(taiko): uzenTestChainConfig returns a Taiko chain config with Uzen (and
// all prior forks) active from genesis. Used by Uzen truncation parity tests.
func uzenTestChainConfig(t *testing.T) *params.ChainConfig {
	t.Helper()
	zero := uint64(0)
	cfg := *params.MergedTestChainConfig
	cfg.Taiko = true
	cfg.ChainID = big.NewInt(167000)
	cfg.UzenTime = &zero
	cfg.OsakaTime = &zero
	return &cfg
}

// CHANGE(taiko): uzenTestScheduleExhausting returns a zk gas schedule whose
// BlockLimit is zero, so the very first opcode that would consume non-zero
// zk gas exhausts the budget.
func uzenTestScheduleExhausting() *vm.ZkGasSchedule {
	s := &vm.ZkGasSchedule{BlockLimit: 0}
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = math.MaxUint16
	}
	for i := range s.PrecompileMultipliers {
		s.PrecompileMultipliers[i] = math.MaxUint16
	}
	return s
}

// CHANGE(taiko): TestApplyTransactionWithEVM_ZkGasExhausted_RevertsAndReturnsError
// documents the expected alethia-reth-parity behavior: when the interpreter
// surfaces a zk-gas-limit error via result.Err, ApplyTransactionWithEVM should
// revert all tx-level state mutations (nonce, balance, gas pool) and return
// vm.ErrZkGasLimitExceeded as a Go error. Without the fix in Task 2, this test
// fails because the function builds a failed-status receipt and returns nil err.
func TestApplyTransactionWithEVM_ZkGasExhausted_RevertsAndReturnsError(t *testing.T) {
	var (
		chainConfig    = uzenTestChainConfig(t)
		senderKey, _   = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		sender         = crypto.PubkeyToAddress(senderKey.PublicKey)
		callee         = common.HexToAddress("0x000000000000000000000000000000000000c0de")
		initialBalance = new(big.Int).SetUint64(1_000_000_000_000_000_000) // 1 ETH
		initialNonce   = uint64(5)
	)

	// A minimal contract body: ADD; STOP. Any opcode that would charge non-zero
	// zk gas exhausts under the zero-budget schedule.
	calleeCode := []byte{0x60, 0x01, 0x60, 0x01, 0x01, 0x00} // PUSH1 1 PUSH1 1 ADD STOP

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.SetBalance(sender, uint256.MustFromBig(initialBalance), 0)
	statedb.SetNonce(sender, initialNonce, 0)
	statedb.SetCode(callee, calleeCode, tracing.CodeChangeGenesis)
	statedb.Finalise(true)
	stateRootBefore := statedb.IntermediateRoot(true)

	gp := NewGasPool(30_000_000)
	gpBefore := gp.Gas()

	tx, err := types.SignTx(
		types.NewTransaction(initialNonce, callee, big.NewInt(0), 100_000, big.NewInt(1_000_000_000), nil),
		types.LatestSigner(chainConfig),
		senderKey,
	)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	msg, err := TransactionToMessage(tx, types.LatestSigner(chainConfig), big.NewInt(1_000_000_000))
	if err != nil {
		t.Fatalf("to message: %v", err)
	}

	blockCtx := vm.BlockContext{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.Address{},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Difficulty:  big.NewInt(0),
		BaseFee:     big.NewInt(1_000_000_000),
		GasLimit:    30_000_000,
		Random:      &common.Hash{},
		BlobBaseFee: big.NewInt(1),
	}
	vmConfig := vm.Config{ZkGasMeter: vm.NewZkGasMeter(uzenTestScheduleExhausting())}
	evm := vm.NewEVM(blockCtx, statedb, chainConfig, vmConfig)

	receipt, applyErr := ApplyTransactionWithEVM(
		msg, gp, statedb, big.NewInt(1), common.Hash{}, 1, tx, evm,
	)

	if !errors.Is(applyErr, vm.ErrZkGasLimitExceeded) {
		t.Fatalf("expected ErrZkGasLimitExceeded, got err=%v receipt=%+v", applyErr, receipt)
	}
	if receipt != nil {
		t.Fatalf("expected nil receipt on zk-gas exhaustion, got %+v", receipt)
	}
	if got := statedb.GetNonce(sender); got != initialNonce {
		t.Fatalf("sender nonce changed: got %d want %d", got, initialNonce)
	}
	if got := statedb.GetBalance(sender).ToBig(); got.Cmp(initialBalance) != 0 {
		t.Fatalf("sender balance changed: got %s want %s", got, initialBalance)
	}
	if got := gp.Gas(); got != gpBefore {
		t.Fatalf("gas pool changed: got %d want %d", got, gpBefore)
	}
	statedb.Finalise(true)
	if got := statedb.IntermediateRoot(true); got != stateRootBefore {
		t.Fatalf("state root changed: got %s want %s", got.Hex(), stateRootBefore.Hex())
	}
}

// CHANGE(taiko): deployExhaustingContract installs a tight loop at `addr` that
// burns opcode gas (and therefore zk gas) quickly. The body is:
//
//	JUMPDEST; PUSH1 0; PUSH1 0; ADD; POP; PUSH1 0; JUMP
//
// It loops until out of gas, which under a tight zk gas budget means the
// block-wide zk gas limit trips first.
func deployExhaustingContract(t *testing.T, statedb *state.StateDB, addr common.Address) {
	t.Helper()
	statedb.SetCode(addr, []byte{0x5b, 0x60, 0x00, 0x60, 0x00, 0x01, 0x50, 0x60, 0x00, 0x56}, tracing.CodeChangeGenesis)
}

// CHANGE(taiko): TestApplyTransactionWithEVM_UzenCommitThenTruncate drives two
// transactions against a shared ZkGasMeter. Tx A (a small value transfer)
// succeeds and is committed. Tx B (a call to a tight-loop contract) exhausts
// the remaining block budget and must be reverted with no state imprint.
// Mirrors alethia-reth's block-executor truncation contract
// (crates/block/src/executor.rs:261-291).
func TestApplyTransactionWithEVM_UzenCommitThenTruncate(t *testing.T) {
	var (
		chainConfig    = uzenTestChainConfig(t)
		key1, _        = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1          = crypto.PubkeyToAddress(key1.PublicKey)
		// Mirror of core/state_processor_test.go:51 — the literal "0020" in the
		// middle is load-bearing (matches an existing fixture key used across
		// the repo), not a typo.
		key2, _ = crypto.HexToECDSA("0202020202020202020202020202020202020202020202020202002020202020")
		addr2          = crypto.PubkeyToAddress(key2.PublicKey)
		heavyContract  = common.HexToAddress("0x000000000000000000000000000000000000beef")
		initialBalance = new(big.Int).SetUint64(1_000_000_000_000_000_000)
	)

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.SetBalance(addr1, uint256.MustFromBig(initialBalance), 0)
	statedb.SetBalance(addr2, uint256.MustFromBig(initialBalance), 0)
	deployExhaustingContract(t, statedb, heavyContract)
	statedb.Finalise(true)

	signer := types.LatestSigner(chainConfig)
	txA, err := types.SignTx(types.NewTransaction(0, addr2, big.NewInt(1), params.TxGas, big.NewInt(1_000_000_000), nil), signer, key1)
	if err != nil {
		t.Fatal(err)
	}
	txB, err := types.SignTx(types.NewTransaction(0, heavyContract, big.NewInt(0), 5_000_000, big.NewInt(1_000_000_000), nil), signer, key2)
	if err != nil {
		t.Fatal(err)
	}

	// Tight budget that admits tx A but not tx B.
	schedule := &vm.ZkGasSchedule{BlockLimit: 50_000}
	for i := range schedule.OpcodeMultipliers {
		schedule.OpcodeMultipliers[i] = 10
	}
	for i := range schedule.PrecompileMultipliers {
		schedule.PrecompileMultipliers[i] = math.MaxUint16
	}

	gp := NewGasPool(30_000_000)
	meter := vm.NewZkGasMeter(schedule)
	vmConfig := vm.Config{ZkGasMeter: meter}
	blockCtx := vm.BlockContext{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.Address{},
		BlockNumber: big.NewInt(1),
		Time:        1,
		Difficulty:  big.NewInt(0),
		BaseFee:     big.NewInt(1_000_000_000),
		GasLimit:    30_000_000,
		Random:      &common.Hash{},
		BlobBaseFee: big.NewInt(1),
	}
	evm := vm.NewEVM(blockCtx, statedb, chainConfig, vmConfig)

	// Apply txA — must succeed.
	msgA, _ := TransactionToMessage(txA, signer, blockCtx.BaseFee)
	meter.ResetTransaction()
	receiptA, err := ApplyTransactionWithEVM(msgA, gp, statedb, blockCtx.BlockNumber, common.Hash{}, blockCtx.Time, txA, evm)
	if err != nil {
		t.Fatalf("txA unexpectedly failed: %v", err)
	}
	if receiptA == nil || receiptA.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("txA unexpected receipt: %+v", receiptA)
	}
	if err := meter.CommitTransaction(); err != nil {
		t.Fatalf("commit txA meter: %v", err)
	}
	// Capture addr2's state after txA commits — any change after txB runs must
	// be a txB-origin mutation that the snapshot/revert path failed to undo.
	preNonce2 := statedb.GetNonce(addr2)
	preBalance2 := new(big.Int).Set(statedb.GetBalance(addr2).ToBig())
	postTxARoot := statedb.IntermediateRoot(true)
	postTxAGp := gp.Gas()

	// Apply txB — must return ErrZkGasLimitExceeded and leave addr2 untouched.
	msgB, _ := TransactionToMessage(txB, signer, blockCtx.BaseFee)
	meter.ResetTransaction()
	receiptB, errB := ApplyTransactionWithEVM(msgB, gp, statedb, blockCtx.BlockNumber, common.Hash{}, blockCtx.Time, txB, evm)
	if !errors.Is(errB, vm.ErrZkGasLimitExceeded) {
		t.Fatalf("txB expected ErrZkGasLimitExceeded, got err=%v receipt=%+v", errB, receiptB)
	}
	if receiptB != nil {
		t.Fatalf("txB expected nil receipt, got %+v", receiptB)
	}
	if got := statedb.GetNonce(addr2); got != preNonce2 {
		t.Fatalf("addr2 nonce mutated after exhausted tx: got %d want %d", got, preNonce2)
	}
	if got := statedb.GetBalance(addr2).ToBig(); got.Cmp(preBalance2) != 0 {
		t.Fatalf("addr2 balance mutated after exhausted tx: got %s want %s", got, preBalance2)
	}
	statedb.Finalise(true)
	if got := statedb.IntermediateRoot(true); got != postTxARoot {
		t.Fatalf("state root changed after exhausted tx: got %s want %s", got.Hex(), postTxARoot.Hex())
	}
	if got := gp.Gas(); got != postTxAGp {
		t.Fatalf("gas pool changed after exhausted tx: got %d want %d", got, postTxAGp)
	}
}
