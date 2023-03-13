package core

import (
	"github.com/ethereum/go-ethereum/common"
	taikoGenesis "github.com/ethereum/go-ethereum/core/taiko_genesis"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// TaikoGenesisBlock returns the Taiko network genesis block configs.
func TaikoGenesisBlock(networkID uint64) *Genesis {
	chainConfig := params.TaikoChainConfig

	var allocJSON []byte
	switch networkID {
	case params.TaikoInternal1NetworkID.Uint64():
		chainConfig.ChainID = params.TaikoInternal1NetworkID
		allocJSON = taikoGenesis.Internal1GenesisAllocJSON
	case params.TaikoInternal2NetworkID.Uint64():
		chainConfig.ChainID = params.TaikoInternal2NetworkID
		allocJSON = taikoGenesis.Internal2GenesisAllocJSON
	case params.SnæfellsjökullNetworkID.Uint64():
		chainConfig.ChainID = params.SnæfellsjökullNetworkID
		allocJSON = taikoGenesis.SnæfellsjökullGenesisAllocJSON
	case params.AskjaNetworkID.Uint64():
		chainConfig.ChainID = params.AskjaNetworkID
		allocJSON = taikoGenesis.AskjaGenesisAllocJSON
	default:
		chainConfig.ChainID = params.TaikoMainnetNetworkID
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
