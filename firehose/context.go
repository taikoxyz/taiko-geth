package firehose

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/atomic"
)

// NoOpContext can be used when no recording should happen for a given code path
var NoOpContext *Context

var syncContext *Context = NewContext(&DelegateToWriterPrinter{writer: os.Stdout})

// MaybeSyncContext is used when syncing blocks with the network for mindreader consumption, there
// is always a single active sync context use for the whole syncing process, should not be used
// for other purposes.
//
// It responsibility of the user of sync context to ensure it's being used in a concurrent safe
// way and to handle its lifecycle behavior (like resetting it at the end of a block).
func MaybeSyncContext() *Context {
	if !Enabled {
		return NoOpContext
	}

	if !SyncInstrumentationEnabled {
		return NoOpContext
	}

	return syncContext
}

// SyncContext returns the sync context without any checking if firehose is enabled or not. Use
// it only for specific cases and ensure you only use it when it's strictly correct to do so as this
// will print stdout lines.
func SyncContext() *Context {
	return syncContext
}

// Context is a block level data container used throughout firehose instrumentation to
// keep active state about current instrumentation. This contains method to deal with
// block, transaction and call metadata required for proper functionning of Firehose
// code.
type Context struct {
	printer Printer

	blockLogIndex   uint64
	activeCallIndex string
	nextCallIndex   uint64
	callIndexStack  *ExtendedStack

	seenBlock            *atomic.Bool
	inBlock              *atomic.Bool
	inTransaction        *atomic.Bool
	totalOrderingCounter *atomic.Uint64
}

func NewContext(printer Printer) *Context {
	ctx := &Context{
		printer: printer,

		activeCallIndex: "0",
		callIndexStack:  &ExtendedStack{},

		seenBlock:            atomic.NewBool(false),
		inBlock:              atomic.NewBool(false),
		inTransaction:        atomic.NewBool(false),
		totalOrderingCounter: atomic.NewUint64(0),
	}

	ctx.callIndexStack.Push(ctx.activeCallIndex)

	return ctx
}

func (ctx *Context) InitVersion(nodeVersion, dmVersion, variant string) {
	if ctx == nil {
		return
	}
	ctx.printer.Print("INIT", dmVersion, variant, nodeVersion)
}

func NewSpeculativeExecutionContext() *Context {
	return NewContext(NewToBufferPrinter())
}

func (ctx *Context) Enabled() bool {
	return ctx != nil
}

func (ctx *Context) FirehoseLog() []byte {
	if ctx == nil {
		return nil
	}

	if v, ok := ctx.printer.(*ToBufferPrinter); ok {
		return v.buffer.Bytes()
	}

	return nil
}

// Block methods

func (ctx *Context) RecordGenesisBlock(block *types.Block, recordGenesisAlloc func(ctx *Context)) {
	if ctx == nil {
		return
	}

	if ctx.inBlock.Load() == true {
		panic("trying to record genesis block while in block context")
	}

	zero := common.Address{}
	root := block.Root()

	ctx.StartBlock(block)
	ctx.StartTransactionRaw(common.Hash{}, &zero, &big.Int{}, nil, nil, nil, 0, &big.Int{}, 0, nil, nil, nil, nil, 0)
	ctx.RecordTrxFrom(zero)
	recordGenesisAlloc(ctx)
	ctx.EndTransaction(&types.Receipt{PostState: root[:]})
	ctx.FinalizeBlock(block)
	ctx.EndBlock(block, nil, block.Difficulty())
}

func (ctx *Context) StartBlock(block *types.Block) {
	if !ctx.inBlock.CAS(false, true) {
		panic("entering a block while already in a block scope")
	}

	ctx.seenBlock.Store(true)
	ctx.totalOrderingCounter.Store(0)
	ctx.printer.Print("BEGIN_BLOCK", Uint64(block.NumberU64()))
}

func (ctx *Context) FinalizeBlock(block *types.Block) {
	// We must not check if the finalize block is actually in the a block since
	// when firehose block progress only is enabled, it would hit a panic
	ctx.printer.Print("FINALIZE_BLOCK", Uint64(block.NumberU64()))
}

