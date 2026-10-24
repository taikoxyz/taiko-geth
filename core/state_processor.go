// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/internal/telemetry"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	chain ChainContext // Chain context interface
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(chain ChainContext) *StateProcessor {
	return &StateProcessor{
		chain: chain,
	}
}

// chainConfig returns the chain configuration.
func (p *StateProcessor) chainConfig() *params.ChainConfig {
	return p.chain.Config()
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(ctx context.Context, block *types.Block, statedb *state.StateDB, jumpDestCache vm.JumpDestCache, precompileCache *vm.PrecompileCache, cfg vm.Config, execIndex *atomic.Int64) (*ProcessResult, error) {
	if supportsParallelExecution(block, p.chainConfig(), statedb.Witness() != nil, cfg.Tracer != nil, cfg.DisableParallelExecution) {
		return p.processParallel(ctx, block, statedb, jumpDestCache, precompileCache, cfg)
	}
	var (
		config      = p.chainConfig()
		receipts    = make(types.Receipts, 0, len(block.Transactions()))
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = NewGasPool(block.GasLimit())

		// The receipts are digested on a pipeline of their own, fed as the
		// transactions finish, so the block bloom and the receipt root are
		// ready when the validator asks. With a tracer attached the blooms
		// stay in the loop, it is handed every receipt through OnTxEnd.
		eagerBloom = cfg.Tracer != nil
		pipeline   = newDigestPipeline(len(block.Transactions()), eagerBloom)
	)
	// Run out the pipeline on the paths that abandon the block half way through.
	defer pipeline.abandon()

	var tracingStateDB = vm.StateDB(statedb)
	if hooks := cfg.Tracer; hooks != nil {
		tracingStateDB = state.NewHookedState(statedb, hooks)
	}
	// Mutate the block and state according to any hard-fork specs
	if config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(tracingStateDB)
	}
	parent := p.chain.GetHeader(block.ParentHash(), block.NumberU64()-1)
	if parent == nil {
		return nil, fmt.Errorf("missing parent %#x", block.ParentHash())
	}
	// CHANGE(taiko): initialize zk gas meter for Unzen blocks.
	// Must be set before NewEVM since it copies cfg by value.
	if config.IsUnzen(header.Time) {
		cfg.ZkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
		// CHANGE(taiko): Unzen imported blocks must not contain blob transactions.
		for i, tx := range block.Transactions() {
			if tx.Type() == types.BlobTxType {
				return nil, fmt.Errorf("blob transaction at index %d not allowed in Unzen block", i)
			}
		}
	}
	var (
		context         = NewEVMBlockContext(header, p.chain, nil)
		signer          = types.MakeSigner(config, header.Number, header.Time)
		evm             = vm.NewEVM(context, tracingStateDB, config, cfg)
		blockAccessList = bal.NewConstructionBlockAccessList()
	)
	defer evm.Release()

	if jumpDestCache != nil {
		evm.SetJumpDestCache(jumpDestCache)
	}
	if precompileCache != nil {
		evm.SetPrecompileCache(precompileCache)
	}
	// Run the pre-execution system calls
	blockAccessList.Merge(PreExecution(ctx, block.BeaconRoot(), parent, config, evm, block.Number(), block.Time()))

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		// Publish the progress, letting the prefetcher skip caught up work.
		if execIndex != nil {
			execIndex.Store(int64(i))
		}
		// CHANGE(taiko): mark the first transaction as anchor transaction.
		if i == 0 && config.Taiko {
			if err := tx.MarkAsAnchor(); err != nil {
				return nil, err
			}
		}
		msg, err := TransactionToMessage(tx, signer, header.BaseFee)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		// CHANGE(taiko): decode the basefeeSharingPctg config from the extradata.
		if config.IsShasta(header.Time) {
			msg.BasefeeSharingPctg = DecodeShastaBasefeeSharingPctg(header.Extra)
		} else if config.IsOntake(block.Number()) {
			msg.BasefeeSharingPctg = DecodeOntakeExtraData(header.Extra)
		}
		statedb.SetTxContext(tx.Hash(), i, uint32(i+1))

		// CHANGE(taiko): reset in-flight zk gas before each transaction.
		if cfg.ZkGasMeter != nil {
			cfg.ZkGasMeter.ResetTransaction()
			evm.ResetZkGasErr()
		}

		receipt, bal, err := applyTransactionWithEVM(ctx, msg, gp, statedb, blockNumber, blockHash, context.Time, tx, evm, eagerBloom)
		if err != nil {
			// CHANGE(taiko): if zk gas exceeded on a non-anchor tx, abort and skip remaining.
			// The anchor tx (i==0) is never discarded — it must always be in the block.
			if cfg.ZkGasMeter != nil && errors.Is(err, vm.ErrZkGasLimitExceeded) && i > 0 {
				log.Debug(
					"Unzen zk gas limit reached during block processing; truncating",
					"txIndex", i,
					"txHash", tx.Hash(),
					"blockZkGasUsed", cfg.ZkGasMeter.BlockZkGasUsed(),
				)
				cfg.ZkGasMeter.ResetTransaction()
				evm.ResetZkGasErr()
				break
			}
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}

		// CHANGE(taiko): commit transaction zk gas on success.
		if cfg.ZkGasMeter != nil {
			if commitErr := cfg.ZkGasMeter.CommitTransaction(); commitErr != nil {
				// The anchor can never be truncated: fail the block like the
				// reference executor does instead of committing a body the
				// reference rejects. Unreachable in practice, since charging
				// already bounds committed+in-flight zk gas to the block limit.
				if i == 0 {
					return nil, fmt.Errorf("could not apply anchor tx [%v]: %w", tx.Hash().Hex(), commitErr)
				}
				cfg.ZkGasMeter.ResetTransaction()
				break
			}
		}

		receipts = append(receipts, receipt)
		pipeline.feedReceipt(receipt)
		allLogs = append(allLogs, receipt.Logs...)
		blockAccessList.Merge(bal)
	}
	requests, bal, err := PostExecution(ctx, config, block.Number(), block.Time(), allLogs, block.Withdrawals(), evm, uint32(len(block.Transactions())+1))
	if err != nil {
		return nil, err
	}
	blockAccessList.Merge(bal)

	// CHANGE(taiko): validate Unzen block post-execution invariants. These
	// must return before pipeline.joinReceipts below: a truncated body never
	// feeds the pipeline all of its receipts.
	if cfg.ZkGasMeter != nil {
		// Validate that body doesn't extend past zk gas truncation point.
		// Mirrors the Rust reference block-executor body_transaction_count == committed_receipt_count check.
		if len(block.Transactions()) != len(receipts) {
			return nil, fmt.Errorf(
				"Unzen block body extends past zk gas truncation point: body has %d transactions but execution committed %d",
				len(block.Transactions()), len(receipts),
			)
		}
		// Validate that the imported header difficulty matches the recomputed
		// finalized block zk gas.
		recomputed := new(big.Int).SetUint64(cfg.ZkGasMeter.BlockZkGasUsed())
		if header.Difficulty.Cmp(recomputed) != 0 {
			return nil, fmt.Errorf("zk gas difficulty mismatch: header has %v, recomputed %v", header.Difficulty, recomputed)
		}
	}

	// Finalize the block, applying any consensus engine specific extras
	// (e.g. block rewards).
	p.chain.Engine().Finalize(p.chain, header, tracingStateDB, block.Body())

	// The access list is final, let the pipeline encode it while the block is
	// validated.
	pipeline.feedBAL(blockAccessList)

	// Join the receipts. They are only complete, and safe to hand back, once
	// the pipeline has filled in their blooms.
	pipeline.joinReceipts()

	return &ProcessResult{
		Receipts: receipts,
		Requests: requests,
		Logs:     allLogs,
		GasUsed:  gp.Used(),
		pipeline: pipeline,
	}, nil
}

