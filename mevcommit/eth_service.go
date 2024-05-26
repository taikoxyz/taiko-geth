package mevcommit

import (
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/params"
)

type IEthereumService interface {
	Config() *params.ChainConfig
	Synced() bool
}

type EthereumService struct {
	eth *eth.Ethereum
}

func NewEthereumService(eth *eth.Ethereum) *EthereumService {
	return &EthereumService{eth: eth}
}

func (s *EthereumService) Config() *params.ChainConfig {
	return s.eth.BlockChain().Config()
}

func (s *EthereumService) Synced() bool {
	return s.eth.Synced()
}
