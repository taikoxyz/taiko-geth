package miner

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

func (g *GattacaWorker) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	log.Info("gattaca StateAndHeaderByNumberOrHash")
	if number, ok := blockNrOrHash.Number(); ok {
		log.Info("gattaca StateAndHeaderByNumberOrHash", "number", number)
		if int64(number) == g.preconfHead.header.Number.Int64() {
			return g.preconfHead.state, g.preconfHead.header, nil
		}
		if entry, in := g.mapBlockNumber[int64(number)]; in {
			return entry.env.state, entry.env.header, nil
		}
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		log.Info("gattaca StateAndHeaderByNumberOrHash", "hash", hash.Hex())
		if entry, in := g.mapBlockHash[hash.Hex()]; in {
			return entry.env.state, entry.env.header, nil
		}
	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")

}

func (g *GattacaWorker) GetPendingPoolNonce(address common.Address) uint64 {
	log.Info("getting pending pool nonce")
	return g.preconfHead.state.GetNonce(address)
}

func (g *GattacaWorker) GetBlockByHash(ctx context.Context, hash common.Hash, fullTx bool) *types.Block {
	if entry, ok := g.mapBlockHash[hash.Hex()]; ok {
		return entry.block
	}
	return nil
}

func (g *GattacaWorker) GetBlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	if entry, ok := g.mapBlockNumber[blockNr.Int64()]; ok {
		return entry.block, nil
	}
	return nil, nil
}

func (g *GattacaWorker) BlockNumber() uint64 {
	if len(g.builtBlocks) > 0 {
		return g.builtBlocks[len(g.builtBlocks)-1].block.NumberU64()
	}
	return 0
}
