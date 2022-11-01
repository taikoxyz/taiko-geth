package core

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	taikoGenesis "github.com/ethereum/go-ethereum/core/taiko_genesis"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// Network IDs
const (
	MainnetNetworkID = 167
	Alpha1NetworkID  = 167001
	Alpha2NetworkID  = 167002
)

// TaikoGenesisBlock returns the Taiko network genesis block configs.
func TaikoGenesisBlock(networkID uint64) *Genesis {
	chainConfig := params.TaikoChainConfig

	var allocJSON []byte
	switch networkID {
	case Alpha1NetworkID:
		chainConfig.ChainID = big.NewInt(Alpha1NetworkID)
		allocJSON = taikoGenesis.Alpha1GenesisAllocJSON
	case Alpha2NetworkID:
		chainConfig.ChainID = big.NewInt(Alpha2NetworkID)
		allocJSON = taikoGenesis.Alpha2GenesisAllocJSON
	default:
		chainConfig.ChainID = big.NewInt(MainnetNetworkID)
		allocJSON = taikoGenesis.MainnetGenesisAllocJSON
	}

	var alloc GenesisAlloc
	if err := alloc.UnmarshalJSON(allocJSON); err != nil {
		log.Crit("unmarshal alloc json error", "error", err)
	}

	return &Genesis{
		Config:     chainConfig,
		ExtraData:  []byte{},
		GasLimit:   uint64(5000000),
		Difficulty: common.Big0,
		Alloc:      alloc,
	}
}
