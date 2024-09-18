package miner

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func (g *GattacaWorker) GetPendingPoolNonce(address common.Address) uint64 {
	log.Info("getting pending pool nonce")
	return g.preconfHead.state.GetNonce(address)
}