// PreExecution processes pre-execution state changes and system calls.
func PreExecution(ctx context.Context, beaconRoot *common.Hash, parent *types.Header, config *params.ChainConfig, evm *vm.EVM, number *big.Int, time uint64) *bal.ConstructionBlockAccessList {
	_, _, spanEnd := telemetry.StartSpan(ctx, "core.preExecution")
	defer spanEnd(nil)

	var blockAccessList *bal.ConstructionBlockAccessList
	if config.IsAmsterdam(number, time) {
		blockAccessList = bal.NewConstructionBlockAccessList()
	}
	// EIP-4788
	if beaconRoot != nil {
		ProcessBeaconBlockRoot(*beaconRoot, evm, blockAccessList)
	}
	// EIP-2935
	if config.IsPrague(number, time) || config.IsUBT(number, time) {
		ProcessParentBlockHash(parent.Hash(), evm, blockAccessList)
	}
	return blockAccessList
}

// PostExecution processes the withdrawals and then the post-execution system
// calls when Prague is enabled. If Prague is not activated, it returns null
// requests to differentiate from empty requests.
func PostExecution(ctx context.Context, config *params.ChainConfig, number *big.Int, time uint64, allLogs []*types.Log, withdrawals types.Withdrawals, evm *vm.EVM, blockAccessIndex uint32) (requests [][]byte, blockAccessList *bal.ConstructionBlockAccessList, err error) {
	_, _, spanEnd := telemetry.StartSpan(ctx, "core.postExecution")
	defer spanEnd(&err)

	if config.IsAmsterdam(number, time) {
		blockAccessList = bal.NewConstructionBlockAccessList()
	}
	rules := config.Rules(number, true, time) // IsMerge is always true

	// Withdrawals are applied before the system calls
	// CHANGE(taiko): Taiko credits withdrawals in its consensus engine's Finalize (raw wei).
	if rules.IsShanghai && !config.Taiko {
		ProcessWithdrawals(withdrawals, evm, blockAccessIndex, blockAccessList)
	}
	// Read requests if Prague is enabled.
	if rules.IsPrague {
		requests = [][]byte{}
		// EIP-6110
		if err := ParseDepositLogs(&requests, allLogs, config); err != nil {
			return nil, nil, fmt.Errorf("failed to parse deposit logs: %w", err)
		}
		// EIP-7002
		if err := ProcessWithdrawalQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process withdrawal queue: %w", err)
		}
		// EIP-7251
		if err := ProcessConsolidationQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process consolidation queue: %w", err)
		}
	}

	if rules.IsAmsterdam {
		// EIP-8282
		if err := ProcessBuilderDepositQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process builder deposit queue: %w", err)
		}
		if err := ProcessBuilderExitQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process builder exit queue: %w", err)
		}
	}
	return requests, blockAccessList, nil
}

