package miner

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/holiman/uint256"
)

const (
	TxListCompressionCheckInterval = 100
	TxListCompressionPruneStep     = 10

	// txListZlibGuardBytes is how close to the tx-list size limit a
	// transaction's encoded size must be before its compressed size is checked
	// exactly. zlib adds only a few bytes of framing even to incompressible
	// data, so a transaction further from the limit always fits on its own.
	txListZlibGuardBytes = 4 * 1024

	// txListItemOverhead bounds the RLP header bytes a transaction adds to a
	// tx list beyond its own encoded size.
	txListItemOverhead = 8

	// txListAnchorZkGasReserve is the Unzen zk gas held back for the anchor
	// transaction, which tx-list building does not execute but every derived
	// block starts with. Mainnet anchor-only blocks use about 1.16M zk gas.
	txListAnchorZkGasReserve = 2_000_000
)

// txListZkOverflowSet remembers, across tx-list building calls, the
// transactions that overflowed the zk-gas budget and the execution state each
// one last overflowed at. An entry is kept for as long as its transaction
// stays in the pool, which also bounds the set by the pool's size. A cache
// smaller than the pool would not do: a sender holding more such transactions
// than it fits could cycle evicted ones back in to be run as if new.
type txListZkOverflowSet struct {
	mu     sync.Mutex
	states map[common.Hash]common.Hash // tx hash to txListZkBudget.state at its last overflow
}

func newTxListZkOverflowSet() *txListZkOverflowSet {
	return &txListZkOverflowSet{states: make(map[common.Hash]common.Hash)}
}

// record notes that the transaction overflowed at the given execution state.
func (s *txListZkOverflowSet) record(hash, state common.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[hash] = state
}

// state returns the execution state the transaction last overflowed at.
func (s *txListZkOverflowSet) state(hash common.Hash) (common.Hash, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[hash]
	return state, ok
}

// forget drops the transaction, which no longer overflows.
func (s *txListZkOverflowSet) forget(hash common.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, hash)
}

// prune forgets the transactions that have left the pool.
func (s *txListZkOverflowSet) prune(inPool func(common.Hash) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash := range s.states {
		if !inPool(hash) {
			delete(s.states, hash)
		}
	}
}

// txListZkBudget meters the Unzen zk-gas budget across the lists of one
// tx-list building call. A transaction that overflows the budget is reverted
// and pays nothing, so the work spent running such transactions is capped.
//
// Each overflow is remembered, in this call and across calls, with a digest of
// the execution state it ran at: the parent block, the block context the
// simulation sets, and the transactions committed ahead of it in this call.
// How much zk gas a transaction needs depends on that state. Then:
//   - at the same state the transaction would overflow again, so it is
//     skipped without being run;
//   - at another state it may now fit, so it is run again, but only while the
//     gas limits of re-runs that overflowed again in this call are below
//     limit. Past that, remembered transactions are skipped for the rest of
//     the call. Re-runs never stop selection;
//   - once the gas limits of first-time overflows in this call reach limit,
//     no further transaction is selected in this call.
//
// So a call stops only on transactions not seen to overflow before, and the
// unpaid work of a call is bounded by twice limit plus two transactions.
// The simulation's timestamp is left out of the digest: it changes with every
// call and differs from the derived block's anyway.
//
// Remembered transactions do not expire while they are in the pool. An
// expiry would let a sender stagger its overflowing transactions so that some
// come due as new every block and stop selection again.
type txListZkBudget struct {
	meter     *vm.ZkGasMeter
	overflows *txListZkOverflowSet // shared by all calls, or this call's own
	state     common.Hash          // digest of the execution state the next transaction runs at
	rerun     bool                 // the current transaction overflowed before, at another state
	wastedGas uint64               // summed gas limits of first-time overflows
	rerunGas  uint64               // summed gas limits of re-runs that overflowed again
	limit     uint64
	stopped   bool // wastedGas reached limit
}

