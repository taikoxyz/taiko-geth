package core

import (
	"github.com/ethereum/go-ethereum/common"
	taikoGenesis "github.com/ethereum/go-ethereum/core/taiko_genesis"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// CHANGE(taiko): TaikoGenesisBlock returns the Taiko network genesis block configs.
func TaikoGenesisBlock() *Genesis {
	var alloc GenesisAlloc
	if err := alloc.UnmarshalJSON(taikoGenesis.GenesisAllocJSON); err != nil {
		log.Crit("unmarshal alloc json error", "error", err)
	}

	return &Genesis{
		Config:     params.TaikoChainConfig,
		ExtraData:  []byte{},
		GasLimit:   uint64(8000000),
		Difficulty: common.Big0,
		Alloc:      alloc,
	}
}
