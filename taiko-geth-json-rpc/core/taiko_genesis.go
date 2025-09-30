package core

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	taikoGenesis "github.com/ethereum/go-ethereum/core/taiko_genesis"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var (
	InternalDevnetOntakeBlock = common.Big0
	PreconfDevnetOntakeBlock  = common.Big0
	MasayaDevnetOntakeBlock   = common.Big0
	TolbaOntakeBlock          = common.Big0
	MainnetOntakeBlock        = new(big.Int).SetUint64(538_304)

	InternalDevnetPacayaBlock = common.Big0
	PreconfDevnetPacayaBlock  = common.Big0
	MasayaDevnetPacayaBlock   = common.Big0
	TolbaPacayaBlock          = common.Big0
	MainnetPacayaBlock        = new(big.Int).SetUint64(1_166_000)

	InternalDevnetShastaBlock = new(big.Int).SetUint64(10)
	PreconfDevnetShastaBlock  = common.Big0
	MasayaDevnetShastaBlock   = common.Big0
	MainnetShastaBlock        = new(big.Int).SetUint64(999_999_999_999)
)

// TaikoGenesisBlock returns the Taiko network genesis block configs.
func TaikoGenesisBlock(networkID uint64) *Genesis {
	chainConfig := params.TaikoChainConfig

	var allocJSON []byte
	switch networkID {
	case params.TaikoMainnetNetworkID.Uint64():
		chainConfig.ChainID = params.TaikoMainnetNetworkID
		chainConfig.OntakeBlock = MainnetOntakeBlock
		chainConfig.PacayaBlock = MainnetPacayaBlock
		chainConfig.ShastaBlock = MainnetShastaBlock
		allocJSON = taikoGenesis.MainnetGenesisAllocJSON
	case params.TaikoInternalNetworkID.Uint64():
		chainConfig.ChainID = params.TaikoInternalNetworkID
		chainConfig.OntakeBlock = InternalDevnetOntakeBlock
		chainConfig.PacayaBlock = InternalDevnetPacayaBlock
		chainConfig.ShastaBlock = InternalDevnetShastaBlock
		allocJSON = taikoGenesis.InternalGenesisAllocJSON
	case params.PreconfDevnetNetworkID.Uint64():
		chainConfig.ChainID = params.PreconfDevnetNetworkID
		chainConfig.OntakeBlock = PreconfDevnetOntakeBlock
		chainConfig.PacayaBlock = PreconfDevnetPacayaBlock
		chainConfig.ShastaBlock = PreconfDevnetShastaBlock
		allocJSON = taikoGenesis.PreconfDevnetGenesisAllocJSON
	case params.MasayaDevnetNetworkID.Uint64():
		chainConfig.ChainID = params.MasayaDevnetNetworkID
		chainConfig.OntakeBlock = MasayaDevnetOntakeBlock
		chainConfig.PacayaBlock = MasayaDevnetPacayaBlock
		chainConfig.ShastaBlock = MasayaDevnetShastaBlock
		allocJSON = taikoGenesis.MasayaGenesisAllocJSON
	case params.TolbaNetworkID.Uint64():
		chainConfig.ChainID = params.TolbaNetworkID
		chainConfig.OntakeBlock = TolbaOntakeBlock
		chainConfig.PacayaBlock = TolbaPacayaBlock
		allocJSON = taikoGenesis.TolbaGenesisAllocJSON
	default:
		chainConfig.ChainID = params.TaikoInternalNetworkID
		chainConfig.OntakeBlock = InternalDevnetOntakeBlock
		chainConfig.PacayaBlock = InternalDevnetPacayaBlock
		chainConfig.ShastaBlock = InternalDevnetShastaBlock
		allocJSON = taikoGenesis.InternalGenesisAllocJSON
	}

	var alloc GenesisAlloc
	if err := alloc.UnmarshalJSON(allocJSON); err != nil {
		log.Crit("unmarshal alloc json error", "error", err)
	}

	return &Genesis{
		Config:     chainConfig,
		ExtraData:  []byte{},
		GasLimit:   uint64(15_000_000),
		Difficulty: common.Big0,
		Alloc:      alloc,
		GasUsed:    0,
		BaseFee:    new(big.Int).SetUint64(10_000_000),
	}
}