// newTxListZkBudget attaches a zk-gas meter with the anchor reserve to env.
// The lists of the call are built with blockMaxGasLimit as the block gas limit,
// which also serves as limit.
func newTxListZkBudget(env *environment, overflows *txListZkOverflowSet, blockMaxGasLimit uint64) (*txListZkBudget, error) {
	meter := vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
	if err := meter.ReserveBlockZkGas(txListAnchorZkGasReserve); err != nil {
		return nil, err
	}
	env.evm.SetZkGasMeter(meter)
	if overflows == nil {
		overflows = newTxListZkOverflowSet()
	}
	var baseFee common.Hash
	if env.header.BaseFee != nil {
		baseFee = common.BigToHash(env.header.BaseFee)
	}
	return &txListZkBudget{
		meter:     meter,
		overflows: overflows,
		state: crypto.Keccak256Hash(
			env.header.ParentHash[:],
			env.coinbase[:],
			baseFee[:],
			binary.BigEndian.AppendUint64(nil, blockMaxGasLimit),
		),
		limit: blockMaxGasLimit,
	}, nil
}

// admit reports whether the transaction should be run: it has not overflowed
// at the current execution state, and if it overflowed at another one, this
// call's allowance for re-running such transactions is not used up.
func (b *txListZkBudget) admit(hash common.Hash) bool {
	state, ok := b.overflows.state(hash)
	b.rerun = ok
	if !ok {
		return true
	}
	return state != b.state && b.rerunGas < b.limit
}

// commit promotes the in-flight zk gas of a successfully executed transaction
// and advances the execution state. Charging already keeps the total within
// the budget, so this only fails on arithmetic overflow.
func (b *txListZkBudget) commit(tx *types.Transaction) error {
	if err := b.meter.CommitTransaction(); err != nil {
		return err
	}
	b.state = crypto.Keccak256Hash(b.state[:], tx.Hash().Bytes())
	if b.rerun {
		b.overflows.forget(tx.Hash())
	}
	return nil
}

// reject records that tx overflowed the budget at the current execution
// state. A first-time overflow counts towards stopping selection for the rest
// of the call, a re-run towards the re-run allowance. The overflowed
// transaction's in-flight zk gas must already be discarded.
func (b *txListZkBudget) reject(tx *types.Transaction) {
	b.overflows.record(tx.Hash(), b.state)
	if b.rerun {
		b.rerunGas += tx.Gas()
		return
	}
	b.wastedGas += tx.Gas()
	if b.wastedGas >= b.limit {
		b.stopped = true
	}
}

// txListFit checks, before a transaction is executed, whether it still fits
// in the tx list being filled. Executing first and trimming the tail
// afterwards would drop, together with a transaction that does not fit, every
// transaction selected after it.
//
// It tracks the list's compressed size: measured exactly from time to time,
// and raised by each added transaction's encoded size in between. Appending
// to a zlib stream can grow it by more than the appended bytes, by a few
// percent when incompressible data shares a deflate block with compressible
// data, so a transaction is let in unmeasured only while the estimate stays
// txListZlibGuardBytes plus an eighth of the bytes added since the last
// measurement below the limit.
type txListFit struct {
	limit    uint64
	size     uint64 // last measured compressed size, plus the encoded bytes added since
	added    uint64 // encoded bytes added since size was last measured
	measured bool   // size is exact
	next     uint64 // measured size of the list with the last fitting candidate, or 0
}

// newTxListFit starts tracking a list that already holds txs.
func newTxListFit(txs types.Transactions, limit uint64) *txListFit {
	f := &txListFit{limit: limit, measured: len(txs) == 0}
	for _, tx := range txs {
		f.size += tx.Size() + txListItemOverhead
	}
	f.added = f.size
	return f
}

// fits reports whether tx fits in the list after txs.
func (f *txListFit) fits(txs types.Transactions, tx *types.Transaction) (bool, error) {
	f.next = 0
	if f.withinEstimate(tx) {
		return true, nil
	}
	if !f.measured {
		size, err := compressedTxListSize(txs)
		if err != nil {
			return false, err
		}
		f.setMeasured(size)
		if f.withinEstimate(tx) {
			return true, nil
		}
	}
	// A transaction that needs more than the room left even compressed on its
	// own is taken not to fit, without compressing the whole list for it.
	room := f.room()
	if tx.Size() > room {
		alone, err := compressedTxListSize(types.Transactions{tx})
		if err != nil {
			return false, err
		}
		if alone > room {
			return false, nil
		}
	}
	size, err := compressedTxListSize(append(txs[:len(txs):len(txs)], tx))
	if err != nil {
		return false, err
	}
	if size > f.limit {
		return false, nil
	}
	f.next = size
	return true, nil
}

