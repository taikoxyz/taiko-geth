// Copyright 2026 The go-ethereum Authors
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

package catalyst

import (
	"errors"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/rpc"
)

// testingAPI implements the testing_ namespace.
// It's an engine-API adjacent namespace for testing purposes.
type testingAPI struct {
	eth *eth.Ethereum
}

func newTestingAPI(backend *eth.Ethereum) rpc.API {
	return rpc.API{
		Namespace:     "testing",
		Service:       &testingAPI{backend},
		Version:       "1.0",
		Authenticated: false,
	}
}

func decodeTransactions(raws []hexutil.Bytes) ([]*types.Transaction, error) {
	txs := make([]*types.Transaction, len(raws))
	for i, raw := range raws {
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(raw); err != nil {
			return nil, err
		}
		txs[i] = tx
	}
	return txs, nil
}

func (api *testingAPI) BuildBlockV1(parentHash common.Hash, payloadAttributes engine.PayloadAttributes, transactions *[]hexutil.Bytes, extraData *hexutil.Bytes) (*engine.ExecutionPayloadEnvelope, error) {
	if api.eth.BlockChain().CurrentBlock().Hash() != parentHash {
		return nil, errors.New("parentHash is not current head")
	}
	buildEmpty := transactions != nil && len(*transactions) == 0
	var txs []*types.Transaction
	if transactions != nil {
		var err error
		txs, err = decodeTransactions(*transactions)
		if err != nil {
			return nil, err
		}
	}
	extra := make([]byte, 0)
	if extraData != nil {
		extra = *extraData
	}
	args := &miner.BuildPayloadArgs{
		Parent:       parentHash,
		Timestamp:    payloadAttributes.Timestamp,
		FeeRecipient: payloadAttributes.SuggestedFeeRecipient,
		Random:       payloadAttributes.Random,
		Withdrawals:  payloadAttributes.Withdrawals,
		BeaconRoot:   payloadAttributes.BeaconRoot,
	}
	return api.eth.Miner().BuildTestingPayload(args, txs, buildEmpty, extra)
}
