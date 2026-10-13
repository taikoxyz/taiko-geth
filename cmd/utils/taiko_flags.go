package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfave/cli/v2"
)

var (
	TaikoFlag = cli.BoolFlag{
		Name:  "taiko",
		Usage: "Taiko network",
	}
	// TaikoDevnetEtnaTimeFlag is a string flag, parsed by taikoDevnetEtnaTime:
	// a numeric flag would accept hex, octal and underscores, and treat an
	// empty environment variable as unset.
	TaikoDevnetEtnaTimeFlag = cli.StringFlag{
		Name:        "taiko.devnet-etna-time",
		Usage:       "Etna fork time for the Taiko internal devnet (decimal Unix timestamp, 0 = genesis)",
		DefaultText: "never",
		EnvVars:     []string{"TAIKO_DEVNET_ETNA_TIME"},
	}
)

// taikoDevnetEtnaTime returns the internal devnet's Etna switch time from
// --taiko.devnet-etna-time or TAIKO_DEVNET_ETNA_TIME. It is nil (never) when
// neither is set; zero activates Etna at genesis. Like the reference client's
// decimal argument, a set value must be a decimal u64, with an optional
// leading '+' and leading zeros allowed: any other value, an explicitly empty
// one included, is an error. The value applies only to the internal devnet,
// which unknown network IDs fall back to: on mainnet and Hoodi it is ignored
// with a warning.
func taikoDevnetEtnaTime(ctx *cli.Context, networkID uint64) (*uint64, error) {
	if !ctx.IsSet(TaikoDevnetEtnaTimeFlag.Name) {
		return nil, nil
	}
	value := ctx.String(TaikoDevnetEtnaTimeFlag.Name)
	etnaTime, err := strconv.ParseUint(strings.TrimPrefix(value, "+"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid --%s value %q: want a decimal Unix timestamp", TaikoDevnetEtnaTimeFlag.Name, value)
	}
	if networkID == params.TaikoMainnetNetworkID.Uint64() || networkID == params.TaikoHoodiNetworkID.Uint64() {
		log.Warn("Ignoring --"+TaikoDevnetEtnaTimeFlag.Name+": it applies only to the Taiko internal devnet",
			"etnaTime", etnaTime, "networkid", networkID)
		return nil, nil
	}
	return &etnaTime, nil
}

// setTaikoDevnetEtnaTime sets core.DevnetEtnaTime for the Taiko network with
// the given ID, or stops startup on an invalid value.
func setTaikoDevnetEtnaTime(ctx *cli.Context, networkID uint64) {
	etnaTime, err := taikoDevnetEtnaTime(ctx, networkID)
	if err != nil {
		Fatalf("%v", err)
	}
	core.DevnetEtnaTime = etnaTime
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
