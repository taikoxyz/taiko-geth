// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// EthAPIBackend implements ethapi.Backend and tracers.Backend for full nodes
type GattacaAPIBackend struct {
	extRPCEnabled       bool
	allowUnprotectedTxs bool
	eth                 *Ethereum
	gpo                 *gasprice.Oracle
	preconfirmationURL  string // change(taiko)
	preconfState        *miner.PreconfState
}

// GetPreconfirmationForwardingURL
func (b *GattacaAPIBackend) GetPreconfirmationForwardingURL() string {
	return b.preconfirmationURL
}

// ChainConfig returns the active chain configuration.
func (b *GattacaAPIBackend) ChainConfig() *params.ChainConfig {
	return b.eth.blockchain.Config()
}

func (b *GattacaAPIBackend) CurrentBlock() *types.Header {

	return b.eth.blockchain.CurrentBlock()
}

func (b *GattacaAPIBackend) SetHead(number uint64) {
	b.eth.handler.downloader.Cancel()
	b.eth.blockchain.SetHead(number)
}

func (b *GattacaAPIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		block := b.eth.miner.PendingBlock()
		if block == nil {
			return nil, errors.New("pending block is not available")
		}
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		return b.eth.blockchain.CurrentBlock(), nil
	}
	if number == rpc.FinalizedBlockNumber {
		block := b.eth.blockchain.CurrentFinalBlock()
		if block == nil {
			return nil, errors.New("finalized block not found")
		}
		return block, nil
	}
	if number == rpc.SafeBlockNumber {
		block := b.eth.blockchain.CurrentSafeBlock()
		if block == nil {
			return nil, errors.New("safe block not found")
		}
		return block, nil
	}
	return b.eth.blockchain.GetHeaderByNumber(uint64(number)), nil
}

func (b *GattacaAPIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.eth.blockchain.GetHeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		return header, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *GattacaAPIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.eth.blockchain.GetHeaderByHash(hash), nil
}

func (b *GattacaAPIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		block := b.eth.miner.PendingBlock()
		if block == nil {
			return nil, errors.New("pending block is not available")
		}
		return block, nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		header := b.eth.blockchain.CurrentBlock()
		return b.eth.blockchain.GetBlock(header.Hash(), header.Number.Uint64()), nil
	}
	if number == rpc.FinalizedBlockNumber {
		header := b.eth.blockchain.CurrentFinalBlock()
		if header == nil {
			return nil, errors.New("finalized block not found")
		}
		return b.eth.blockchain.GetBlock(header.Hash(), header.Number.Uint64()), nil
	}
	if number == rpc.SafeBlockNumber {
		header := b.eth.blockchain.CurrentSafeBlock()
		if header == nil {
			return nil, errors.New("safe block not found")
		}
		return b.eth.blockchain.GetBlock(header.Hash(), header.Number.Uint64()), nil
	}
	return b.eth.blockchain.GetBlockByNumber(uint64(number)), nil
}

func (b *GattacaAPIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.eth.blockchain.GetBlockByHash(hash), nil
}

// GetBody returns body of a block. It does not resolve special block numbers.
func (b *GattacaAPIBackend) GetBody(ctx context.Context, hash common.Hash, number rpc.BlockNumber) (*types.Body, error) {
	if number < 0 || hash == (common.Hash{}) {
		return nil, errors.New("invalid arguments; expect hash and no special block numbers")
	}
	if body := b.eth.blockchain.GetBody(hash); body != nil {
		return body, nil
	}
	return nil, errors.New("block body not found")
}

func (b *GattacaAPIBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.eth.blockchain.GetHeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		block := b.eth.blockchain.GetBlock(hash, header.Number.Uint64())
		if block == nil {
			return nil, errors.New("header found, but block body is missing")
		}
		return block, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *GattacaAPIBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return b.eth.miner.PendingBlockAndReceipts()
}

func (b *GattacaAPIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the miner
	if number == rpc.PendingBlockNumber {
		block, state := b.eth.miner.Pending()
		if block == nil || state == nil {
			return nil, nil, errors.New("pending state is not available")
		}
		return state, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	stateDb, err := b.eth.BlockChain().StateAt(header.Root)
	if err != nil {
		return nil, nil, err
	}
	return stateDb, header, nil
}

func (b *GattacaAPIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, nil, err
		}
		if header == nil {
			return nil, nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, nil, errors.New("hash is not currently canonical")
		}
		stateDb, err := b.eth.BlockChain().StateAt(header.Root)
		if err != nil {
			return nil, nil, err
		}
		return stateDb, header, nil
	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *GattacaAPIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return b.eth.blockchain.GetReceiptsByHash(hash), nil
}

func (b *GattacaAPIBackend) GetLogs(ctx context.Context, hash common.Hash, number uint64) ([][]*types.Log, error) {
	return rawdb.ReadLogs(b.eth.chainDb, hash, number), nil
}

func (b *GattacaAPIBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	if header := b.eth.blockchain.GetHeaderByHash(hash); header != nil {
		return b.eth.blockchain.GetTd(hash, header.Number.Uint64())
	}
	return nil
}

