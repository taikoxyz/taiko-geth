// Copyright 2026 The Taiko Authors
// This file is part of the go-ethereum library.
//
// Licensed under the GNU Lesser General Public License v3 or later.

package core

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// TestPostExecution_TaikoSkipsWithdrawals pins that PostExecution never credits
// withdrawals on a Taiko chain. Taiko's consensus engine Finalize credits them
// (as raw wei), and every block path runs PostExecution followed by Finalize, so
// a second credit here would pay each withdrawal twice and in different units.
func TestPostExecution_TaikoSkipsWithdrawals(t *testing.T) {
	const (
		blockNumber = 1
		blockTime   = 1
	)
	addr := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	w := types.Withdrawals{{Index: 0, Validator: 1, Address: addr, Amount: 7}}

	// Returns a single byte, so the upstream request-queue system calls have a
	// contract to run instead of failing on an empty system contract.
	queueCode := common.Hex2Bytes("60016000526001601ff3")

	for _, tt := range []struct {
		name   string
		config func(t *testing.T) *params.ChainConfig
		want   uint64
	}{
		{
			name:   "taiko unzen",
			config: unzenTestChainConfig,
			want:   0,
		},
		{
			name: "taiko mainnet pre-unzen",
			config: func(t *testing.T) *params.ChainConfig {
				return TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64()).Config
			},
			want: 0,
		},
		{
			name: "upstream credits in gwei",
			config: func(t *testing.T) *params.ChainConfig {
				cfg := *params.MergedTestChainConfig
				return &cfg
			},
			want: 7 * params.GWei,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config(t)
			if !config.IsShanghai(big.NewInt(blockNumber), blockTime) {
				t.Fatal("config does not have Shanghai active at the test block")
			}
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			statedb.SetCode(params.WithdrawalQueueAddress, queueCode, tracing.CodeChangeGenesis)
			statedb.SetCode(params.ConsolidationQueueAddress, queueCode, tracing.CodeChangeGenesis)
			statedb.Finalise(params.Rules{IsEIP158: true})

			evm := vm.NewEVM(taikoSystemCallTestBlockContext(), statedb, config, vm.Config{})
			if _, _, err := PostExecution(context.Background(), config, big.NewInt(blockNumber), blockTime, nil, w, evm, 0); err != nil {
				t.Fatalf("PostExecution: %v", err)
			}
			if got := statedb.GetBalance(addr).Uint64(); got != tt.want {
				t.Fatalf("balance after PostExecution = %d, want %d", got, tt.want)
			}
		})
	}
}
