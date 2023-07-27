package miner

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	errGasUsedLimitReached = errors.New("gas used limit reached")
)

func (w *worker) BuildTransactionsLists(
	beneficiary common.Address,
	baseFee *big.Int,
	maxTransactionsPerBlock uint64,
	blockMaxGasUsedLimit uint64,
	maxBytesPerTxList uint64,
	minTxGasLimit uint64,
	locals []string,
	maxTransactionsLists uint64,
) ([]types.Transactions, error) {
	var (
		txsLists []types.Transactions
	)

	currentHead := w.chain.CurrentBlock()
	if currentHead == nil {
		return nil, fmt.Errorf("failed to find current head")
	}

	params := &generateParams{
		timestamp:     uint64(time.Now().Unix()),
		forceTime:     true,
		parentHash:    currentHead.Hash(),
		coinbase:      beneficiary,
		random:        currentHead.MixDigest,
		noUncle:       true,
		noTxs:         false,
		baseFeePerGas: baseFee,
	}

	env, err := w.prepareWork(params)
	if err != nil {
		return nil, err
	}
	defer env.discard()

	// Make gas limit infinite, L2 blocks will use gasUsed as real gas limit.
	env.gasPool = new(core.GasPool).AddGas(math.MaxUint64)
	env.header.GasLimit = math.MaxUint64

	// Split the pending transactions into locals and remotes, then
	// fill the block with all available pending transactions.
	pending := w.eth.TxPool().Pending(true)
	localTxs, remoteTxs := make(map[common.Address]types.Transactions), pending
	for _, local := range locals {
		account := common.HexToAddress(local)
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}

	if len(pending) == 0 {
		return txsLists, nil
	}

	commitTxs := func(txs *types.TransactionsByPriceAndNonce) error {
		for len(txsLists) < int(maxTransactionsLists) {
			allTxsCommitted, isTxListFull, err := w.commitL2Transactions(env, txs, blockMaxGasUsedLimit)
			if err != nil {
				return err
			}

			if isTxListFull {
				txsLists = append(txsLists, env.txs)
				env.txs = []*types.Transaction{}
				env.receipts = []*types.Receipt{}
				env.tcount = 0
			}

			if allTxsCommitted {
				break
			}
		}

		return nil
	}

	if len(localTxs) > 0 {
		if err := commitTxs(types.NewTransactionsByPriceAndNonce(env.signer, localTxs, env.header.BaseFee)); err != nil {
			return nil, err
		}
	}
	if len(remoteTxs) > 0 {
		if err := commitTxs(types.NewTransactionsByPriceAndNonce(env.signer, remoteTxs, env.header.BaseFee)); err != nil {
			return nil, err
		}
	}

	if len(txsLists) < int(maxTransactionsLists) && len(env.txs) != 0 {
		txsLists = append(txsLists, env.txs)
	}

	return txsLists, nil
}

