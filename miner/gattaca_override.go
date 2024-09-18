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
	for idx, tempTx := range g.preconfHead.txs {
		if tempTx.Hash().Hex() == hash.Hex() {
			tx = tempTx
			receipt = g.preconfHead.receipts[idx]
			txIdx = idx
			blockNumber = g.preconfHead.header.Number.Uint64()
			blockHash = g.preconfHead.header.Hash()
			header = g.preconfHead.header
			break
		}
	}
	if tx == nil {
		for _, entry := range g.builtBlocks {
			for idx, tempTx := range entry.block.Transactions() {
				if tempTx.Hash().Hex() == hash.Hex() {
					tx = tempTx
					receipt = entry.env.receipts[idx]
					txIdx = idx
					blockNumber = entry.block.NumberU64()
					blockHash = entry.block.Hash()
					header = entry.env.header
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

func (g *GattacaWorker) BlockNumber() uint64 {
	env, _ := g.retrieveEnv(1)
	if env.header.Number.Uint64() > g.preconfHead.header.Number.Uint64() {
		return env.header.Number.Uint64()
	}
	return g.preconfHead.header.Number.Uint64()
}

func (g *GattacaWorker) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if entry, in := g.mapBlockHash[hash.Hex()]; in {
		return entry.block, nil
	}
	return nil, errors.New(fmt.Sprintf("block hash %s not found", hash.Hex()))
}

func (g *GattacaWorker) BlockByNumber(ctx context.Context, number rpc.BlockNumber, latestChainBlock *types.Block) (*types.Block, error) {
	if number == rpc.LatestBlockNumber {
		if len(g.builtBlocks) > 0 {
			block := g.builtBlocks[len(g.builtBlocks)-1].block
			if latestChainBlock == nil || block.NumberU64() > latestChainBlock.NumberU64() {
				return block, nil
			}
			return latestChainBlock, nil
		}
	}
	if entry, in := g.mapBlockNumber[number.Int64()]; in {
		return entry.block, nil
	}
	return nil, errors.New(fmt.Sprintf("block number %d not found", number.Int64()))
}

func (g *GattacaWorker) GetPoolNonce(ctx context.Context, addr common.Address) uint64 {
	return g.preconfHead.state.GetNonce(addr)
}

func (g *GattacaWorker) StateAndHeaderByNumberOrHash(blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if number, ok := blockNrOrHash.Number(); ok {
		if number == rpc.LatestBlockNumber {
			env, _ := g.retrieveEnv(1)
			if g.preconfHead.header.Number.Uint64() > env.header.Number.Uint64() {
				return env.state, env.header, nil
			} else {
				return nil, nil, errors.New(fmt.Sprintf("preconf head is older than chain head"))
			}
		}
		if entry, in := g.mapBlockNumber[number.Int64()]; in {
			return entry.env.state, entry.env.header, nil
		}
		return nil, nil, errors.New(fmt.Sprintf("block number %d not found", number.Int64()))
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		log.Info("hash", "hash", hash.Hex())
		if entry, in := g.mapBlockHash[hash.Hex()]; in {
			return entry.env.state, entry.env.header, nil
		}
		return nil, nil, errors.New(fmt.Sprintf("block hash %s not found", hash.Hex()))
	}
	return nil, nil, nil
}
