package utils

import (
	"fmt"
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
	TaikoDevnetUnzenTimeFlag = cli.Uint64Flag{
		Name:    "taiko.devnet-unzen-time",
		Usage:   "Override Unzen fork time for Taiko internal devnet (timestamp)",
		Value:   0,
		EnvVars: []string{"TAIKO_DEVNET_UNZEN_TIME"},
	}
	TaikoDevnetEtnaTimeFlag = cli.Uint64Flag{
		Name:        "taiko.devnet-etna-time",
		Usage:       "Override Etna fork time for Taiko internal devnet (timestamp)",
		DefaultText: "Unzen fork time",
		EnvVars:     []string{"TAIKO_DEVNET_ETNA_TIME"},
	}
)

// taikoDevnetForkTimes resolves the devnet fork time overrides. Etna follows
// Unzen unless it is set explicitly, and must never activate before it.
func taikoDevnetForkTimes(ctx *cli.Context) (unzenTime, etnaTime uint64, err error) {
	unzenTime = ctx.Uint64(TaikoDevnetUnzenTimeFlag.Name)
	etnaTime = unzenTime
	if ctx.IsSet(TaikoDevnetEtnaTimeFlag.Name) {
		etnaTime = ctx.Uint64(TaikoDevnetEtnaTimeFlag.Name)
	}
	if etnaTime < unzenTime {
		return 0, 0, fmt.Errorf("--%s (%d) must not be earlier than --%s (%d)",
			TaikoDevnetEtnaTimeFlag.Name, etnaTime, TaikoDevnetUnzenTimeFlag.Name, unzenTime)
	}
	return unzenTime, etnaTime, nil
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
