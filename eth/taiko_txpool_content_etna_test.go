package eth

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// etnaTestChainConfig returns a Taiko chain config with Shanghai through
// Osaka, Shasta, Unzen and Etna active from genesis.
func etnaTestChainConfig() *params.ChainConfig {
	zero := uint64(0)
	config := *params.TaikoChainConfig
	config.ChainID = big.NewInt(167000)
	config.ShanghaiTime = &zero
	config.CancunTime = &zero
	config.PragueTime = &zero
	config.OsakaTime = &zero
	config.ShastaTime = &zero
	config.UnzenTime = &zero
	config.EtnaTime = &zero
	return &config
}

// newEtnaTestEthereum returns an Ethereum whose chain runs the Taiko engine on
// gspec, with a miner and, when withPool is set, a transaction pool.
func newEtnaTestEthereum(t *testing.T, gspec *core.Genesis, withPool bool) *Ethereum {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	engine := taiko.New(gspec.Config, db)
	chain, err := core.NewBlockChain(db, gspec, engine, &core.BlockChainConfig{ArchiveMode: true})
	if err != nil {
		t.Fatalf("core.NewBlockChain: %v", err)
	}
	t.Cleanup(chain.Stop)
	eth := &Ethereum{blockchain: chain, chainDb: db, engine: engine}
	if withPool {
		poolConfig := legacypool.DefaultConfig
		poolConfig.Journal = ""
		pool, err := txpool.New(poolConfig.PriceLimit, chain, []txpool.SubPool{legacypool.New(poolConfig, chain)})
		if err != nil {
			t.Fatalf("txpool.New: %v", err)
		}
		// Cleanups run last-in first-out: the pool stops before the chain.
		t.Cleanup(func() { pool.Close() })
		eth.txPool = pool
	}
	eth.miner = miner.New(eth, miner.DefaultConfig, engine)
	return eth
}

// TestTxPoolContentEtnaParamErrors pins that the taikoAuth transaction-pool
// methods answer an Etna parent's invalid parameters with JSON-RPC -32602.
func TestTxPoolContentEtnaParamErrors(t *testing.T) {
	eth := newEtnaTestEthereum(t, &core.Genesis{Config: etnaTestChainConfig(), BaseFee: big.NewInt(params.ShastaInitialBaseFee)}, false)
	server := rpc.NewServer()
	t.Cleanup(server.Stop)
	if err := server.RegisterName("taikoAuth", &TaikoAuthAPIBackend{eth: eth}); err != nil {
		t.Fatalf("RegisterName: %v", err)
	}
	client := rpc.DialInProc(server)
	t.Cleanup(client.Close)

	beneficiary := common.HexToAddress("0x42")
	for _, tt := range []struct {
		name   string
		method string
		args   []any
	}{
		{"zero lists", "taikoAuth_txPoolContent", []any{beneficiary, big.NewInt(1), uint64(30_000_000), uint64(120_000), nil, uint64(0)}},
		{"zero lists with min tip", "taikoAuth_txPoolContentWithMinTip", []any{beneficiary, big.NewInt(1), uint64(30_000_000), uint64(120_000), nil, uint64(0), uint64(0)}},
		{"gas limit overflow", "taikoAuth_txPoolContent", []any{beneficiary, big.NewInt(1), uint64(math.MaxUint64), uint64(120_000), nil, uint64(2)}},
		{"gas limit overflow with min tip", "taikoAuth_txPoolContentWithMinTip", []any{beneficiary, big.NewInt(1), uint64(math.MaxUint64), uint64(120_000), nil, uint64(2), uint64(0)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var lists []*miner.PreBuiltTxList
			err := client.Call(&lists, tt.method, tt.args...)
			var rpcErr rpc.Error
			if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != -32602 {
				t.Fatalf("error = %v, want a -32602 JSON-RPC error", err)
			}
		})
	}
}