// ProcessWithdrawals credits the given withdrawals to their recipients.
func ProcessWithdrawals(withdrawals types.Withdrawals, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) {
	rules := evm.GetRules()
	evm.StateDB.Prepare(rules, common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, blockAccessIndex)
	for _, w := range withdrawals {
		// Convert amount from gwei to wei.
		amount := new(uint256.Int).SetUint64(w.Amount)
		amount = amount.Mul(amount, uint256.NewInt(params.GWei))
		evm.StateDB.AddBalance(w.Address, amount, tracing.BalanceIncreaseWithdrawal)

		if rules.IsEIP4762 {
			evm.StateDB.AccessEvents().AddAccount(w.Address, true, math.MaxUint64)
		}
	}
	accessList := evm.StateDB.Finalise(rules)
	if rules.IsAmsterdam {
		blockAccessList.Merge(accessList)
	}
}

// ApplyTransactionWithEVM attempts to apply a transaction to the given state database
// and uses the input parameters for its environment similar to ApplyTransaction. However,
// this method takes an already created EVM instance as input.
func ApplyTransactionWithEVM(ctx context.Context, msg *Message, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, evm *vm.EVM) (*types.Receipt, *bal.ConstructionBlockAccessList, error) {
	return applyTransactionWithEVM(ctx, msg, gp, statedb, blockNumber, blockHash, blockTime, tx, evm, true)
}

