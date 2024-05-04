package miner

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
)

// buildTransactionsLists is the inner method that builds multiple
// transactions lists which satisfy all the given conditions
// 1. All transactions should all be able to pay the given base fee.
// 2. The total gas used should not exceed the given blockMaxGasLimit
// 3. The total bytes used should not exceed the given maxBytesPerTxList
// 4. The total number of transactions lists should not exceed the given maxTransactionsLists
func (m *Miner) buildTransactionsLists(
	beneficiary common.Address,
	baseFee *big.Int,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	localAccounts []string,
	maxTransactionsLists uint64,
) ([]*PreBuiltTxList, error) {
	var (
		txsLists    []*PreBuiltTxList
		currentHead = m.chain.CurrentBlock()
	)

	if currentHead == nil {
		return nil, fmt.Errorf("failed to find current head")
	}

	// Check if tx pool is empty at first.
	if len(m.txpool.Pending(txpool.PendingFilter{BaseFee: uint256.MustFromBig(baseFee), OnlyPlainTxs: true})) == 0 {
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

	env, err := m.prepareWork(params)
	if err != nil {
		return nil, err
	}

	var (
		signer = types.MakeSigner(m.chainConfig, new(big.Int).Add(currentHead.Number, common.Big1), currentHead.Time)
		// Split the pending transactions into locals and remotes, then
		// fill the block with all available pending transactions.
		localTxs, remoteTxs = m.getPendingTxs(localAccounts, baseFee)
	)

	commitTxs := func() (*PreBuiltTxList, error) {
		env.tcount = 0
		env.txs = []*types.Transaction{}
		env.gasPool = new(core.GasPool).AddGas(blockMaxGasLimit)
		env.header.GasLimit = blockMaxGasLimit

		var (
			locals  = make(map[common.Address][]*txpool.LazyTransaction)
			remotes = make(map[common.Address][]*txpool.LazyTransaction)
		)

		for address, txs := range localTxs {
			locals[address] = txs
		}
		for address, txs := range remoteTxs {
			remotes[address] = txs
		}

		m.commitL2Transactions(
			env,
			newTransactionsByPriceAndNonce(signer, locals, baseFee),
			newTransactionsByPriceAndNonce(signer, remotes, baseFee),
			maxBytesPerTxList,
		)

		b, err := encodeAndComporeessTxList(env.txs)
		if err != nil {
			return nil, err
		}

		return &PreBuiltTxList{
			TxList:           env.txs,
			EstimatedGasUsed: env.header.GasLimit - env.gasPool.Gas(),
			BytesLength:      uint64(len(b)),
		}, nil
	}

	for i := 0; i < int(maxTransactionsLists); i++ {
		res, err := commitTxs()
		if err != nil {
			return nil, err
		}

		if len(res.TxList) == 0 {
			break
		}

		txsLists = append(txsLists, res)
	}

	return txsLists, nil
}

// sealBlockWith mines and seals a block with the given block metadata.
func (m *Miner) sealBlockWith(
	parent common.Hash,
	timestamp uint64,
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
		noTxs:         false,
		baseFeePerGas: baseFeePerGas,
	}

	env, err := m.prepareWork(params)
	if err != nil {
		return nil, err
	}

	env.header.Extra = blkMeta.ExtraData
	env.header.GasLimit = blkMeta.GasLimit

	// Commit transactions.
	gasLimit := env.header.GasLimit
	rules := m.chain.Config().Rules(env.header.Number, true, timestamp)

	env.gasPool = new(core.GasPool).AddGas(gasLimit)

	for i, tx := range txs {
		if i == 0 {
			if err := tx.MarkAsAnchor(); err != nil {
				return nil, err
			}
		}
		sender, err := types.LatestSignerForChainID(tx.ChainId()).Sender(tx)
		if err != nil {
			log.Info("Skip an invalid proposed transaction", "hash", tx.Hash(), "reason", err)
			continue
		}

		env.state.Prepare(rules, sender, blkMeta.Beneficiary, tx.To(), vm.ActivePrecompiles(rules), tx.AccessList())
		env.state.SetTxContext(tx.Hash(), env.tcount)
		if err := m.commitTransaction(env, tx); err != nil {
			log.Info("Skip an invalid proposed transaction", "hash", tx.Hash(), "reason", err)
			continue
		}
		env.tcount++
	}

	block, err := m.engine.FinalizeAndAssemble(m.chain, env.header, env.state, nil, env.receipts)
	if err != nil {
		return nil, err
	}

	results := make(chan *types.Block, 1)
	if err := m.engine.Seal(m.chain, block, results, nil); err != nil {
		return nil, err
	}
	block = <-results

	return block, nil
}

// getPendingTxs fetches the pending transactions from tx pool.
func (m *Miner) getPendingTxs(localAccounts []string, baseFee *big.Int) (
	map[common.Address][]*txpool.LazyTransaction,
	map[common.Address][]*txpool.LazyTransaction,
) {
	pending := m.txpool.Pending(txpool.PendingFilter{OnlyPlainTxs: true, BaseFee: uint256.MustFromBig(baseFee)})
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

// commitL2Transactions tries to commit the transactions into the given state.
func (m *Miner) commitL2Transactions(
	env *environment,
	txsLocal *transactionsByPriceAndNonce,
	txsRemote *transactionsByPriceAndNonce,
	maxBytesPerTxList uint64,
) {
	var (
		txs     = txsLocal
		isLocal = true
	)

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
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		from, _ := types.Sender(env.signer, tx)

		b, err := encodeAndComporeessTxList(append(env.txs, tx))
		if err != nil {
			log.Trace("Failed to rlp encode and compress the pending transaction %s: %w", tx.Hash(), err)
			txs.Pop()
			continue
		}
		if len(b) >= int(maxBytesPerTxList) {
			break
		}

		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !m.chainConfig.IsEIP155(env.header.Number) {
			log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", m.chainConfig.EIP155Block)

			txs.Pop()
			continue
		}
		// Start executing the transaction
		env.state.SetTxContext(tx.Hash(), env.tcount)

		err = m.commitTransaction(env, tx)
		switch {
		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "hash", ltx.Hash, "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, nil):
			// Everything ok, collect the logs and shift in the next transaction from the same account
			env.tcount++
			txs.Shift()

		default:
			// Transaction is regarded as invalid, drop all consecutive transactions from
			// the same sender because of `nonce-too-high` clause.
			log.Trace("Transaction failed, account skipped", "hash", ltx.Hash, "err", err)
			txs.Pop()
		}
	}
}

// encodeAndComporeessTxList encodes and compresses the given transactions list.
func encodeAndComporeessTxList(txs types.Transactions) ([]byte, error) {
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

	if err := w.Flush(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}