// withinEstimate reports whether tx fits without measuring the list.
func (f *txListFit) withinEstimate(tx *types.Transaction) bool {
	n := tx.Size() + txListItemOverhead
	return f.size+n+txListZlibGuardBytes+(f.added+n)/8 <= f.limit
}

// full reports whether the list is too close to the limit to keep looking
// for transactions that fit: less than a 32nd of the limit is left. After
// fits returns false the size it checks against is exact.
func (f *txListFit) full() bool {
	return f.room() < f.limit/32
}

func (f *txListFit) room() uint64 {
	if f.size >= f.limit {
		return 0
	}
	return f.limit - f.size
}

// add records that tx, which fits returned true for, joined the list.
func (f *txListFit) add(tx *types.Transaction) {
	if f.next != 0 {
		f.setMeasured(f.next)
		return
	}
	n := tx.Size() + txListItemOverhead
	f.size += n
	f.added += n
	f.measured = false
}

// setMeasured records the list's exact compressed size.
func (f *txListFit) setMeasured(size uint64) {
	f.size, f.added, f.measured, f.next = size, 0, true, 0
}

// BuildTransactionsLists builds multiple transactions lists which satisfy all the given conditions
// 1. All transactions should all be able to pay the given base fee.
// 2. The total gas used should not exceed the given blockMaxGasLimit
// 3. The total bytes used should not exceed the given maxBytesPerTxList
// 4. The total number of transactions lists should not exceed the given maxTransactionsLists
func (w *Miner) buildTransactionsLists(
	beneficiary common.Address,
	baseFee *big.Int,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	localAccounts []string,
	maxTransactionsLists uint64,
	minTip uint64,
) ([]*PreBuiltTxList, error) {
	var (
		txsLists    []*PreBuiltTxList
		currentHead = w.chain.CurrentBlock()
	)

	log.Info(
		"Start building transactions lists",
		"blockMaxGasLimit", blockMaxGasLimit,
		"maxBytesPerTxList", maxBytesPerTxList,
		"maxTransactionsLists", maxTransactionsLists,
		"localAccounts", localAccounts,
		"minTip", minTip,
	)

	if currentHead == nil {
		log.Error(
			"Failed to find current head",
			"blockMaxGasLimit", blockMaxGasLimit,
			"maxBytesPerTxList", maxBytesPerTxList,
			"maxTransactionsLists", maxTransactionsLists,
			"localAccounts", localAccounts,
			"minTip", minTip,
		)
		return nil, fmt.Errorf("failed to find current head")
	}

	// Check if tx pool is empty at first.
	pending, _ := w.txpool.Pending(
		txpool.PendingFilter{
			MinTip:  uint256.NewInt(minTip),
			BaseFee: uint256.MustFromBig(baseFee),
		},
	)
	pendingTxs := removeGoldenTouchPendingTxs(pending)
	if len(pendingTxs) == 0 {
		log.Warn(
			"Transaction pool for building transactions lists is empty",
			"minTip", minTip,
			"baseFee", baseFee,
			"onlyPlainTxs", true,
		)
		return txsLists, nil
	}

	params := &generateParams{
		timestamp:     uint64(time.Now().Unix()),
		forceTime:     true,
		parentHash:    currentHead.Hash(),
		coinbase:      beneficiary,
		random:        currentHead.MixDigest,
		noTxs:         false,
		baseFeePerGas: baseFee,
	}

	log.Info(
		"Prepare the sealing task for building transactions lists",
		"timestamp", params.timestamp,
		"forceTime", params.forceTime,
		"parentHash", params.parentHash,
		"coinbase", params.coinbase,
		"random", params.random,
		"minTip", minTip,
		"baseFee", baseFee,
		"noTxs", params.noTxs,
	)

	ctx := context.Background()
	env, err := w.prepareWork(ctx, params, false)
	if err != nil {
		return nil, err
	}
	// The simulated state is never committed, so nothing else stops the
	// prefetcher that prepareWork started.
	defer env.discard()

	// Sealing and import cut an Unzen block at the first transaction that
	// exceeds the zk-gas budget, so meter the simulation against the same
	// budget, with the anchor's share reserved. One meter spans every list:
	// each list becomes its own block, so this is conservative, but it also
	// keeps lists that a proposer merges into one block within the budget.
	var zkBudget *txListZkBudget
	if w.chainConfig.IsUnzen(env.header.Time) {
		if w.txListZkOverflows != nil {
			w.txListZkOverflows.prune(w.txpool.Has)
		}
		if zkBudget, err = newTxListZkBudget(env, w.txListZkOverflows, blockMaxGasLimit); err != nil {
			return nil, err
		}
	}

	var (
		signer = types.MakeSigner(w.chainConfig, new(big.Int).Add(currentHead.Number, common.Big1), currentHead.Time)
		// Split the pending transactions into locals and remotes, then
		// fill the block with all available pending transactions.
		localTxs, remoteTxs = w.getPendingTxs(localAccounts, baseFee)
	)

	commitTxs := func(pruningResult *txsPruningResult) (*txsPruningResult, *PreBuiltTxList, error) {
		env.tcount = 0
		env.txs = []*types.Transaction{}
		env.gasPool = core.NewGasPool(blockMaxGasLimit - accumulateGasUsed(pruningResult.ReceiptsPruned))
		setBlockGasLimit(env, blockMaxGasLimit)

		result, err := w.commitL2Transactions(
			env,
			pruningResult.TxsPruned,
			pruningResult.ReceiptsPruned,
			newTransactionsByPriceAndNonce(signer, maps.Clone(localTxs), baseFee),
			newTransactionsByPriceAndNonce(signer, maps.Clone(remoteTxs), baseFee),
			maxBytesPerTxList,
			minTip,
			zkBudget,
		)
		if err != nil {
			log.Error(
				"Failed to commit transactions for building transactions lists",
				"timestamp", params.timestamp,
				"forceTime", params.forceTime,
				"parentHash", params.parentHash,
				"coinbase", params.coinbase,
				"random", params.random,
				"minTip", minTip,
				"baseFee", baseFee,
				"noTxs", params.noTxs,
				"error", err,
			)

			return nil, nil, err
		}

		log.Info(
			"New transactions committed for building transactions lists",
			"localTxs", len(localTxs),
			"remoteTxs", len(remoteTxs),
			"txsPruned", len(pruningResult.TxsPruned),
			"receiptsPruned", len(pruningResult.ReceiptsPruned),
			"txsRemaining", len(result.TxsRemaining),
			"receiptsRemaining", len(result.ReceiptsRemaining),
			"size", result.Size,
			"gasUsed", accumulateGasUsed(result.ReceiptsRemaining),
			"bytesLength", uint64(result.Size),
		)

		return result, &PreBuiltTxList{
			TxList:           result.TxsRemaining,
			EstimatedGasUsed: accumulateGasUsed(result.ReceiptsRemaining),
			BytesLength:      uint64(result.Size),
		}, nil
	}

	var (
		pruningResult  = new(txsPruningResult)
		preBuiltTxList *PreBuiltTxList
	)
	for range int(maxTransactionsLists) {
		if pruningResult, preBuiltTxList, err = commitTxs(pruningResult); err != nil {
			return nil, err
		}

		if len(preBuiltTxList.TxList) == 0 {
			break
		}

		txsLists = append(txsLists, preBuiltTxList)
	}

	log.Info(
		"Finished building transactions lists",
		"timestamp", params.timestamp,
		"forceTime", params.forceTime,
		"parentHash", params.parentHash,
		"coinbase", params.coinbase,
		"random", params.random,
		"minTip", minTip,
		"baseFee", baseFee,
		"noTxs", params.noTxs,
		"txLists", len(txsLists),
	)

	return txsLists, nil
}