// applyTransactionWithEVM is ApplyTransactionWithEVM with the receipt bloom
// filter optional. The block processor leaves it out and lets its receipt
// pipeline hash the logs instead.
func applyTransactionWithEVM(ctx context.Context, msg *Message, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, evm *vm.EVM, withBloom bool) (receipt *types.Receipt, bal *bal.ConstructionBlockAccessList, err error) {
	_, _, spanEnd := telemetry.StartSpan(ctx, "core.ApplyTransactionWithEVM",
		telemetry.StringAttribute("tx.hash", tx.Hash().Hex()),
		telemetry.IntAttribute("tx.index", statedb.TxIndex()),
	)
	defer spanEnd(&err)

	if hooks := evm.Config.Tracer; hooks != nil {
		if hooks.OnTxStart != nil {
			hooks.OnTxStart(evm.GetVMContext(), tx, msg.From)
		}
		if hooks.OnTxEnd != nil {
			defer func() { hooks.OnTxEnd(receipt, err) }()
		}
	}

	// CHANGE(taiko): Unzen-only — snapshot statedb and gas pool so a
	// zk-gas-exhausted transaction can be reverted cleanly to match
	// the Rust reference EVM. Its state transition never commits on error; taiko-geth
	// mutates statedb in place, so we capture pre-tx state here and revert
	// below when the interpreter surfaces a zk-gas-limit error via result.Err.
	var (
		zkSnap int = -1
		zkGp   *GasPool
	)
	if evm.Config.ZkGasMeter != nil {
		// CHANGE(taiko): charge the per-tx intrinsic zk gas before EVM execution
		// begins (zk-gas spec: taikoxyz/taiko-mono#21669). The charge accumulates
		// into the in-flight tx total and is committed alongside opcode and
		// precompile usage on success. If the intrinsic alone exceeds the
		// remaining block budget, return ErrZkGasLimitExceeded so the outer loop
		// truncates (non-anchor) or fails (anchor). No state has been mutated yet,
		// so no revert is needed — return before taking the snapshot.
		if err := evm.Config.ZkGasMeter.ChargeTxIntrinsic(); err != nil {
			return nil, nil, err
		}
		zkSnap = statedb.Snapshot()
		zkGp = gp.Snapshot()
	}

	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, nil, err
	}

	// CHANGE(taiko): a zk-gas-limit error from the interpreter is stored in
	// result.Err (not bubbled as err, per core/state_transition.go comment).
	// Revert all tx-level state mutations and propagate as a Go error so the
	// block-building/processing loops can break without appending a receipt.
	if zkSnap >= 0 && result.Err != nil && errors.Is(result.Err, vm.ErrZkGasLimitExceeded) {
		statedb.RevertToSnapshot(zkSnap)
		gp.Set(zkGp)
		return nil, nil, vm.ErrZkGasLimitExceeded
	}

	// Update the state with pending changes.
	var root []byte
	if evm.ChainConfig().IsByzantium(blockNumber) {
		bal = evm.StateDB.Finalise(evm.GetRules())
	} else {
		root = statedb.IntermediateRoot(evm.GetRules()).Bytes()
	}
	// Merge the tx-local access event into the "block-local" one, in order to collect
	// all values, so that the witness can be built.
	if statedb.Database().Type().Is(state.TypeUBT) {
		statedb.AccessEvents().Merge(evm.AccessEvents)
	}
	receipt = makeReceipt(evm, result, statedb, blockNumber, blockHash, blockTime, tx, gp.CumulativeUsed(), root)
	if withBloom {
		receipt.Bloom = types.CreateBloom(receipt)
	}
	return receipt, bal, nil
}

