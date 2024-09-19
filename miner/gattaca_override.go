package miner

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (g *GattacaWorker) GetTransactionReceipt(ctx context.Context, hash common.Hash) map[string]interface{} {
	found, tx, receipt, txIndex, blockNumber, blockHash, header := g.getTransaction(ctx, hash)
	if !found {
		return nil
	}
	// Derive the sender.
	signer := types.MakeSigner(g.chainConfig, header.Number, header.Time)
	return marshalReceipt(receipt, blockHash, blockNumber, signer, tx, txIndex)
}

func (g *GattacaWorker) getTransaction(ctx context.Context, hash common.Hash) (bool, *types.Transaction, *types.Receipt, int, uint64, common.Hash, *types.Header) {
	var tx *types.Transaction
	var receipt *types.Receipt
	var txIdx int
	var blockNumber uint64
	var blockHash common.Hash
	var header *types.Header
	pendingPreconfBlock, err := g.preconfState.getPendingPreconfBlock()
	if err == nil {
		for idx, tempTx := range pendingPreconfBlock.txs {
			if tempTx.Hash().Hex() == hash.Hex() {
				tx = tempTx
				receipt = pendingPreconfBlock.receipts[idx]
				txIdx = idx
				blockNumber = pendingPreconfBlock.header.Number.Uint64()
				blockHash = pendingPreconfBlock.header.Hash()
				header = pendingPreconfBlock.header
				break
			}
		}
	}

	if tx == nil || err != nil {
		for _, envBlocks := range g.PreconfState().getSealedPreconfBlock() {
			for idx, tempTx := range envBlocks.txs {
				if tempTx.Hash().Hex() == hash.Hex() {
					tx = tempTx
					receipt = envBlocks.receipts[idx]
					txIdx = idx
					blockNumber = envBlocks.sealedBlock.NumberU64()
					blockHash = envBlocks.sealedBlock.Hash()
					header = envBlocks.header
					break
				}
			}
			if tx != nil {
				break
			}
		}
	}
	return tx != nil, tx, receipt, txIdx, blockNumber, blockHash, header
}

func (g *GattacaWorker) StateAndHeaderByNumberOrHash(blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if number, ok := blockNrOrHash.Number(); ok {
		if number == rpc.LatestBlockNumber {
			env, _ := g.retrieveEnv(1)
			preconfEnv, err := g.preconfState.getPendingPreconfBlock()
			if err == nil {
				if preconfEnv.header.Number.Uint64() > env.header.Number.Uint64() {
					return env.state, env.header, nil
				} else {
					return nil, nil, errors.New(fmt.Sprintf("preconf head is older than chain head"))
				}
			}
		}
		if env := findBlockByNumber(g.preconfState.getSealedPreconfBlock(), uint64(number)); env != nil {
			return env.state, env.header, nil
		}
		return nil, nil, errors.New(fmt.Sprintf("block number %d not found", number.Int64()))
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		log.Info("hash", "hash", hash.Hex())
		if env := findBlockByHash(g.preconfState.getSealedPreconfBlock(), hash); env != nil {
			return env.state, env.header, nil
		}
		return nil, nil, errors.New(fmt.Sprintf("block hash %s not found", hash.Hex()))
	}
	return nil, nil, nil
}

func findBlockByNumber(envs []*environment, number uint64) *environment {
	for _, env := range envs {
		if env.sealedBlock.NumberU64() == number {
			return env
		}
	}
	return nil
}

func findBlockByHash(envs []*environment, hash common.Hash) *environment {
	for _, env := range envs {
		if env.sealedBlock.Hash().Hex() == hash.Hex() {
			return env
		}
	}
	return nil
}
