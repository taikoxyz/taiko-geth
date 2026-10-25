package miner

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
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

	// txListAnchorZkGasReserve is the Unzen zk gas held back for the anchor
	// transaction, which tx-list building does not execute but every derived
	// block starts with. Mainnet anchor-only blocks use about 1.16M zk gas.
	txListAnchorZkGasReserve = 2_000_000

	// txListZkUnfitCacheSize bounds how many transactions tx-list building
	// remembers as too large for any block's zk-gas budget.
	txListZkUnfitCacheSize = 4096
)

// txListZkBudget meters the Unzen zk-gas budget across the lists of one
// tx-list building call. A transaction that overflows the budget is reverted
// and pays nothing, so the work spent running such transactions is capped:
// a rejected transaction is not run again in the same call, one that
// overflows before anything was committed is remembered across calls (with
// the anchor reserve it fits no block at this state and context), and once
// the gas limits of the rejected transactions reach wasteLimit no further
// transaction is selected in this call.
//
// Remembered transactions do not expire. An expiry would let a sender
// stagger its overflowing transactions so that one comes due every block,
// and a single re-run reaches the limit and stops selection.
type txListZkBudget struct {
	meter      *vm.ZkGasMeter
	unfit      *lru.Cache[common.Hash, struct{}] // shared by all calls; may be nil
	rejected   map[common.Hash]struct{}          // overflowed earlier in this call
	committed  bool                              // a transaction was committed in this call
	wastedGas  uint64                            // summed gas limits of rejected transactions
	wasteLimit uint64
	stopped    bool // wastedGas reached wasteLimit
}

// newTxListZkBudget attaches a zk-gas meter with the anchor reserve to env.
func newTxListZkBudget(env *environment, unfit *lru.Cache[common.Hash, struct{}], wasteLimit uint64) (*txListZkBudget, error) {
	meter := vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
	if err := meter.ReserveBlockZkGas(txListAnchorZkGasReserve); err != nil {
		return nil, err
	}
	env.evm.SetZkGasMeter(meter)
	return &txListZkBudget{
		meter:      meter,
		unfit:      unfit,
		rejected:   make(map[common.Hash]struct{}),
		wasteLimit: wasteLimit,
	}, nil
}

// known reports whether the transaction already overflowed the budget, in
// this call or, on its own, in an earlier one.
func (b *txListZkBudget) known(hash common.Hash) bool {
	if _, ok := b.rejected[hash]; ok {
		return true
	}
	return b.unfit != nil && b.unfit.Contains(hash)
}

// commit promotes the in-flight zk gas of a successfully executed transaction.
// Charging already keeps the total within the budget, so this only fails on
// arithmetic overflow.
func (b *txListZkBudget) commit() error {
	if err := b.meter.CommitTransaction(); err != nil {
		return err
	}
	b.committed = true
	return nil
}

// reject records that tx overflowed the budget, and stops selection for the
// rest of the call once the work spent on rejected transactions reaches the
// limit.
func (b *txListZkBudget) reject(tx *types.Transaction) {
	b.rejected[tx.Hash()] = struct{}{}
	if b.unfit != nil && !b.committed {
		b.unfit.Add(tx.Hash(), struct{}{})
	}
	b.wastedGas += tx.Gas()
	if b.wastedGas >= b.wasteLimit {
		b.stopped = true
	}
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
		if zkBudget, err = newTxListZkBudget(env, w.txListZkUnfit, blockMaxGasLimit); err != nil {
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
		if zkBudget != nil && zkBudget.known(tx.Hash()) {
			log.Trace("Skipping transaction known to overflow the zk gas budget", "hash", tx.Hash())
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
			// reverted; leave it and the sender's later transactions out and
			// keep filling the list with transactions that may still fit, until
			// rejected transactions have cost a block's worth of gas.
			log.Trace("Skipping transaction over the zk gas budget", "hash", ltx.Hash, "sender", from, "remaining", zkGasMeter.RemainingBlockZkGas())
			zkGasMeter.ResetTransaction()
			env.evm.ResetZkGasErr()
			txs.Pop()
			if zkBudget.reject(tx); zkBudget.stopped {
				log.Debug("Stopping tx-list selection after zk gas overflows", "wastedGas", zkBudget.wastedGas, "limit", zkBudget.wasteLimit)
				break loop
			}

		case errors.Is(err, nil):
			if zkBudget != nil {
				if err := zkBudget.commit(); err != nil {
					return nil, err
				}
			}
			// Everything ok, collect the logs and shift in the next transaction from the same account
			txs.Shift()

			// Check the size of the compressed txList, if it exceeds the maxBytesPerTxList, break the loop.
			if env.tcount%TxListCompressionCheckInterval == 0 {
				if pruningResult, err = pruneTransactions(env.txs, env.receipts, maxBytesPerTxList); err != nil {
					return nil, err
				}
				// If there are pruned transactions, break the loop.
				if len(pruningResult.TxsPruned) > 0 {
					break loop
				}
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
	b, err := encodeAndCompressTxList(types.Transactions{tx})
	if err != nil {
		return false, err
	}
	return uint64(len(b)) <= maxBytesPerTxList, nil
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