// sealBlockWith mines and seals a block with the given block metadata.
func (w *Miner) sealBlockWith(
	parent *types.Header,
	timestamp uint64,
	parentBlockTime uint64,
	blkMeta *engine.BlockMetadata,
	baseFeePerGas *big.Int,
	withdrawals types.Withdrawals,
) (*types.Block, error) {
	// Decode transactions bytes.
	var txs types.Transactions
	if err := rlp.DecodeBytes(blkMeta.TxList, &txs); err != nil {
		return nil, fmt.Errorf("failed to decode txList: %w", err)
	}

	if len(txs) == 0 {
		// A L2 block needs to have have at least one `TaikoL2.anchor` / `TaikoL2.anchorV2` / `TaikoL2.anchorV3`.
		return nil, fmt.Errorf("too less transactions in the block")
	}

	if w.chainConfig.IsShasta(timestamp) {
		baseFeePerGas = misc.CalcEIP4396BaseFee(w.chainConfig, parent, parentBlockTime)
	}

	params := &generateParams{
		timestamp:     timestamp,
		forceTime:     true,
		parentHash:    parent.Hash(),
		coinbase:      blkMeta.Beneficiary,
		random:        blkMeta.MixHash,
		withdrawals:   withdrawals,
		noTxs:         false,
		baseFeePerGas: baseFeePerGas,
	}

	// Set extraData
	w.SetExtra(blkMeta.ExtraData)

	ctx := context.Background()
	env, err := w.prepareWork(ctx, params, false)
	if err != nil {
		return nil, err
	}
	// Computing the state root stops the prefetcher on success, but not on
	// the error returns below.
	defer env.discard()

	setBlockGasLimit(env, blkMeta.GasLimit)

	// Commit transactions.
	gasLimit := env.header.GasLimit
	rules := w.chain.Config().Rules(env.header.Number, true, timestamp)

	env.gasPool = core.NewGasPool(gasLimit)

	// CHANGE(taiko): initialize zk gas meter for Unzen blocks.
	var zkGasMeter *vm.ZkGasMeter
	if w.chainConfig.IsUnzen(timestamp) {
		zkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
		// CHANGE(taiko): attach the meter through the EVM setter so the
		// per-step zk gas tracker is initialized on this late-bound seal path.
		env.evm.SetZkGasMeter(zkGasMeter)
	}

	for i, tx := range txs {
		if i == 0 {
			if err := tx.MarkAsAnchor(); err != nil {
				return nil, err
			}
		}
		// Skip blob transactions
		if tx.Type() == types.BlobTxType {
			log.Debug("Skip a blob transaction", "hash", tx.Hash())
			continue
		}
		sender, err := types.LatestSignerForChainID(w.chainConfig.ChainID).Sender(tx)
		if err != nil {
			log.Debug("Skip an invalid proposed transaction", "hash", tx.Hash(), "reason", err)
			continue
		}

		env.state.Prepare(rules, sender, blkMeta.Beneficiary, tx.To(), vm.ActivePrecompiles(rules), tx.AccessList())
		env.state.SetTxContext(tx.Hash(), env.tcount)

		// CHANGE(taiko): reset in-flight zk gas before each transaction.
		if zkGasMeter != nil {
			zkGasMeter.ResetTransaction()
			env.evm.ResetZkGasErr()
		}

		if err := w.commitTransaction(ctx, env, tx); err != nil {
			// CHANGE(taiko): if zk gas exceeded, stop including transactions.
			// The anchor tx (i==0) is never discarded — it must always be in the block.
			if zkGasMeter != nil && errors.Is(err, vm.ErrZkGasLimitExceeded) && i > 0 {
				log.Debug(
					"Unzen zk gas limit reached during sealing; truncating block",
					"txIndex", i,
					"txHash", tx.Hash(),
					"blockZkGasUsed", zkGasMeter.BlockZkGasUsed(),
				)
				zkGasMeter.ResetTransaction()
				env.evm.ResetZkGasErr()
				break
			}
			if i == 0 {
				return nil, fmt.Errorf("anchor transaction failed: %w", err)
			}
			log.Debug("Skip an invalid proposed transaction", "hash", tx.Hash(), "reason", err)
			continue
		}

		// CHANGE(taiko): commit transaction zk gas on success.
		if zkGasMeter != nil {
			if commitErr := zkGasMeter.CommitTransaction(); commitErr != nil {
				// The anchor can never be truncated: fail sealing instead of
				// building a block the reference implementation rejects.
				// Unreachable in practice, since charging already bounds
				// committed+in-flight zk gas to the block limit.
				if i == 0 {
					return nil, fmt.Errorf("anchor transaction failed: %w", commitErr)
				}
				zkGasMeter.ResetTransaction()
				break
			}
		}

		env.tcount++
	}

	// CHANGE(taiko): set header difficulty to finalized block zk gas for Unzen.
	if zkGasMeter != nil {
		env.header.Difficulty = new(big.Int).SetUint64(zkGasMeter.BlockZkGasUsed())
	}

	block, err := w.engine.FinalizeAndAssemble(
		ctx,
		w.chain,
		env.header,
		env.state,
		&types.Body{Transactions: env.txs, Withdrawals: withdrawals},
		env.receipts,
	)
	if err != nil {
		return nil, err
	}

	results := make(chan *types.Block, 1)
	if err := w.engine.Seal(w.chain, block, results, nil); err != nil {
		return nil, err
	}
	block = <-results

	return block, nil
}

