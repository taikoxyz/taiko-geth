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

package ethapi

import (
	"context"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/rpc"
	"strconv"
)

// VanillaTransactionAPI exposes methods for reading and creating transaction data.
type VanillaTransactionAPI struct {
	b         Backend
	nonceLock *AddrLocker
	signer    types.Signer
}

type VanillaBlockChainAPI struct {
	b Backend
}

func NewVanillaBlockChainAPI(b Backend) *VanillaBlockChainAPI {
	return &VanillaBlockChainAPI{b: b}
}

// NewVanillaTransactionAPI creates a new RPC service with methods for interacting with transactions.
func NewVanillaTransactionAPI(b Backend, nonceLock *AddrLocker) *TransactionAPI {
	// The signer used by the API should always be the 'latest' known one because we expect
	// signers to be backwards-compatible with old transactions.
	signer := types.LatestSigner(b.ChainConfig())
	return &TransactionAPI{b, nonceLock, signer}
}

// GetBlockTransactionCountByNumber returns the number of transactions in the block with the given block number.
func (s *VanillaTransactionAPI) GetBlockTransactionCountByNumber(ctx context.Context, blockNr rpc.BlockNumber) *hexutil.Uint {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		n := hexutil.Uint(len(block.Transactions()))
		return &n
	}
	return nil
}

// GetBlockTransactionCountByHash returns the number of transactions in the block with the given hash.
func (s *VanillaTransactionAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) *hexutil.Uint {
	if block, _ := s.b.BlockByHash(ctx, blockHash); block != nil {
		n := hexutil.Uint(len(block.Transactions()))
		return &n
	}
	return nil
}

// GetTransactionByBlockNumberAndIndex returns the transaction for the given block number and index.
func (s *VanillaTransactionAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) *RPCTransaction {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		return newRPCTransactionFromBlockIndex(block, uint64(index), s.b.ChainConfig())
	}
	return nil
}

// GetTransactionByBlockHashAndIndex returns the transaction for the given block hash and index.
func (s *VanillaTransactionAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) *RPCTransaction {
	if block, _ := s.b.BlockByHash(ctx, blockHash); block != nil {
		return newRPCTransactionFromBlockIndex(block, uint64(index), s.b.ChainConfig())
	}
	return nil
}

