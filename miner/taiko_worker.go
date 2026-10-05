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
)

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
		env.header.GasLimit = blockMaxGasLimit

		result, err := w.commitL2Transactions(
			env,
			pruningResult.TxsPruned,
			pruningResult.ReceiptsPruned,
			newTransactionsByPriceAndNonce(signer, maps.Clone(localTxs), baseFee),
			newTransactionsByPriceAndNonce(signer, maps.Clone(remoteTxs), baseFee),
			maxBytesPerTxList,
			minTip,
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

// taikoParentBeaconRoot returns the parent beacon root of a Taiko Unzen block
// built from genParams. From Etna on it is the state root of the L1 block at
// the final anchorBlockNumber, which the caller must supply as a non-zero
// root. Before Etna it is the canonical zero hash: a non-zero root cannot
// survive the engine round trip and is rejected, except in transaction-pool
// preselection, which only simulates its target.
func taikoParentBeaconRoot(config *params.ChainConfig, time uint64, genParams *generateParams) (*common.Hash, error) {
	root := new(common.Hash)
	if genParams.beaconRoot != nil {
		*root = *genParams.beaconRoot
	}
	if config.IsEtna(time) {
		if *root == (common.Hash{}) {
			return nil, errors.New("missing non-zero parent beacon root for an Etna block")
		}
		return root, nil
	}
	if *root != (common.Hash{}) && !genParams.taikoPreselection {
		return nil, fmt.Errorf("non-zero parent beacon root %v is unsupported on Taiko", *root)
	}
	return root, nil
}

// etnaSkippedTxErrors are the errors for which the Etna sealer skips a
// proposed transaction: the transaction is invalid against the block or the
// sender's state, or its gas limit exceeds the remaining block gas. Any other
// error from applying a transaction aborts an Etna build.
var etnaSkippedTxErrors = []error{
	types.ErrInvalidSig,
	types.ErrInvalidChainId,
	core.ErrNonceTooLow,
	core.ErrNonceTooHigh,
	core.ErrNonceMax,
	core.ErrGasLimitReached,
	core.ErrGasLimitTooHigh,
	core.ErrInsufficientFunds,
	core.ErrInsufficientFundsForTransfer,
	core.ErrGasUintOverflow,
	core.ErrIntrinsicGas,
	core.ErrFloorDataGas,
	core.ErrTxTypeNotSupported,
	core.ErrTipAboveFeeCap,
	core.ErrTipVeryHigh,
	core.ErrFeeCapVeryHigh,
	core.ErrFeeCapTooLow,
	core.ErrSenderNoEOA,
	core.ErrBlobFeeCapTooLow,
	core.ErrMissingBlobHashes,
	core.ErrTooManyBlobs,
	core.ErrBlobTxCreate,
	core.ErrEmptyAuthList,
	core.ErrSetCodeTxCreate,
	vm.ErrMaxInitCodeSizeExceeded,
}

// isEtnaSkippedTxError reports whether the Etna sealer skips a transaction
// that failed with err.
func isEtnaSkippedTxError(err error) bool {
	for _, target := range etnaSkippedTxErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// sealBlockWith mines and seals a block from the given payload attributes.
//
// From Etna on the block has no anchor transaction and its header comes from
// the attributes: the timestamp, beneficiary, gas limit and extra data of the
// block metadata, prevRandao as the mix digest, the base fee as given, and the
// parent beacon root, which is the state root of the L1 block at the final
// anchorBlockNumber. Its withdrawals are always empty. A transaction list that
// does not decode seals an empty block. Every position, the first included,
// is ordinary: blob transactions, transactions whose signer cannot be
// recovered and invalid transactions are skipped, zk-gas exhaustion ends the
// block before the exhausting transaction, and any other error aborts the
// build.
func (w *Miner) sealBlockWith(
	parent *types.Header,
	parentBlockTime uint64,
	attrs *engine.PayloadAttributes,
) (*types.Block, error) {
	var (
		timestamp   = attrs.Timestamp
		blkMeta     = attrs.BlockMetadata
		withdrawals = attrs.Withdrawals
		isEtna      = w.chainConfig.IsEtna(timestamp)
	)
	if isEtna {
		// The engine API rejects Etna attributes whose two timestamps differ,
		// so the header time is the block metadata timestamp either way.
		if blkMeta.Timestamp != timestamp {
			return nil, fmt.Errorf("block metadata timestamp %d differs from payload timestamp %d", blkMeta.Timestamp, timestamp)
		}
		withdrawals = make(types.Withdrawals, 0)
	}

	// Decode transactions bytes. From Etna on, the list is decoded with the
	// shared execution transaction grammar, and a list that does not decode
	// under it seals an empty block instead of failing.
	var txs types.Transactions
	if isEtna {
		decoded, err := decodeEtnaTxList(blkMeta.TxList)
		if err != nil {
			log.Debug("Failed to decode txList, sealing an empty Etna block", "err", err)
		}
		txs = decoded
	} else if err := rlp.DecodeBytes(blkMeta.TxList, &txs); err != nil {
		return nil, fmt.Errorf("failed to decode txList: %w", err)
	}

	// Before Etna every L2 block starts with its anchor transaction; Etna
	// blocks have none and may be empty.
	if len(txs) == 0 && !isEtna {
		return nil, fmt.Errorf("too less transactions in the block")
	}

	genParams := &generateParams{
		timestamp:     timestamp,
		forceTime:     true,
		parentHash:    parent.Hash(),
		coinbase:      blkMeta.Beneficiary,
		random:        blkMeta.MixHash,
		withdrawals:   withdrawals,
		noTxs:         false,
		baseFeePerGas: attrs.BaseFeePerGas,
	}
	if isEtna {
		// Etna blocks take their randomness, base fee, extra data and parent
		// beacon root from the payload attributes as given. The base fee is
		// not recomputed here: import validates it.
		genParams.random = attrs.Random
		genParams.beaconRoot = attrs.BeaconRoot
		genParams.forceOverrides = true
		genParams.overrideExtraData = blkMeta.ExtraData
	} else if w.chainConfig.IsShasta(timestamp) {
		genParams.baseFeePerGas = misc.CalcEIP4396BaseFee(w.chainConfig, parent, parentBlockTime)
	}

	// Set extraData
	w.SetExtra(blkMeta.ExtraData)

	ctx := context.Background()
	env, err := w.prepareWork(ctx, genParams, false)
	if err != nil {
		return nil, err
	}

	env.header.GasLimit = blkMeta.GasLimit
	if isEtna {
		// prepareWork built the EVM block context with the miner's own gas
		// limit target. Etna transactions read the metadata gas limit through
		// GASLIMIT instead, as they do on import; before Etna the sealing EVM
		// keeps that target.
		env.evm.Context.GasLimit = env.header.GasLimit
	}

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
		// Before Etna the first transaction is the anchor: it is marked so the
		// state transition grants its exemptions, and it may neither fail nor
		// be truncated. Etna blocks have no anchor transaction.
		isAnchor := i == 0 && w.chainConfig.HasTaikoAnchor(timestamp)
		if isAnchor {
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
			// The anchor is never discarded — it must always be in the block.
			if zkGasMeter != nil && errors.Is(err, vm.ErrZkGasLimitExceeded) && !isAnchor {
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
			if isAnchor {
				return nil, fmt.Errorf("anchor transaction failed: %w", err)
			}
			// From Etna on only an invalid transaction is skipped; before
			// Etna every failing transaction is.
			if isEtna && !isEtnaSkippedTxError(err) {
				return nil, fmt.Errorf("failed to apply transaction %d (%v): %w", i, tx.Hash(), err)
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
				if isAnchor {
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
) (*txsPruningResult, error) {
	var (
		txs           = txsLocal
		isLocal       = true
		pruningResult *txsPruningResult
		err           error
	)

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
		// Start executing the transaction
		env.state.SetTxContext(tx.Hash(), env.tcount)

		err := w.commitTransaction(context.Background(), env, tx)
		switch {
		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "hash", ltx.Hash, "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, nil):
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
func accumulateGasUsed(receipts []*types.Receipt) uint64 {
	var gasUsed uint64
	for _, receipt := range receipts {
		gasUsed += receipt.GasUsed
	}
	return gasUsed
}