func (b *GattacaAPIBackend) GetEVM(ctx context.Context, msg *core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config, blockCtx *vm.BlockContext) *vm.EVM {
	if vmConfig == nil {
		vmConfig = b.eth.blockchain.GetVMConfig()
	}
	txContext := core.NewEVMTxContext(msg)
	var context vm.BlockContext
	if blockCtx != nil {
		context = *blockCtx
	} else {
		context = core.NewEVMBlockContext(header, b.eth.BlockChain(), nil)
	}
	return vm.NewEVM(context, txContext, state, b.ChainConfig(), *vmConfig)
}

func (b *GattacaAPIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *GattacaAPIBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.miner.SubscribePendingLogs(ch)
}

func (b *GattacaAPIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainEvent(ch)
}

func (b *GattacaAPIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *GattacaAPIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *GattacaAPIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.BlockChain().SubscribeLogsEvent(ch)
}

func (b *GattacaAPIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.eth.txPool.Add([]*types.Transaction{signedTx}, true, false)[0]
}

func (b *GattacaAPIBackend) GetPoolTransactions() (types.Transactions, error) {
	pending := b.eth.txPool.Pending(txpool.PendingFilter{})
	var txs types.Transactions
	for _, batch := range pending {
		for _, lazy := range batch {
			if tx := lazy.Resolve(); tx != nil {
				txs = append(txs, tx)
			}
		}
	}
	return txs, nil
}

func (b *GattacaAPIBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.eth.txPool.Get(hash)
}

// GetTransaction retrieves the lookup along with the transaction itself associate
// with the given transaction hash.
//
// An error will be returned if the transaction is not found, and background
// indexing for transactions is still in progress. The error is used to indicate the
// scenario explicitly that the transaction might be reachable shortly.
//
// A null will be returned in the transaction is not found and background transaction
// indexing is already finished. The transaction is not existent from the perspective
// of node.
func (b *GattacaAPIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64, error) {
	lookup, tx, err := b.eth.blockchain.GetTransactionLookup(txHash)
	if err != nil {
		return false, nil, common.Hash{}, 0, 0, err
	}
	if lookup == nil || tx == nil {
		return false, nil, common.Hash{}, 0, 0, nil
	}
	return true, tx, lookup.BlockHash, lookup.BlockIndex, lookup.Index, nil
}

func (b *GattacaAPIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.eth.txPool.Nonce(addr), nil
}

func (b *GattacaAPIBackend) Stats() (runnable int, blocked int) {
	return b.eth.txPool.Stats()
}

func (b *GattacaAPIBackend) TxPoolContent() (map[common.Address][]*types.Transaction, map[common.Address][]*types.Transaction) {
	return b.eth.txPool.Content()
}

func (b *GattacaAPIBackend) TxPoolContentFrom(addr common.Address) ([]*types.Transaction, []*types.Transaction) {
	return b.eth.txPool.ContentFrom(addr)
}

func (b *GattacaAPIBackend) TxPool() *txpool.TxPool {
	return b.eth.txPool
}

func (b *GattacaAPIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.eth.txPool.SubscribeTransactions(ch, true)
}

func (b *GattacaAPIBackend) SyncProgress() ethereum.SyncProgress {
	prog := b.eth.Downloader().Progress()
	if txProg, err := b.eth.blockchain.TxIndexProgress(); err == nil {
		prog.TxIndexFinishedBlocks = txProg.Indexed
		prog.TxIndexRemainingBlocks = txProg.Remaining
	}
	return prog
}

func (b *GattacaAPIBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestTipCap(ctx)
}

func (b *GattacaAPIBackend) FeeHistory(ctx context.Context, blockCount uint64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (firstBlock *big.Int, reward [][]*big.Int, baseFee []*big.Int, gasUsedRatio []float64, err error) {
	return b.gpo.FeeHistory(ctx, blockCount, lastBlock, rewardPercentiles)
}

func (b *GattacaAPIBackend) ChainDb() ethdb.Database {
	return b.eth.ChainDb()
}

func (b *GattacaAPIBackend) EventMux() *event.TypeMux {
	return b.eth.EventMux()
}

func (b *GattacaAPIBackend) AccountManager() *accounts.Manager {
	return b.eth.AccountManager()
}

func (b *GattacaAPIBackend) ExtRPCEnabled() bool {
	return b.extRPCEnabled
}

func (b *GattacaAPIBackend) UnprotectedAllowed() bool {
	return b.allowUnprotectedTxs
}

func (b *GattacaAPIBackend) RPCGasCap() uint64 {
	return b.eth.config.RPCGasCap
}

func (b *GattacaAPIBackend) RPCEVMTimeout() time.Duration {
	return b.eth.config.RPCEVMTimeout
}

func (b *GattacaAPIBackend) RPCTxFeeCap() float64 {
	return b.eth.config.RPCTxFeeCap
}

func (b *GattacaAPIBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.eth.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *GattacaAPIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.eth.bloomRequests)
	}
}

func (b *GattacaAPIBackend) Engine() consensus.Engine {
	return b.eth.engine
}

func (b *GattacaAPIBackend) CurrentHeader() *types.Header {
	return b.eth.blockchain.CurrentHeader()
}

func (b *GattacaAPIBackend) Miner() *miner.Miner {
	return b.eth.Miner()
}

func (b *GattacaAPIBackend) StartMining() error {
	return b.eth.StartMining()
}

func (b *GattacaAPIBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	return b.eth.stateAtBlock(ctx, block, reexec, base, readOnly, preferDisk)
}

func (b *GattacaAPIBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	return b.eth.stateAtTransaction(ctx, block, txIndex, reexec)
}
