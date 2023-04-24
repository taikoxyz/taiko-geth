package miner

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// sealBlockWith mines and seals a block with the given block metadata.
func (w *worker) sealBlockWith(
	parent common.Hash,
	timestamp uint64,
	blkMeta *engine.BlockMetadata,
	baseFeePerGas *big.Int,
	withdraws types.Withdrawals,
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
		withdrawals:   withdraws,
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
	env.header.GasLimit = blkMeta.GasLimit
	env.header.Extra = blkMeta.ExtraData

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

	block, err := w.engine.FinalizeAndAssemble(w.chain, env.header, env.state, env.txs, nil, env.receipts, nil)
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