// MakeReceipt generates the receipt object for a transaction given its execution result.
func MakeReceipt(evm *vm.EVM, result *ExecutionResult, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, cumulativeGas uint64, root []byte) *types.Receipt {
	receipt := makeReceipt(evm, result, statedb, blockNumber, blockHash, blockTime, tx, cumulativeGas, root)
	receipt.Bloom = types.CreateBloom(receipt)
	return receipt
}

// makeReceipt generates the receipt object without its bloom filter, which the
// caller either computes itself or leaves to the receipt pipeline.
func makeReceipt(evm *vm.EVM, result *ExecutionResult, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, cumulativeGas uint64, root []byte) *types.Receipt {
	// Create a new receipt for the transaction, storing the intermediate root
	// and gas used by the tx.
	//
	// The cumulative gas used equals the sum of gasUsed across all preceding
	// txs with refunded gas deducted.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: cumulativeGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()

	// GasUsed = max(tx_gas_used - gas_refund, calldata_floor_gas_cost), unchanged
	// in the Amsterdam fork.
	receipt.GasUsed = result.UsedGas

	if tx.Type() == types.BlobTxType {
		receipt.BlobGasUsed = uint64(len(tx.BlobHashes()) * params.BlobTxBlobGasPerBlob)
		receipt.BlobGasPrice = evm.Context.BlobBaseFee
	}

	// If the transaction created a contract, store the creation address in the receipt.
	if tx.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
	}

	// Set the receipt logs.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockNumber.Uint64(), blockHash, blockTime)
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	return receipt
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(ctx context.Context, evm *vm.EVM, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction) (*types.Receipt, *bal.ConstructionBlockAccessList, error) {
	msg, err := TransactionToMessage(tx, types.MakeSigner(evm.ChainConfig(), header.Number, header.Time), header.BaseFee)
	if err != nil {
		return nil, nil, err
	}
	// CHANGE(taiko): decode the basefeeSharingPctg config from the extradata.
	if evm.ChainConfig().IsShasta(header.Time) {
		msg.BasefeeSharingPctg = DecodeShastaBasefeeSharingPctg(header.Extra)
	} else if evm.ChainConfig().IsOntake(header.Number) {
		msg.BasefeeSharingPctg = DecodeOntakeExtraData(header.Extra)
	}
	// Create a new context to be used in the EVM environment
	return ApplyTransactionWithEVM(ctx, msg, gp, statedb, header.Number, header.Hash(), header.Time, tx, evm)
}

// CHANGE(taiko): systemCallGasLimit returns the gas limit for EVM system
// calls. The reference execution client caps every system call at the
// EIP-7825 transaction gas cap, so Taiko chains observe the same cap should
// code ever exist at a system address. Only the cap is aligned: the
// reference runs system calls as full inner transactions (intrinsic gas
// deducted inside the cap, block-env gas limit and basefee swapped for the
// call) — residual differences no canonical system contract observes.
func systemCallGasLimit(config *params.ChainConfig) uint64 {
	if config.Taiko {
		return params.MaxTxGas
	}
	return 30_000_000
}

// systemCallGasBudget returns the gas budget for system calls.
func systemCallGasBudget(evm *vm.EVM) (gasLimit uint64, gasBudget vm.GasBudget) {
	// CHANGE(taiko): observe the reference client's system-call gas limit. The
	// limit maps to the execution-gas dimension of the budget.
	gasLimit = systemCallGasLimit(evm.ChainConfig())
	if !evm.GetRules().IsAmsterdam {
		gasBudget = vm.NewGasBudget(gasLimit, 0)
	} else {
		// SYSTEM_MAX_SSTORES_PER_CALL = 16 is the upper bound on the number of
		// new storage slots a single system call is expected to write.
		stateBudget := params.SystemMaxSStoresPerCall * evm.Context.CostPerStateByte * params.StorageCreationSize
		gasBudget = vm.NewGasBudget(gasLimit, stateBudget)
	}
	return gasLimit, gasBudget
}