// sealBlockWith mines and seals a block with the given block metadata.
func (w *worker) sealBlockWith(
	parent common.Hash,
	timestamp uint64,
	blkMeta *engine.BlockMetadata,
	baseFeePerGas *big.Int,
	withdrawals types.Withdrawals,
	withdrawalsHash common.Hash,
) (*types.Block, error) {
	// Decode transactions bytes.
	var txs types.Transactions
	if err := rlp.DecodeBytes(blkMeta.TxList, &txs); err != nil {
		return nil, fmt.Errorf("failed to decode txList: %w", err)
	}

	if len(txs) == 0 {
		// A L2 block needs to have have at least one `V1TaikoL2.anchor` or
		// `V1TaikoL2.invalidateBlock` transaction.
		return nil, fmt.Errorf("too less transactions in the block")
	}

	params := &generateParams{
		timestamp:     timestamp,
		forceTime:     true,
		parentHash:    parent,
		coinbase:      blkMeta.Beneficiary,
		random:        blkMeta.MixHash,
		withdrawals:   withdrawals,
		noUncle:       true,
		noTxs:         false,
		baseFeePerGas: baseFeePerGas,
	}

	env, err := w.prepareWork(params)
	if err != nil {
		return nil, err
	}
	defer env.discard()

	// Set the block fields using the given block metadata:
	// 1. gas limit
	// 2. extra data
	// 3. withdrawals hash
	env.header.GasLimit = blkMeta.GasLimit
	env.header.Extra = blkMeta.ExtraData
	env.header.WithdrawalsHash = &withdrawalsHash

	// Commit transactions.
	commitErrs := make([]error, 0, len(txs))
	gasLimit := env.header.GasLimit
	rules := w.chain.Config().Rules(env.header.Number, true, timestamp)

	env.gasPool = new(core.GasPool).AddGas(gasLimit)

	for i, tx := range txs {
		sender, err := types.LatestSignerForChainID(tx.ChainId()).Sender(tx)
		if err != nil {
			log.Info("Skip an invalid proposed transaction", "hash", tx.Hash(), "reason", err)
			commitErrs = append(commitErrs, err)
			continue
		}

		env.state.Prepare(rules, sender, blkMeta.Beneficiary, tx.To(), vm.ActivePrecompiles(rules), tx.AccessList())
		env.state.SetTxContext(tx.Hash(), env.tcount)
		if _, err := w.commitTransaction(env, tx, i == 0); err != nil {
			log.Info("Skip an invalid proposed transaction", "hash", tx.Hash(), "reason", err)
			commitErrs = append(commitErrs, err)
			continue
		}
		env.tcount++
	}
	// TODO: save the commit transactions errors for generating witness.
	_ = commitErrs

	block, err := w.engine.FinalizeAndAssemble(w.chain, env.header, env.state, env.txs, nil, env.receipts, withdrawals)
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

func (w *worker) commitL2Transactions(
	env *environment,
	txs *types.TransactionsByPriceAndNonce,
	gasUsedLimit uint64,
) (bool, bool, error) {
	var (
		accGasUsed      uint64
		allTxsCommitted bool
		isTxListFull    bool
	)

loop:
	for {
		// If we don't have enough gas for any further transactions then we're done.
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			break
		}

		if accGasUsed >= gasUsedLimit {
			break
		}

		// Retrieve the next transaction and abort if all done.
		tx := txs.Peek()
		if tx == nil {
			allTxsCommitted = true
			break
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

		_, err := w.commitL2Transaction(env, tx, accGasUsed, gasUsedLimit)
		switch {
		case errors.Is(err, errGasUsedLimitReached):
			log.Trace("GasUsed limit exceeded for current block", "sender", from)
			isTxListFull = true
			break loop

		case errors.Is(err, core.ErrGasLimitReached):
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, core.ErrNonceTooHigh):
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Trace("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
			txs.Pop()

		case errors.Is(err, nil):
			// Everything ok, shift in the next transaction from the same account
			env.tcount++
			accGasUsed += env.receipts[len(env.receipts)-1].GasUsed
			txs.Shift()

		case errors.Is(err, types.ErrTxTypeNotSupported):
			// Pop the unsupported transaction without shifting in the next from the account
			log.Trace("Skipping unsupported transaction type", "sender", from, "type", tx.Type())
			txs.Pop()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
		}
	}

	return allTxsCommitted, isTxListFull, nil
}

func (w *worker) commitL2Transaction(
	env *environment,
	tx *types.Transaction,
	accGasUsed uint64,
	gasUsedLimit uint64,
) (*types.Receipt, error) {
	var (
		snap = env.state.Snapshot()
		gp   = env.gasPool.Gas()
	)
	receipt, err := core.ApplyTransaction(
		w.chainConfig,
		w.chain,
		&env.coinbase,
		env.gasPool,
		env.state,
		env.header,
		tx,
		&env.header.GasUsed,
		*w.chain.GetVMConfig(),
		false,
	)
	if err != nil {
		env.state.RevertToSnapshot(snap)
		env.gasPool.SetGas(gp)
		return nil, err
	}
	if accGasUsed+receipt.GasUsed > gasUsedLimit {
		env.state.RevertToSnapshot(snap)
		env.gasPool.SetGas(gp)
		return nil, errGasUsedLimitReached
	}
	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return receipt, nil
}
