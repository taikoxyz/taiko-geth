package ethapi

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestTaikoSimulateV1KeepsAssemblyChecks pins that eth_simulateV1 assembles
// its blocks through the Taiko engine's checks: a simulated Unzen block whose
// first transaction is not an anchor is rejected, as a sealed one would be.
func TestTaikoSimulateV1KeepsAssemblyChecks(t *testing.T) {
	zero := uint64(0)
	config := *params.TaikoChainConfig
	config.ChainID = big.NewInt(167000)
	config.OntakeBlock, config.PacayaBlock, config.ShastaTime = common.Big0, common.Big0, &zero
	config.CancunTime, config.PragueTime, config.OsakaTime, config.UnzenTime = &zero, &zero, &zero, &zero

	var (
		acc       = newTestAccount()
		recipient = common.Address{0xbb, 0xbb}
		gspec     = &core.Genesis{
			Config:     &config,
			Alloc:      types.GenesisAlloc{acc.addr: {Balance: big.NewInt(params.Ether)}},
			Difficulty: common.Big0,
			BaseFee:    big.NewInt(params.InitialBaseFee),
		}
	)
	backend := newTestBackend(t, 0, gspec, taiko.New(&config, rawdb.NewMemoryDatabase()), nil)
	api := NewBlockChainAPI(backend)

	_, err := api.SimulateV1(context.Background(), simOpts{
		BlockStateCalls: []simBlock{{
			Calls: []TransactionArgs{{
				From:  &acc.addr,
				To:    &recipient,
				Value: (*hexutil.Big)(big.NewInt(1)),
			}},
		}},
	}, nil)
	if !errors.Is(err, taiko.ErrAnchorTxNotFound) {
		t.Fatalf("SimulateV1 error = %v, want %v", err, taiko.ErrAnchorTxNotFound)
	}
}
