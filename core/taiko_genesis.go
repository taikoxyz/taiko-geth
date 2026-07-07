package core

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	taikoGenesis "github.com/ethereum/go-ethereum/core/taiko_genesis"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var (
	InternalDevnetOntakeBlock = common.Big0
	MasayaDevnetOntakeBlock   = common.Big0
	TaikoHoodiOntakeBlock     = common.Big0
	MainnetOntakeBlock        = new(big.Int).SetUint64(538_304)

	InternalDevnetPacayaBlock = common.Big0
	MasayaDevnetPacayaBlock   = common.Big0
	TaikoHoodiPacayaBlock     = common.Big0
	MainnetPacayaBlock        = new(big.Int).SetUint64(1_166_000)

	InternalShastaTime uint64 = 0
	MasayaShastaTime   uint64 = 0
	MainnetShastaTime  uint64 = 1_775_135_700
	HoodiShastaTime    uint64 = 1_770_296_400

	DevnetUnzenTime  uint64 = 0
	MasayaUnzenTime  uint64 = 0
	MainnetUnzenTime uint64 = 1_786_021_200 // 2026-08-06 13:00:00 UTC
	HoodiUnzenTime   uint64 = 1_781_787_600
)

// TaikoGenesisBlock returns the Taiko network genesis block configs.
func TaikoGenesisBlock(networkID uint64) *Genesis {
	// CHANGE(taiko): Each generated genesis needs an isolated chain config because
	// fork timestamps are overwritten per network below.
	chainConfig := *params.TaikoChainConfig

	var allocJSON []byte
	switch networkID {
	case params.TaikoMainnetNetworkID.Uint64():
		chainConfig.ChainID = params.TaikoMainnetNetworkID
		chainConfig.OntakeBlock = MainnetOntakeBlock
		chainConfig.PacayaBlock = MainnetPacayaBlock
		chainConfig.ShastaTime = &MainnetShastaTime
		chainConfig.UnzenTime = &MainnetUnzenTime
		if MainnetUnzenTime != math.MaxUint64 {
			chainConfig.CancunTime = &MainnetUnzenTime
			chainConfig.PragueTime = &MainnetUnzenTime
			chainConfig.OsakaTime = &MainnetUnzenTime
		}
		allocJSON = taikoGenesis.MainnetGenesisAllocJSON
	case params.TaikoInternalNetworkID.Uint64():
		chainConfig.ChainID = params.TaikoInternalNetworkID
		chainConfig.OntakeBlock = InternalDevnetOntakeBlock
		chainConfig.PacayaBlock = InternalDevnetPacayaBlock
		chainConfig.ShastaTime = &InternalShastaTime
		chainConfig.UnzenTime = &DevnetUnzenTime
		chainConfig.CancunTime = &DevnetUnzenTime
		chainConfig.PragueTime = &DevnetUnzenTime
		chainConfig.OsakaTime = &DevnetUnzenTime
		allocJSON = taikoGenesis.InternalGenesisAllocJSON
	case params.MasayaDevnetNetworkID.Uint64():
		chainConfig.ChainID = params.MasayaDevnetNetworkID
		chainConfig.OntakeBlock = MasayaDevnetOntakeBlock
		chainConfig.PacayaBlock = MasayaDevnetPacayaBlock
		chainConfig.ShastaTime = &MasayaShastaTime
		chainConfig.UnzenTime = &MasayaUnzenTime
		if MasayaUnzenTime != math.MaxUint64 {
			chainConfig.CancunTime = &MasayaUnzenTime
			chainConfig.PragueTime = &MasayaUnzenTime
			chainConfig.OsakaTime = &MasayaUnzenTime
		}
		allocJSON = taikoGenesis.MasayaGenesisAllocJSON
	case params.TaikoHoodiNetworkID.Uint64():
		chainConfig.ChainID = params.TaikoHoodiNetworkID
		chainConfig.OntakeBlock = TaikoHoodiOntakeBlock
		chainConfig.PacayaBlock = TaikoHoodiPacayaBlock
		chainConfig.ShastaTime = &HoodiShastaTime
		chainConfig.UnzenTime = &HoodiUnzenTime
		if HoodiUnzenTime != math.MaxUint64 {
			chainConfig.CancunTime = &HoodiUnzenTime
			chainConfig.PragueTime = &HoodiUnzenTime
			chainConfig.OsakaTime = &HoodiUnzenTime
		}
		allocJSON = taikoGenesis.TaikoHoodiGenesisAllocJSON
	default:
		chainConfig.ChainID = params.TaikoInternalNetworkID
		chainConfig.OntakeBlock = InternalDevnetOntakeBlock
		chainConfig.PacayaBlock = InternalDevnetPacayaBlock
		chainConfig.ShastaTime = &InternalShastaTime
		chainConfig.UnzenTime = &DevnetUnzenTime
		chainConfig.CancunTime = &DevnetUnzenTime
		chainConfig.PragueTime = &DevnetUnzenTime
		chainConfig.OsakaTime = &DevnetUnzenTime
		allocJSON = taikoGenesis.InternalGenesisAllocJSON
	}
	var alloc GenesisAlloc
	if err := alloc.UnmarshalJSON(allocJSON); err != nil {
		log.Crit("unmarshal alloc json error", "error", err)
	}

	return &Genesis{
		Config:     &chainConfig,
		ExtraData:  []byte{},
		GasLimit:   uint64(15_000_000),
		Difficulty: common.Big0,
		Alloc:      alloc,
		GasUsed:    0,
		BaseFee:    new(big.Int).SetUint64(10_000_000),
	}
}