// getPendingTxs fetches the pending transactions from tx pool.
func (w *Miner) getPendingTxs(localAccounts []string, baseFee *big.Int) (
	map[common.Address][]*txpool.LazyTransaction,
	map[common.Address][]*txpool.LazyTransaction,
) {
	rawPending, _ := w.txpool.Pending(txpool.PendingFilter{BaseFee: uint256.MustFromBig(baseFee)})
	pending := removeGoldenTouchPendingTxs(rawPending)
	localTxs, remoteTxs := make(map[common.Address][]*txpool.LazyTransaction), pending

	for _, local := range localAccounts {
		account := common.HexToAddress(local)
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}

	return localTxs, remoteTxs
}

// removeGoldenTouchPendingTxs drops GoldenTouchAccount transactions when building txpool content.
func removeGoldenTouchPendingTxs(
	pending map[common.Address][]*txpool.LazyTransaction,
) map[common.Address][]*txpool.LazyTransaction {
	if len(pending) == 0 {
		return pending
	}

	delete(pending, taiko.GoldenTouchAccount)
	return pending
}

// commitL2Transactions tries to commit the transactions into the given state.
func (w *Miner) commitL2Transactions(
	env *environment,
	presetTxs []*types.Transaction,
	presetReceipts []*types.Receipt,
	txsLocal *transactionsByPriceAndNonce,
	txsRemote *transactionsByPriceAndNonce,
	maxBytesPerTxList uint64,
	minTip uint64,
	zkBudget *txListZkBudget, // nil before Unzen
) (*txsPruningResult, error) {
	var (
		txs           = txsLocal
		isLocal       = true
		pruningResult *txsPruningResult
		err           error
		zkGasMeter    *vm.ZkGasMeter
	)
	if zkBudget != nil {
		zkGasMeter = zkBudget.meter
	}

	if presetTxs != nil {
		env.tcount = len(presetTxs)
		env.txs = append(env.txs, presetTxs...)
		env.receipts = append(env.receipts, presetReceipts...)
	}
	listFit := newTxListFit(env.txs, maxBytesPerTxList)

loop:
	for {
		// If we don't have enough gas for any further transactions then we're done.
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			break
		}
		// The same holds for zk gas: every transaction pays the intrinsic charge.
		if zkGasMeter != nil && zkGasMeter.RemainingBlockZkGas() < zkGasMeter.Schedule().TxIntrinsicZkGas {
			log.Trace("Not enough zk gas for further transactions", "have", zkGasMeter.RemainingBlockZkGas())
			break
		}
		// Transactions over the zk-gas budget have already cost a block's
		// worth of unpaid work in this call.
		if zkBudget != nil && zkBudget.stopped {
			break
		}

		// Retrieve the next transaction and abort if all done.
		ltx, _ := txs.Peek()
		if ltx == nil {
			if isLocal {
				txs = txsRemote
				isLocal = false
				continue
			}
			break
		}
		tx := ltx.Resolve()
		if tx == nil {
			log.Trace("Ignoring evicted transaction")

			txs.Pop()
			continue
		}

		if tx.GasTipCapIntCmp(new(big.Int).SetUint64(minTip)) < 0 {
			log.Trace("Ignoring transaction with low tip", "hash", tx.Hash(), "tip", tx.GasTipCap(), "minTip", minTip)
			txs.Pop()
			continue
		}

		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		from, _ := types.Sender(env.signer, tx)

		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(env.header.Number) {
			log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)

			txs.Pop()
			continue
		}
		if zkBudget != nil && !zkBudget.admit(tx.Hash()) {
			log.Trace("Skipping transaction that overflowed the zk gas budget before", "hash", tx.Hash())
			txs.Pop()
			continue
		}
		// A transaction too large for a tx list of its own can never be
		// proposed. Skip it before executing it: the size pruning below only
		// trims the tail, so once executed it would take every transaction
		// after it out of the list too.
		if fits, err := fitsEmptyTxList(tx, maxBytesPerTxList); err != nil {
			return nil, err
		} else if !fits {
			log.Trace("Skipping transaction too large for a tx list", "hash", tx.Hash(), "size", tx.Size(), "maxBytesPerTxList", maxBytesPerTxList)
			txs.Pop()
			continue
		}
		// For the same reason, check the transaction against the list filled so
		// far before executing it. Skip it if it does not fit and keep filling,
		// unless the list is close enough to the limit to be full.
		if fits, err := listFit.fits(env.txs, tx); err != nil {
			return nil, err
		} else if !fits {
			if listFit.full() {
				log.Trace("Tx list is full", "size", listFit.size, "maxBytesPerTxList", maxBytesPerTxList)
				break
			}
			log.Trace("Skipping transaction that does not fit in the tx list", "hash", tx.Hash(), "size", tx.Size(), "listSize", listFit.size)
			txs.Pop()
			continue
		}
		// Start executing the transaction
		env.state.SetTxContext(tx.Hash(), env.tcount)
		if zkGasMeter != nil {
			zkGasMeter.ResetTransaction()
			env.evm.ResetZkGasErr()
		}

		err := w.commitTransaction(context.Background(), env, tx)
		switch {
		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "hash", ltx.Hash, "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case zkBudget != nil && errors.Is(err, vm.ErrZkGasLimitExceeded):
			// The transaction needs more zk gas than is left, so a derived block
			// would be cut right before it. Its state changes are already
			// reverted; leave it and the sender's later transactions out,
			// remember it so it is not run again at this state, and keep
			// filling the list with transactions that may still fit, until
			// first-time overflows have cost a block's worth of gas.
			log.Trace("Skipping transaction over the zk gas budget", "hash", ltx.Hash, "sender", from, "remaining", zkGasMeter.RemainingBlockZkGas())
			zkGasMeter.ResetTransaction()
			env.evm.ResetZkGasErr()
			txs.Pop()
			if zkBudget.reject(tx); zkBudget.stopped {
				log.Debug("Stopping tx-list selection after zk gas overflows", "wastedGas", zkBudget.wastedGas, "limit", zkBudget.limit)
				break loop
			}

		case errors.Is(err, nil):
			if zkBudget != nil {
				if err := zkBudget.commit(tx); err != nil {
					return nil, err
				}
			}
			// Everything ok, collect the logs and shift in the next transaction from the same account
			txs.Shift()
			listFit.add(tx)

			// Check the size of the compressed txList, if it exceeds the maxBytesPerTxList, break the loop.
			if env.tcount%TxListCompressionCheckInterval == 0 {
				if pruningResult, err = pruneTransactions(env.txs, env.receipts, maxBytesPerTxList); err != nil {
					return nil, err
				}
				// If there are pruned transactions, break the loop.
				if len(pruningResult.TxsPruned) > 0 {
					break loop
				}
				listFit.setMeasured(uint64(pruningResult.Size))
			}

		default:
			// Transaction is regarded as invalid, drop all consecutive transactions from
			// the same sender because of `nonce-too-high` clause.
			log.Trace("Transaction failed, account skipped", "hash", ltx.Hash, "err", err)
			txs.Pop()
		}
	}

	if pruningResult, err = pruneTransactions(env.txs, env.receipts, maxBytesPerTxList); err != nil {
		return nil, err
	}

	return pruningResult, nil
}