// exitBlock is used when an abnormal condition is encountered while processing
// transactions and we must end the block processing right away, resetting the start
// along the way.
func (ctx *Context) exitBlock() {
	if !ctx.inBlock.CAS(true, false) {
		panic("exiting a block while not already within a block scope")
	}
	ctx.blockLogIndex = 0
}

func (ctx *Context) EndBlock(block *types.Block, finalBlockHeader *types.Header, totalDifficulty *big.Int) {
	ctx.exitBlock()

	endData := map[string]interface{}{
		"header":          block.Header(),
		"uncles":          block.Body().Uncles,
		"totalDifficulty": (*hexutil.Big)(totalDifficulty),
	}

	if finalBlockHeader != nil {
		endData["finalizedBlockNum"] = (*hexutil.Big)(finalBlockHeader.Number)
		endData["finalizedBlockHash"] = finalBlockHeader.Hash()
	}

	ctx.printer.Print("END_BLOCK",
		Uint64(block.NumberU64()),
		Uint64(uint64(block.Size())),
		JSON(endData),
	)
}

// CancelBlock emit a Firehose CANCEL_BLOCK event that tells the console reader to discard any
// accumulated block's data and start over. This happens on certains error conditions where the block
// is actually invalid and will be re-processed by the chain so we should not record it.
func (ctx *Context) CancelBlock(block *types.Block, err error) {
	if ctx == nil {
		return
	}

	// There is some particular runtime code path that could trigger a CANCEL_BLOCK without having started
	// one, it's ok, the reader is resistant to such and here, we simply don't call `ExitBlock`.
	if ctx.inBlock.Load() {
		ctx.exitBlock()
	}

	ctx.printer.Print("CANCEL_BLOCK",
		Uint64(block.NumberU64()),
		err.Error(),
	)
}

// Transaction methods

func (ctx *Context) StartTransaction(tx *types.Transaction, baseFee *big.Int) {
	if ctx == nil {
		return
	}

	hash := tx.Hash()
	v, r, s := tx.RawSignatureValues()

	ctx.StartTransactionRaw(
		hash,
		tx.To(),
		tx.Value(),
		v.Bytes(),
		r.Bytes(),
		s.Bytes(),
		tx.Gas(),
		gasPrice(tx, baseFee),
		tx.Nonce(),
		tx.Data(),
		AccessList(tx.AccessList()),
		maxFeePerGas(tx),
		maxPriorityFeePerGas(tx),
		tx.Type(),
	)
}

func maxFeePerGas(tx *types.Transaction) *big.Int {
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType:
		return nil

	case types.DynamicFeeTxType:
		return tx.GasFeeCap()
	}

	panic(errUnhandledTransactionType("maxFeePerGas", tx.Type()))
}

func maxPriorityFeePerGas(tx *types.Transaction) *big.Int {
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType:
		return nil

	case types.DynamicFeeTxType:
		return tx.GasTipCap()
	}

	panic(errUnhandledTransactionType("maxPriorityFeePerGas", tx.Type()))
}

func gasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType:
		return tx.GasPrice()

	case types.DynamicFeeTxType:
		if baseFee == nil {
			return tx.GasPrice()
		}

		return ethmath.BigMin(new(big.Int).Add(tx.GasTipCap(), baseFee), tx.GasFeeCap())
	}

	panic(errUnhandledTransactionType("gasPrice", tx.Type()))
}

func errUnhandledTransactionType(tag string, value uint8) error {
	return fmt.Errorf("unhandled transaction type's %d for firehose.%s(), carefully review the patch, if this new transaction type add new fields, think about adding them to Firehose Block format, when you see this message, it means something changed in the chain model and great care and thinking most be put here to properly understand the changes and the consequences they bring for the instrumentation", value, tag)
}

