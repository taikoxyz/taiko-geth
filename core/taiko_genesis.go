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
	HeklaOntakeBlock          = new(big.Int).SetUint64(840_512)
	TolbaOntakeBlock          = common.Big0
	MainnetOntakeBlock        = new(big.Int).SetUint64(538_304)

	InternalDevnetPacayaBlock = common.Big0
	PreconfDevnetPacayaBlock  = common.Big0
	MasayaDevnetPacayaBlock   = common.Big0
	HeklaPacayaBlock          = new(big.Int).SetUint64(1_299_888)
	TolbaPacayaBlock          = common.Big0
	MainnetPacayaBlock        = new(big.Int).SetUint64(1_166_000)

	InternalDevnetShastaBlock = new(big.Int).SetUint64(10)
	PreconfDevnetShastaBlock  = common.Big0
	MasayaDevnetShastaBlock   = common.Big0
	HeklaShastaBlock          = new(big.Int).SetUint64(999_999_999_999)
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
	case params.TaikoInternalL2ANetworkID.Uint64():
		chainConfig.ChainID = params.TaikoInternalL2ANetworkID
		chainConfig.OntakeBlock = InternalDevnetOntakeBlock
		chainConfig.PacayaBlock = InternalDevnetPacayaBlock
		chainConfig.ShastaBlock = InternalDevnetShastaBlock
		allocJSON = taikoGenesis.InternalL2AGenesisAllocJSON
	case params.TaikoInternalL2BNetworkID.Uint64():
		chainConfig.ChainID = params.TaikoInternalL2BNetworkID
		allocJSON = taikoGenesis.InternalL2BGenesisAllocJSON
	case params.SnaefellsjokullNetworkID.Uint64():
		chainConfig.ChainID = params.SnaefellsjokullNetworkID
		allocJSON = taikoGenesis.SnaefellsjokullGenesisAllocJSON
	case params.AskjaNetworkID.Uint64():
		chainConfig.ChainID = params.AskjaNetworkID
		allocJSON = taikoGenesis.AskjaGenesisAllocJSON
	case params.GrimsvotnNetworkID.Uint64():
		chainConfig.ChainID = params.GrimsvotnNetworkID
		allocJSON = taikoGenesis.GrimsvotnGenesisAllocJSON
	case params.EldfellNetworkID.Uint64():
		chainConfig.ChainID = params.EldfellNetworkID
		allocJSON = taikoGenesis.EldfellGenesisAllocJSON
	case params.JolnirNetworkID.Uint64():
		chainConfig.ChainID = params.JolnirNetworkID
		allocJSON = taikoGenesis.JolnirGenesisAllocJSON
	case params.KatlaNetworkID.Uint64():
		chainConfig.ChainID = params.KatlaNetworkID
		allocJSON = taikoGenesis.KatlaGenesisAllocJSON
	case params.HeklaNetworkID.Uint64():
		chainConfig.ChainID = params.HeklaNetworkID
		chainConfig.OntakeBlock = HeklaOntakeBlock
		chainConfig.PacayaBlock = HeklaPacayaBlock
		chainConfig.ShastaBlock = HeklaShastaBlock
		allocJSON = taikoGenesis.HeklaGenesisAllocJSON
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
		chainConfig.ChainID = params.TaikoInternalL2ANetworkID
		chainConfig.OntakeBlock = InternalDevnetOntakeBlock
		chainConfig.PacayaBlock = InternalDevnetPacayaBlock
		chainConfig.ShastaBlock = InternalDevnetShastaBlock
		allocJSON = taikoGenesis.InternalL2AGenesisAllocJSON
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
