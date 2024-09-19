package miner

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func (g *GattacaWorker) GetPendingPoolNonce(address common.Address) uint64 {
	log.Info("getting pending pool nonce")
	env := g.preconfState.getLatestPreconfBlock()
	if env != nil {
		return env.state.GetNonce(address)
	}
	return 0
}