// fitsEmptyTxList reports whether tx fits, compressed, in a tx list of its own.
func fitsEmptyTxList(tx *types.Transaction, maxBytesPerTxList uint64) (bool, error) {
	if tx.Size()+txListZlibGuardBytes < maxBytesPerTxList {
		return true, nil
	}
	size, err := compressedTxListSize(types.Transactions{tx})
	if err != nil {
		return false, err
	}
	return size <= maxBytesPerTxList, nil
}

// compressedTxListSize returns the size of txs encoded and compressed as a
// tx list.
func compressedTxListSize(txs types.Transactions) (uint64, error) {
	b, err := encodeAndCompressTxList(txs)
	if err != nil {
		return 0, err
	}
	return uint64(len(b)), nil
}

// encodeAndCompressTxList encodes and compresses the given transactions list.
func encodeAndCompressTxList(txs types.Transactions) ([]byte, error) {
	b, err := rlp.EncodeToBytes(txs)
	if err != nil {
		return nil, err
	}

	return compress(b)
}

// compress compresses the given txList bytes using zlib.
func compress(txListBytes []byte) ([]byte, error) {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	defer w.Close()

	if _, err := w.Write(txListBytes); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// txsPruningResult represents the result of a transactions list pruning.
type txsPruningResult struct {
	TxsPruned         []*types.Transaction
	ReceiptsPruned    []*types.Receipt
	TxsRemaining      []*types.Transaction
	ReceiptsRemaining []*types.Receipt
	Size              int
}

// pruneTransactions prunes the transactions from the given environment to fit the size limit.
func pruneTransactions(
	txs []*types.Transaction,
	receipts []*types.Receipt,
	sizeLimit uint64,
) (*txsPruningResult, error) {
	var (
		prunedTxs      []*types.Transaction
		prunedReceipts []*types.Receipt
		step           = TxListCompressionPruneStep
	)

	for len(txs) > 0 {
		if len(txs) <= step {
			step = 1
		}

		b, err := encodeAndCompressTxList(txs)
		if err != nil {
			return nil, err
		}
		if len(b) <= int(sizeLimit) {
			return &txsPruningResult{
				TxsPruned:      prunedTxs,
				ReceiptsPruned: prunedReceipts,
				TxsRemaining:   txs,
				Size:           len(b),
			}, nil
		}

		prunedTxs = append(txs[len(txs)-step:], prunedTxs...)
		prunedReceipts = append(receipts[len(receipts)-step:], prunedReceipts...)
		txs = txs[:len(txs)-step]
		receipts = receipts[:len(receipts)-step]
	}

	// All transactions are pruned.
	return &txsPruningResult{
		TxsPruned:         prunedTxs,
		ReceiptsPruned:    prunedReceipts,
		TxsRemaining:      txs,
		ReceiptsRemaining: receipts,
		Size:              0,
	}, nil
}

// accumulateGasUsed sums up the gas used from the receipts.
// setBlockGasLimit pins the block gas limit the protocol dictates on both the
// header and the EVM block context.
//
// prepareWork derives a gas limit from the miner's own target and builds the
// block context from it, so overriding only the header would leave GASLIMIT
// reading that target while the importer reads the header. Keeping the two in
// step is what makes a sealed block re-executable.
func setBlockGasLimit(env *environment, gasLimit uint64) {
	env.header.GasLimit = gasLimit
	env.evm.Context.GasLimit = gasLimit
}

func accumulateGasUsed(receipts []*types.Receipt) uint64 {
	var gasUsed uint64
	for _, receipt := range receipts {
		gasUsed += receipt.GasUsed
	}
	return gasUsed
}