func (ctx *Context) StartTransactionRaw(
	hash common.Hash,
	to *common.Address,
	value *big.Int,
	v, r, s []byte,
	gasLimit uint64,
	gasPrice *big.Int,
	nonce uint64,
	data []byte,
	accessList AccessList,
	maxFeePerGas *big.Int,
	maxPriorityFeePerGas *big.Int,
	txType uint8,
) {
	if ctx == nil {
		return
	}

	ctx.openTransaction()

	// We start assuming the "null" value (i.e. a dot character), and update if `to` is set
	toAsString := "."
	if to != nil {
		toAsString = Addr(*to)
	}

	maxFeePerGasAsString := "."
	if maxFeePerGas != nil {
		maxFeePerGasAsString = Hex(maxFeePerGas.Bytes())
	}

	maxPriorityFeePerGasAsString := "."
	if maxPriorityFeePerGas != nil {
		maxPriorityFeePerGasAsString = Hex(maxPriorityFeePerGas.Bytes())
	}

	ctx.printer.Print("BEGIN_APPLY_TRX",
		Hash(hash),
		toAsString,
		Hex(value.Bytes()),
		Hex(v),
		Hex(r),
		Hex(s),
		Uint64(gasLimit),
		Hex(gasPrice.Bytes()),
		Uint64(nonce),
		Hex(data),
		Hex(accessList.marshal()),
		maxFeePerGasAsString,
		maxPriorityFeePerGasAsString,
		Uint8(txType),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

func (ctx *Context) openTransaction() {
	if !ctx.inTransaction.CAS(false, true) {
		panic("entering a transaction while already in a transaction scope")
	}
}

func (ctx *Context) RecordTrxFrom(from common.Address) {
	if ctx == nil {
		return
	}

	if !ctx.inTransaction.Load() {
		debug.PrintStack()
		panic("the RecordTrxFrom should have been call within a transaction, something is deeply wrong")
	}

	ctx.printer.Print("TRX_FROM",
		Addr(from),
	)
}

func (ctx *Context) EndTransaction(receipt *types.Receipt) {
	if ctx == nil {
		return
	}

	if !ctx.inTransaction.CAS(true, false) {
		panic("exiting a transaction while not already within a transaction scope")
	}

	logItems := make([]logItem, len(receipt.Logs))
	for i, log := range receipt.Logs {
		logItems[i] = logItem{
			"address": log.Address,
			"topics":  log.Topics,
			"data":    hexutil.Bytes(log.Data),
		}
	}

	ctx.printer.Print(
		"END_APPLY_TRX",
		Uint64(receipt.GasUsed),
		Hex(receipt.PostState),
		Uint64(receipt.CumulativeGasUsed),
		Hex(receipt.Bloom[:]),
		Uint64(ctx.totalOrderingCounter.Inc()),
		JSON(logItems),
	)

	ctx.nextCallIndex = 0
	ctx.activeCallIndex = "0"
	ctx.callIndexStack = &ExtendedStack{}
	ctx.callIndexStack.Push(ctx.activeCallIndex)
}

// Call methods

func (ctx *Context) StartCall(callType string) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_RUN_CALL",
		callType,
		ctx.openCall(),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)

}

func (ctx *Context) openCall() string {
	ctx.nextCallIndex++
	ctx.activeCallIndex = strconv.FormatUint(ctx.nextCallIndex, 10)

	ctx.callIndexStack.Push(ctx.activeCallIndex)

	return ctx.activeCallIndex
}

func (ctx *Context) callIndex() string {
	if ctx.seenBlock.Load() && !ctx.inBlock.Load() {
		debug.PrintStack()
		panic("should have been call in a block, something is deeply wrong")
	}

	return ctx.activeCallIndex
}

func (ctx *Context) RecordCallParams(callType string, caller common.Address, callee common.Address, value *big.Int, gasLimit uint64, input []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_PARAM",
		callType,
		ctx.callIndex(),
		Addr(caller),
		Addr(callee),
		Hex(value.Bytes()),
		Uint64(gasLimit),
		Hex(input),
	)
}

func (ctx *Context) RecordCallWithoutCode() {
	if ctx == nil {
		return
	}

	ctx.printer.Print("ACCOUNT_WITHOUT_CODE",
		ctx.callIndex(),
	)
}

func (ctx *Context) RecordCallFailed(gasLeft uint64, reason string) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_CALL_FAILED",
		ctx.callIndex(),
		Uint64(gasLeft),
		reason,
	)
}

func (ctx *Context) RecordCallReverted() {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_REVERTED",
		ctx.callIndex(),
	)
}

func (ctx *Context) closeCall() string {
	previousIndex := ctx.callIndexStack.MustPop()
	ctx.activeCallIndex = ctx.callIndexStack.MustPeek()

	return previousIndex
}