// ProcessBeaconBlockRoot applies the EIP-4788 system call to the beacon block root
// contract. This method is exported to be used in tests.
func ProcessBeaconBlockRoot(beaconRoot common.Hash, evm *vm.EVM, blockAccessList *bal.ConstructionBlockAccessList) {
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	gasLimit, gasBudget := systemCallGasBudget(evm)
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  gasLimit,
		GasPrice:  uint256.NewInt(0),
		GasFeeCap: uint256.NewInt(0),
		GasTipCap: uint256.NewInt(0),
		To:        &params.BeaconRootsAddress,
		Data:      beaconRoot[:],
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.Prepare(evm.GetRules(), common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, 0)
	evm.StateDB.AddAddressToAccessList(params.BeaconRootsAddress)

	// Unchecked system call (EIP-4788), failures are ignored as spec
	_, _, _ = evm.Call(msg.From, *msg.To, msg.Data, gasBudget, common.U2560)
	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	accessList := evm.StateDB.Finalise(evm.GetRules())
	if evm.GetRules().IsAmsterdam {
		blockAccessList.Merge(accessList)
	}
}

// ProcessParentBlockHash stores the parent block hash in the history storage contract
// as per EIP-2935/7709.
func ProcessParentBlockHash(prevHash common.Hash, evm *vm.EVM, blockAccessList *bal.ConstructionBlockAccessList) {
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	gasLimit, gasBudget := systemCallGasBudget(evm)
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  gasLimit,
		GasPrice:  uint256.NewInt(0),
		GasFeeCap: uint256.NewInt(0),
		GasTipCap: uint256.NewInt(0),
		To:        &params.HistoryStorageAddress,
		Data:      prevHash.Bytes(),
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.Prepare(evm.GetRules(), common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, 0)
	evm.StateDB.AddAddressToAccessList(params.HistoryStorageAddress)

	// Unchecked system call (EIP-2935), failures are ignored as spec
	_, _, _ = evm.Call(msg.From, *msg.To, msg.Data, gasBudget, common.U2560)
	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	accessList := evm.StateDB.Finalise(evm.GetRules())
	if evm.GetRules().IsAmsterdam {
		blockAccessList.Merge(accessList)
	}
}

// ProcessWithdrawalQueue calls the EIP-7002 withdrawal queue contract.
// It returns the opaque request data returned by the contract.
func ProcessWithdrawalQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x01, params.WithdrawalQueueAddress, blockAccessIndex, blockAccessList)
}

// ProcessConsolidationQueue calls the EIP-7251 consolidation queue contract.
// It returns the opaque request data returned by the contract.
func ProcessConsolidationQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x02, params.ConsolidationQueueAddress, blockAccessIndex, blockAccessList)
}

// ProcessBuilderDepositQueue calls the EIP-8282 builder deposit contract.
// It returns the opaque request data returned by the contract.
func ProcessBuilderDepositQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x03, params.BuilderDepositAddress, blockAccessIndex, blockAccessList)
}

// ProcessBuilderExitQueue calls the EIP-8282 builder exit contract.
// It returns the opaque request data returned by the contract.
func ProcessBuilderExitQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x04, params.BuilderExitAddress, blockAccessIndex, blockAccessList)
}

