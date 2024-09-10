package utils

import (
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"os"

	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
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
)

// RegisterTaikoAPIs initializes and registers the Taiko RPC APIs.
func RegisterTaikoAPIs(stack *node.Node, cfg *ethconfig.Config, backend *eth.Ethereum) {
	if os.Getenv("TAIKO_TEST") != "" {
		return
	}
	// Add methods under "taiko_" RPC namespace to the available APIs list
	stack.RegisterAPIs([]rpc.API{
		{
			Namespace: "taiko",
			Version:   params.VersionWithMeta,
			Service:   eth.NewTaikoAPIBackend(backend),
			Public:    true,
		},
		{
			Namespace:     "taikoAuth",
			Version:       params.VersionWithMeta,
			Service:       eth.NewTaikoAuthAPIBackend(backend),
			Authenticated: true,
		},
	})
}

func RegisterVanillaTransactionApi(stack *node.Node, backend *eth.Ethereum, ethcfg *ethconfig.Config) error {
	filterSystem := filters.NewFilterSystem(backend.APIBackend, filters.Config{
		LogCacheSize: ethcfg.FilterLogCacheSize,
	})
	vanillaApi := []rpc.API{
		{
			Namespace: "eth",
			Service:   ethapi.NewVanillaTransactionAPI(backend.APIBackend, new(ethapi.AddrLocker)),
		},
		{
			Namespace: "eth",
			Service:   ethapi.NewVanillaBlockChainAPI(backend.APIBackend),
		},
		{
			Namespace: "net",
			Service:   ethapi.NewNetAPI(backend.P2PServer(), 167010),
		},
		{
			Namespace: "taiko",
			Version:   params.VersionWithMeta,
			Service:   eth.NewTaikoAPIBackend(backend),
			Public:    true,
		},
		{
			Namespace: "eth",
			Service:   filters.NewFilterAPI(filterSystem, false),
		},
	}
	stack.RegisterAPIs(vanillaApi)
	log.Info("vanilla api registered")
	return stack.Start()
}