func (ctx *Context) EndCall(gasLeft uint64, returnValue []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_END_CALL",
		ctx.closeCall(),
		Uint64(gasLeft),
		Hex(returnValue),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

// EndFailedCall is works similarly to EndCall but actualy also prints extra required line
// like EVM_CALL_FAILED and EVM_REVERTED when it's the case. This is used on early exit in the
// the instrumentation when a failure (and revertion) occurs to reduce the actual method call
// peformed.
func (ctx *Context) EndFailedCall(gasLeft uint64, reverted bool, reason string) {
	if ctx == nil {
		return
	}

	ctx.RecordCallFailed(gasLeft, reason)

	if reverted {
		ctx.RecordCallReverted()
	} else {
		ctx.RecordGasConsume(gasLeft, gasLeft, FailedExecutionGasChangeReason)
		gasLeft = 0
	}

	ctx.printer.Print("EVM_END_CALL",
		ctx.closeCall(),
		Uint64(gasLeft),
		Hex(nil),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

// In-call methods

func (ctx *Context) RecordKeccak(hashOfdata common.Hash, data []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_KECCAK",
		ctx.callIndex(),
		Hash(hashOfdata),
		Hex(data),
	)
}

func (ctx *Context) RecordGasRefund(gasOld, gasRefund uint64) {
	if ctx == nil {
		return
	}

	if gasRefund != 0 {
		ctx.printer.Print("GAS_CHANGE",
			ctx.callIndex(),
			Uint64(gasOld),
			Uint64(gasOld+gasRefund),
			string(RefundAfterExecutionGasChangeReason),
			Uint64(ctx.totalOrderingCounter.Inc()),
		)
	}
}

func (ctx *Context) RecordGasConsume(gasOld, gasConsumed uint64, reason GasChangeReason) {
	if ctx == nil {
		return
	}

	if gasConsumed != 0 && reason != IgnoredGasChangeReason {
		ctx.printer.Print("GAS_CHANGE",
			ctx.callIndex(),
			Uint64(gasOld),
			Uint64(gasOld-gasConsumed),
			string(reason),
			Uint64(ctx.totalOrderingCounter.Inc()),
		)
	}
}

func (ctx *Context) RecordStorageChange(addr common.Address, key, oldData, newData common.Hash) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("STORAGE_CHANGE",
		ctx.callIndex(),
		Addr(addr),
		Hash(key),
		Hash(oldData),
		Hash(newData),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

func (ctx *Context) RecordBalanceChange(addr common.Address, oldBalance, newBalance *big.Int, reason BalanceChangeReason) {
	if ctx == nil {
		return
	}

	if reason != IgnoredBalanceChangeReason {
		// THOUGHTS: There is a choice between storage vs CPU here as we store the old balance and the new balance.
		//           Usually, balances are quite big. Storing instead the old balance and the delta would probably
		//           reduce a lot the storage space at the expense of CPU time to compute the delta and recomputed
		//           the new balance in place where it's required. This would need to be computed (the space
		//           savings) to see if it make sense to apply it or not.
		ctx.printer.Print("BALANCE_CHANGE",
			ctx.callIndex(),
			Addr(addr),
			BigInt(oldBalance),
			BigInt(newBalance),
			string(reason),
			Uint64(ctx.totalOrderingCounter.Inc()),
		)
	}
}

func (ctx *Context) RecordLog(log *types.Log) {
	if ctx == nil {
		return
	}

	strtopics := make([]string, len(log.Topics))
	for idx, topic := range log.Topics {
		strtopics[idx] = Hash(topic)
	}

	ctx.printer.Print("ADD_LOG",
		ctx.callIndex(),
		ctx.logIndexInBlock(),
		Addr(log.Address),
		strings.Join(strtopics, ","),
		Hex(log.Data),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

func (ctx *Context) logIndexInBlock() string {
	current := strconv.FormatUint(ctx.blockLogIndex, 10)
	ctx.blockLogIndex++
	return current
}

func (ctx *Context) RecordSuicide(addr common.Address, suicided bool, balanceBeforeSuicide *big.Int) {
	if ctx == nil {
		return
	}

	// This infers a balance change, a reduction from this account. In the `opSuicide` op code, the corresponding AddBalance is emitted.
	ctx.printer.Print("SUICIDE_CHANGE",
		ctx.callIndex(),
		Addr(addr),
		Bool(suicided),
		BigInt(balanceBeforeSuicide),
	)

	if balanceBeforeSuicide.Sign() != 0 {
		// We need to explicit add a balance change removing the suicided contract balance since
		// the remaining balance of the contract has already been resetted to 0 by the time we
		// do the print call.
		ctx.RecordBalanceChange(addr, balanceBeforeSuicide, common.Big0, BalanceChangeReason("suicide_withdraw"))
	}
}

func (ctx *Context) RecordNewAccount(addr common.Address) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("CREATED_ACCOUNT",
		ctx.callIndex(),
		Addr(addr),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

func (ctx *Context) RecordCodeChange(addr common.Address, oldCodeHash, oldCode []byte, newCodeHash common.Hash, newCode []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("CODE_CHANGE",
		ctx.callIndex(),
		Addr(addr),
		Hex(oldCodeHash),
		Hex(oldCode),
		Hash(newCodeHash),
		Hex(newCode),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

func (ctx *Context) RecordNonceChange(addr common.Address, oldNonce, newNonce uint64) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("NONCE_CHANGE",
		ctx.callIndex(),
		Addr(addr),
		Uint64(oldNonce),
		Uint64(newNonce),
		Uint64(ctx.totalOrderingCounter.Inc()),
	)
}

// Mempool methods

func (ctx *Context) RecordTrxPool(eventType string, tx *types.Transaction, err error) {
	if ctx == nil {
		return
	}

	signer := types.LatestSignerForChainID(tx.ChainId())

	fromAsString := "."
	from, err := types.Sender(signer, tx)
	if err == nil {
		fromAsString = Addr(from)
	}

	toAsString := "."
	if tx.To() != nil {
		toAsString = Addr(*tx.To())
	}

	v, r, s := tx.RawSignatureValues()

	//todo: handle error message
	ctx.printer.Print(
		eventType,
		Hash(tx.Hash()),
		fromAsString,
		toAsString,
		Hex(tx.Value().Bytes()),
		Hex(v.Bytes()),
		Hex(r.Bytes()),
		Hex(s.Bytes()),
		Uint64(tx.Gas()),
		Hex(tx.GasPrice().Bytes()),
		Uint64(tx.Nonce()),
		Hex(tx.Data()),
	)
}

type AccessList types.AccessList

// marshal in a binary format that will be printed as hex in firehose and read on the console reader
// in a binary format.
//
// An access list format will be, varint for the length of the list, followed by each tuple
// being serialized as 20 bytes for the address, varint for the storage keys length followed by
// each storage key as 32 bytes.
func (l AccessList) marshal() (out []byte) {
	if len(l) == 0 {
		// Returns right away when there is not element in the access list
		return []byte{0x00}
	}

	// There is no need for full precision of the 64 bits, so we restrict it to 32 bits max
	if len(l) > math.MaxUint32 {
		panic(fmt.Errorf("access list length is bigger than 32 bits, refusing to do it"))
	}

	// Compute max varuint (length of access list) + N * (contract address + max varuint (length of storage keys) + 32 * K)
	maxByteCount := binary.MaxVarintLen32
	for _, tuple := range l {
		maxByteCount += 20 + binary.MaxVarintLen32 + len(tuple.StorageKeys)*32
	}

	out = make([]byte, maxByteCount)

	offset := 0
	offset += binary.PutUvarint(out[offset:], uint64(len(l)))

	for _, tuple := range l {
		// There is no need for full precision of the 64 bits, so we restrict it to 32 bits max
		if len(tuple.StorageKeys) > math.MaxUint32 {
			panic(fmt.Errorf("access list length is bigger than 32 bits, refusing to do it"))
		}

		offset += copy(out[offset:], tuple.Address[:])
		offset += binary.PutUvarint(out[offset:], uint64(len(tuple.StorageKeys)))
		for _, key := range tuple.StorageKeys {
			offset += copy(out[offset:], key[:])
		}
	}

	return out[0:offset]
}
