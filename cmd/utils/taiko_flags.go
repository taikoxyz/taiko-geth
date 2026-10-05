package utils

import (
	"os"

	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfave/cli/v2"
)

var (
	TaikoFlag = cli.BoolFlag{
		Name:  "taiko",
		Usage: "Taiko network",
	}
	TaikoDevnetEtnaTimeFlag = cli.Uint64Flag{
		Name:        "taiko.devnet-etna-time",
		Usage:       "Etna fork time for the Taiko internal devnet (timestamp, 0 = genesis)",
		DefaultText: "never",
		EnvVars:     []string{"TAIKO_DEVNET_ETNA_TIME"},
	}
)

// taikoDevnetEtnaTime returns the internal devnet's Etna switch time. It is nil
// (never) unless --taiko.devnet-etna-time is set; zero activates Etna at
// genesis.
func taikoDevnetEtnaTime(ctx *cli.Context) *uint64 {
	if !ctx.IsSet(TaikoDevnetEtnaTimeFlag.Name) {
		return nil
	}
	etnaTime := ctx.Uint64(TaikoDevnetEtnaTimeFlag.Name)
	return &etnaTime
}

// RegisterTaikoAPIs initializes and registers the Taiko RPC APIs.
func RegisterTaikoAPIs(stack *node.Node, cfg *ethconfig.Config, backend *eth.Ethereum) {
	if os.Getenv("TAIKO_TEST") != "" {
		return
	}
	// Add methods under "taiko_" RPC namespace to the available APIs list
	stack.RegisterAPIs([]rpc.API{
		{
			Namespace: "taiko",
			Service:   eth.NewTaikoAPIBackend(backend),
			Public:    true,
		},
		{
			Namespace:     "taikoAuth",
			Service:       eth.NewTaikoAuthAPIBackend(backend),
			Authenticated: true,
		},
	})
}
