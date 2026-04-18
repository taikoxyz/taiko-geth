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
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/internal/telemetry"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
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
func (p *StateProcessor) Process(ctx context.Context, block *types.Block, statedb *state.StateDB, cfg vm.Config) (*ProcessResult, error) {
	var (
		config      = p.chainConfig()
		receipts    types.Receipts
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = NewGasPool(block.GasLimit())
	)
	var tracingStateDB = vm.StateDB(statedb)
	if hooks := cfg.Tracer; hooks != nil {
		tracingStateDB = state.NewHookedState(statedb, hooks)
	}

	// Mutate the block and state according to any hard-fork specs
	if config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(tracingStateDB)
	}
	var (
		context vm.BlockContext
		signer  = types.MakeSigner(config, header.Number, header.Time)
	)

	// CHANGE(taiko): initialize zk gas meter for Uzen blocks.
	// Must be set before NewEVM since it copies cfg by value.
	if config.IsUzen(header.Time) {
		cfg.ZkGasMeter = vm.NewZkGasMeter(&vm.UzenZkGasSchedule)
		// CHANGE(taiko): Uzen imported blocks must not contain blob transactions.
		for i, tx := range block.Transactions() {
			if tx.Type() == types.BlobTxType {
				return nil, fmt.Errorf("blob transaction at index %d not allowed in Uzen block", i)
			}
		}
	}

	// Apply pre-execution system calls.
	context = NewEVMBlockContext(header, p.chain, nil)
	evm := vm.NewEVM(context, tracingStateDB, config, cfg)

	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		ProcessBeaconBlockRoot(*beaconRoot, evm)
	}
	if config.IsPrague(block.Number(), block.Time()) || config.IsVerkle(block.Number(), block.Time()) {
		ProcessParentBlockHash(block.ParentHash(), evm)
	}

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
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
		if config.IsShasta(header.Time) {
			msg.BasefeeSharingPctg = DecodeShastaBasefeeSharingPctg(header.Extra)
		} else if config.IsOntake(block.Number()) {
			msg.BasefeeSharingPctg = DecodeOntakeExtraData(header.Extra)
		}
		statedb.SetTxContext(tx.Hash(), i)
		_, _, spanEnd := telemetry.StartSpan(ctx, "core.ApplyTransactionWithEVM",
			telemetry.StringAttribute("tx.hash", tx.Hash().Hex()),
			telemetry.Int64Attribute("tx.index", int64(i)),
		)

		// CHANGE(taiko): reset in-flight zk gas before each transaction.
		if cfg.ZkGasMeter != nil {
			cfg.ZkGasMeter.ResetTransaction()
		}

		receipt, err := ApplyTransactionWithEVM(msg, gp, statedb, blockNumber, blockHash, context.Time, tx, evm)
		if err != nil {
			// CHANGE(taiko): if zk gas exceeded on a non-anchor tx, abort and skip remaining.
			// The anchor tx (i==0) is never discarded — it must always be in the block.
			if cfg.ZkGasMeter != nil && errors.Is(err, vm.ErrZkGasLimitExceeded) && i > 0 {
				log.Debug(
					"Uzen zk gas limit reached during block processing; truncating",
					"txIndex", i,
					"txHash", tx.Hash(),
					"blockZkGasUsed", cfg.ZkGasMeter.BlockZkGasUsed(),
				)
				cfg.ZkGasMeter.ResetTransaction()
				spanEnd(nil)
				break
			}
			spanEnd(&err)
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}

		// CHANGE(taiko): commit transaction zk gas on success.
		if cfg.ZkGasMeter != nil {
			if commitErr := cfg.ZkGasMeter.CommitTransaction(); commitErr != nil && i > 0 {
				cfg.ZkGasMeter.ResetTransaction()
				spanEnd(nil)
				break
			}
		}

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
		spanEnd(nil)
	}
	requests, err := postExecution(ctx, config, block, allLogs, evm)
	if err != nil {
		return nil, err
	}

	// CHANGE(taiko): validate Uzen block post-execution invariants.
	if cfg.ZkGasMeter != nil {
		// Validate that body doesn't extend past zk gas truncation point.
		// Mirrors alethia-reth's body_transaction_count == committed_receipt_count check.
		if len(block.Transactions()) != len(receipts) {
			return nil, fmt.Errorf(
				"Uzen block body extends past zk gas truncation point: body has %d transactions but execution committed %d",
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

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.chain.Engine().Finalize(p.chain, header, tracingStateDB, block.Body())

	return &ProcessResult{
		Receipts: receipts,
		Requests: requests,
		Logs:     allLogs,
		GasUsed:  gp.Used(),
	}, nil
}

// postExecution processes the post-execution system calls if Prague is enabled.
func postExecution(ctx context.Context, config *params.ChainConfig, block *types.Block, allLogs []*types.Log, evm *vm.EVM) (requests [][]byte, err error) {
	_, _, spanEnd := telemetry.StartSpan(ctx, "core.postExecution")
	defer spanEnd(&err)

	// Read requests if Prague is enabled.
	if config.IsPrague(block.Number(), block.Time()) {
		requests = [][]byte{}
		// EIP-6110
		if err := ParseDepositLogs(&requests, allLogs, config); err != nil {
			return requests, fmt.Errorf("failed to parse deposit logs: %w", err)
		}
		// EIP-7002
		if err := ProcessWithdrawalQueue(&requests, evm); err != nil {
			return requests, fmt.Errorf("failed to process withdrawal queue: %w", err)
		}
		// EIP-7251
		if err := ProcessConsolidationQueue(&requests, evm); err != nil {
			return requests, fmt.Errorf("failed to process consolidation queue: %w", err)
		}
	}

	return requests, nil
}

// ApplyTransactionWithEVM attempts to apply a transaction to the given state database
// and uses the input parameters for its environment similar to ApplyTransaction. However,
// this method takes an already created EVM instance as input.
func ApplyTransactionWithEVM(msg *Message, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, evm *vm.EVM) (receipt *types.Receipt, err error) {
	if hooks := evm.Config.Tracer; hooks != nil {
		if hooks.OnTxStart != nil {
			hooks.OnTxStart(evm.GetVMContext(), tx, msg.From)
		}
		if hooks.OnTxEnd != nil {
			defer func() { hooks.OnTxEnd(receipt, err) }()
		}
	}

	// CHANGE(taiko): Uzen-only — snapshot statedb and gas pool so a
	// zk-gas-exhausted transaction can be reverted cleanly to match
	// alethia-reth. alethia-reth's revm never commits on error; taiko-geth
	// mutates statedb in place, so we capture pre-tx state here and revert
	// below when the interpreter surfaces a zk-gas-limit error via result.Err.
	var (
		zkSnap int = -1
		zkGp   *GasPool
	)
	if evm.Config.ZkGasMeter != nil {
		zkSnap = statedb.Snapshot()
		zkGp = gp.Snapshot()
	}

	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, err
	}

	// CHANGE(taiko): a zk-gas-limit error from the interpreter is stored in
	// result.Err (not bubbled as err, per core/state_transition.go comment).
	// Revert all tx-level state mutations and propagate as a Go error so the
	// block-building/processing loops can break without appending a receipt.
	if zkSnap >= 0 && result.Err != nil && errors.Is(result.Err, vm.ErrZkGasLimitExceeded) {
		statedb.RevertToSnapshot(zkSnap)
		gp.Set(zkGp)
		return nil, vm.ErrZkGasLimitExceeded
	}

	// Update the state with pending changes.
	var root []byte
	if evm.ChainConfig().IsByzantium(blockNumber) {
		evm.StateDB.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(evm.ChainConfig().IsEIP158(blockNumber)).Bytes()
	}
	// Merge the tx-local access event into the "block-local" one, in order to collect
	// all values, so that the witness can be built.
	if statedb.Database().TrieDB().IsVerkle() {
		statedb.AccessEvents().Merge(evm.AccessEvents)
	}
	return MakeReceipt(evm, result, statedb, blockNumber, blockHash, blockTime, tx, gp.CumulativeUsed(), root), nil
}

// MakeReceipt generates the receipt object for a transaction given its execution result.
func MakeReceipt(evm *vm.EVM, result *ExecutionResult, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, cumulativeGas uint64, root []byte) *types.Receipt {
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

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockNumber.Uint64(), blockHash, blockTime)
	receipt.Bloom = types.CreateBloom(receipt)
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	return receipt
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(evm *vm.EVM, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction) (*types.Receipt, error) {
	msg, err := TransactionToMessage(tx, types.MakeSigner(evm.ChainConfig(), header.Number, header.Time), header.BaseFee)
	if err != nil {
		return nil, err
	}
	// CHANGE(taiko): decode the basefeeSharingPctg config from the extradata.
	if evm.ChainConfig().IsShasta(header.Time) {
		msg.BasefeeSharingPctg = DecodeShastaBasefeeSharingPctg(header.Extra)
	} else if evm.ChainConfig().IsOntake(header.Number) {
		msg.BasefeeSharingPctg = DecodeOntakeExtraData(header.Extra)
	}
	// Create a new context to be used in the EVM environment
	return ApplyTransactionWithEVM(msg, gp, statedb, header.Number, header.Hash(), header.Time, tx, evm)
}

// ProcessBeaconBlockRoot applies the EIP-4788 system call to the beacon block root
// contract. This method is exported to be used in tests.
func ProcessBeaconBlockRoot(beaconRoot common.Hash, evm *vm.EVM) {
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &params.BeaconRootsAddress,
		Data:      beaconRoot[:],
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.AddAddressToAccessList(params.BeaconRootsAddress)
	_, _, _ = evm.Call(msg.From, *msg.To, msg.Data, 30_000_000, common.U2560)
	evm.StateDB.Finalise(true)
}

// ProcessParentBlockHash stores the parent block hash in the history storage contract
// as per EIP-2935/7709.
func ProcessParentBlockHash(prevHash common.Hash, evm *vm.EVM) {
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &params.HistoryStorageAddress,
		Data:      prevHash.Bytes(),
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.AddAddressToAccessList(params.HistoryStorageAddress)
	_, _, err := evm.Call(msg.From, *msg.To, msg.Data, 30_000_000, common.U2560)
	if err != nil {
		panic(err)
	}
	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	evm.StateDB.Finalise(true)
}

// ProcessWithdrawalQueue calls the EIP-7002 withdrawal queue contract.
// It returns the opaque request data returned by the contract.
func ProcessWithdrawalQueue(requests *[][]byte, evm *vm.EVM) error {
	return processRequestsSystemCall(requests, evm, 0x01, params.WithdrawalQueueAddress)
}

// ProcessConsolidationQueue calls the EIP-7251 consolidation queue contract.
// It returns the opaque request data returned by the contract.
func ProcessConsolidationQueue(requests *[][]byte, evm *vm.EVM) error {
	return processRequestsSystemCall(requests, evm, 0x02, params.ConsolidationQueueAddress)
}

func processRequestsSystemCall(requests *[][]byte, evm *vm.EVM, requestType byte, addr common.Address) error {
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &addr,
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.AddAddressToAccessList(addr)
	ret, _, err := evm.Call(msg.From, *msg.To, msg.Data, 30_000_000, common.U2560)
	evm.StateDB.Finalise(true)
	if err != nil {
		return fmt.Errorf("system call failed to execute: %v", err)
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