func processRequestsSystemCall(requests *[][]byte, rules params.Rules, evm *vm.EVM, requestType byte, addr common.Address, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	// CHANGE(taiko): the reference execution client never runs the
	// EIP-7002/7251 request-queue system calls — Taiko blocks pin the empty
	// requests hash — so skip them on every path (import, sealing, and test
	// chain generation) to keep the state transition identical should code
	// ever exist at the queue addresses. This must stay the first statement:
	// the empty-system-contract check below would otherwise fail every
	// Prague-enabled Taiko block.
	if evm.ChainConfig().Taiko {
		return nil
	}
	if evm.StateDB.GetCodeSize(addr) == 0 {
		return fmt.Errorf("empty system contract: no code at %v", addr)
	}
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	if evm.Config.ZkGasMeter != nil {
		evm.Config.ZkGasMeter.ResetTransaction()
		evm.ResetZkGasErr()
		defer evm.Config.ZkGasMeter.ResetTransaction()
		defer evm.ResetZkGasErr()
	}
	gasLimit, gasBudget := systemCallGasBudget(evm)
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  gasLimit,
		GasPrice:  uint256.NewInt(0),
		GasFeeCap: uint256.NewInt(0),
		GasTipCap: uint256.NewInt(0),
		To:        &addr,
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.Prepare(evm.GetRules(), common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, blockAccessIndex)
	evm.StateDB.AddAddressToAccessList(addr)
	ret, _, err := evm.Call(msg.From, *msg.To, msg.Data, gasBudget, common.U2560)
	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	bal := evm.StateDB.Finalise(evm.GetRules())
	if err != nil {
		return fmt.Errorf("system call failed to execute: %v", err)
	}
	if rules.IsAmsterdam {
		blockAccessList.Merge(bal)
	}
	if len(ret) == 0 {
		return nil // skip empty output
	}
	// Append prefixed requestsData to the requests list.
	requestsData := make([]byte, len(ret)+1)
	requestsData[0] = requestType
	copy(requestsData[1:], ret)
	*requests = append(*requests, requestsData)
	return nil
}

var depositTopic = common.HexToHash("0x649bbc62d0e31342afea4e5cd82d4049e7e1ee912fc0889aa790803be39038c5")

// ParseDepositLogs extracts the EIP-6110 deposit values from logs emitted by
// BeaconDepositContract.
func ParseDepositLogs(requests *[][]byte, logs []*types.Log, config *params.ChainConfig) error {
	deposits := make([]byte, 1) // note: first byte is 0x00 (== deposit request type)
	for _, log := range logs {
		if log.Address == config.DepositContractAddress && len(log.Topics) > 0 && log.Topics[0] == depositTopic {
			request, err := types.DepositLogToRequest(log.Data)
			if err != nil {
				return fmt.Errorf("unable to parse deposit data: %v", err)
			}
			deposits = append(deposits, request...)
		}
	}
	if len(deposits) > 1 {
		*requests = append(*requests, deposits)
	}
	return nil
}

func onSystemCallStart(tracer *tracing.Hooks, ctx *tracing.VMContext) {
	if tracer.OnSystemCallStartV2 != nil {
		tracer.OnSystemCallStartV2(ctx)
	} else if tracer.OnSystemCallStart != nil {
		tracer.OnSystemCallStart()
	}
}

// AssembleBlock finalizes the state and assembles the block with provided
// body and receipts.
func AssembleBlock(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, body *types.Body, receipts []*types.Receipt, blockAccessList *bal.ConstructionBlockAccessList) *types.Block {
	// Assign the post-transition state root
	rules := chain.Config().Rules(header.Number, header.Difficulty.Sign() == 0, header.Time)
	header.Root = state.IntermediateRoot(rules)

	if !rules.IsAmsterdam {
		return types.NewBlock(header, body, receipts, trie.NewStackTrie(nil))
	}
	// Assign the BlockAccessListHash if Amsterdam has been enabled
	bal := blockAccessList.ToEncodingObj()
	balHash := bal.Hash()
	header.BlockAccessListHash = &balHash
	return types.NewBlock(header, body, receipts, trie.NewStackTrie(nil)).WithAccessListUnsafe(bal)
}