// GetRawTransactionByBlockNumberAndIndex returns the bytes of the transaction for the given block number and index.
func (s *VanillaTransactionAPI) GetRawTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) hexutil.Bytes {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		return newRPCRawTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

// GetRawTransactionByBlockHashAndIndex returns the bytes of the transaction for the given block hash and index.
func (s *VanillaTransactionAPI) GetRawTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) hexutil.Bytes {
	if block, _ := s.b.BlockByHash(ctx, blockHash); block != nil {
		return newRPCRawTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

// GetTransactionCount returns the number of transactions the given address has sent for the given block number
func (s *VanillaTransactionAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	// change(taiko): check to see if it exists from the preconfer.
	// Check if PreconfirmationForwardingURL is set
	log.Info("Calling VanillaTransactionAPI.GetTransactionCount")
	if forwardURL := s.b.GetPreconfirmationForwardingURL(); forwardURL != "" {
		log.Info("forwarding getTransactionCount", "addr", address.Hex())

		if blockNr, ok := blockNrOrHash.Number(); ok {
			txCount, err := forward[hexutil.Uint64](forwardURL, "eth_getTransactionCount", []interface{}{address.Hex(), blockNr.String()})
			if err == nil && txCount != nil {
				return txCount, nil
			}
		}

		if blockHash, ok := blockNrOrHash.Hash(); ok {
			txCount, err := forward[hexutil.Uint64](forwardURL, "eth_getTransactionCount", []interface{}{address.Hex(), blockHash.Hex()})
			if err == nil && txCount != nil {
				return txCount, nil
			}
		}
	}

	// Ask transaction pool for the nonce which includes pending transactions
	if blockNr, ok := blockNrOrHash.Number(); ok && blockNr == rpc.PendingBlockNumber {
		nonce, err := s.b.GetPoolNonce(ctx, address)
		if err != nil {
			return nil, err
		}
		return (*hexutil.Uint64)(&nonce), nil
	}
	// Resolve block number and use its state to ask for the nonce
	state, _, err := s.b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}
	nonce := state.GetNonce(address)
	return (*hexutil.Uint64)(&nonce), state.Error()
}

// BlockNumber returns the block number of the chain head.
func (v *VanillaBlockChainAPI) BlockNumber() hexutil.Uint64 {
	// change(taiko): check to see if it exists from the preconfer.
	// Check if PreconfirmationForwardingURL is set
	if forwardURL := v.b.GetPreconfirmationForwardingURL(); forwardURL != "" {
		log.Info("forwarding blockNumber request")

		// Forward the raw transaction to the specified URL
		res, err := forward[string](forwardURL, "eth_blockNumber", nil)

		if err == nil && res != nil {
			log.Info("forwarded block number request", "res", res)
			i, _ := strconv.ParseUint(*res, 0, 64)

			log.Info("parsed block number", "bn", hexutil.Uint64(i))
			return hexutil.Uint64(i)
		}
	}

	header, _ := v.b.HeaderByNumber(context.Background(), rpc.LatestBlockNumber) // latest header should always be available
	return hexutil.Uint64(header.Number.Uint64())
}

func (api *VanillaBlockChainAPI) ChainId() *hexutil.Big {
	return (*hexutil.Big)(api.b.ChainConfig().ChainID)
}

// GetBlockByNumber returns the requested canonical block.
//   - When blockNr is -1 the chain pending block is returned.
//   - When blockNr is -2 the chain latest block is returned.
//   - When blockNr is -3 the chain finalized block is returned.
//   - When blockNr is -4 the chain safe block is returned.
//   - When fullTx is true all transactions in the block are returned, otherwise
//     only the transaction hash is returned.
func (s *VanillaBlockChainAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (map[string]interface{}, error) {
	worker := miner.GetWorker(5)
	var block *types.Block
	var err error
	if worker != nil {
		block, err = worker.GetBlockByNumber(ctx, number)
		if block == nil {
			block, err = s.b.BlockByNumber(ctx, number)
		}
	} else {
		block, err = s.b.BlockByNumber(ctx, number)
	}
	if block != nil && err == nil {
		response, err := s.rpcMarshalBlock(ctx, block, true, fullTx)
		if err == nil && number == rpc.PendingBlockNumber {
			// Pending blocks need to nil out a few fields
			for _, field := range []string{"hash", "nonce", "miner"} {
				response[field] = nil
			}
		}
		return response, err
	}
	return nil, err
}

// rpcMarshalBlock uses the generalized output filler, then adds the total difficulty field, which requires
// a `BlockchainAPI`.
func (s *VanillaBlockChainAPI) rpcMarshalBlock(ctx context.Context, b *types.Block, inclTx bool, fullTx bool) (map[string]interface{}, error) {
	fields := RPCMarshalBlock(b, inclTx, fullTx, s.b.ChainConfig())
	if inclTx {
		fields["totalDifficulty"] = (*hexutil.Big)(s.b.GetTd(ctx, b.Hash()))
	}
	return fields, nil
}

// GetBlockByHash returns the requested block. When fullTx is true all transactions in the block are returned in full
// detail, otherwise only the transaction hash is returned.
func (s *VanillaBlockChainAPI) GetBlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (map[string]interface{}, error) {
	var block *types.Block
	var err error
	worker := miner.GetWorker(5)
	if worker != nil {
		block = worker.GetBlockByHash(ctx, hash, fullTx)
		if block == nil {
			block, err = s.b.BlockByHash(ctx, hash)
		}
	} else {
		block, err = s.b.BlockByHash(ctx, hash)
	}
	if block != nil {
		return s.rpcMarshalBlock(ctx, block, true, fullTx)
	}
	return nil, err
}

func (s *VanillaBlockChainAPI) Call(ctx context.Context, args TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *StateOverride, blockOverrides *BlockOverrides) (hexutil.Bytes, error) {
	if blockNrOrHash == nil {
		latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		blockNrOrHash = &latest
	}
	result, err := DoCall(ctx, s.b, args, *blockNrOrHash, overrides, blockOverrides, s.b.RPCEVMTimeout(), s.b.RPCGasCap())
	if err != nil {
		return nil, err
	}
	// If the result contains a revert reason, try to unpack and return it.
	if len(result.Revert()) > 0 {
		return nil, newRevertError(result.Revert())
	}
	return result.Return(), result.Err
}

// GetTransactionReceipt returns the transaction receipt for the given transaction hash.
func (s *VanillaTransactionAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	found, tx, blockHash, blockNumber, index, err := s.b.GetTransaction(ctx, hash)
	if err != nil || !found {
		// change(taiko): check to see if it exists from the preconfer.
		// Check if PreconfirmationForwardingURL is set
		if forwardURL := s.b.GetPreconfirmationForwardingURL(); forwardURL != "" {
			log.Info("forwarding get transaction receipt", "url", forwardURL)
			// Forward the raw transaction to the specified URL
			m, err := forward[map[string]interface{}](forwardURL, "eth_getTransactionReceipt", []interface{}{hash.Hex()})
			if err == nil && m != nil {
				return *m, err
			}
		}

		if err != nil {
			return nil, NewTxIndexingError() // transaction is not fully indexed
		}
	}

	if !found {
		return nil, nil // transaction is not existent or reachable
	}
	header, err := s.b.HeaderByHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	receipts, err := s.b.GetReceipts(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	if uint64(len(receipts)) <= index {
		return nil, nil
	}
	receipt := receipts[index]

	// Derive the sender.
	signer := types.MakeSigner(s.b.ChainConfig(), header.Number, header.Time)
	return marshalReceipt(receipt, blockHash, blockNumber, signer, tx, int(index)), nil
}
