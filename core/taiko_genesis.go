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
	TaikoHoodiOntakeBlock     = common.Big0
	MainnetOntakeBlock        = new(big.Int).SetUint64(538_304)

	InternalDevnetPacayaBlock = common.Big0
	TaikoHoodiPacayaBlock     = common.Big0
	MainnetPacayaBlock        = new(big.Int).SetUint64(1_166_000)

	InternalShastaTime uint64 = 0
	MainnetShastaTime  uint64 = 1_775_135_700
	HoodiShastaTime    uint64 = 1_770_296_400

	DevnetUnzenTime  uint64 = 0
	MainnetUnzenTime uint64 = 1_786_021_200 // 2026-08-06 13:00:00 UTC
	HoodiUnzenTime   uint64 = 1_781_787_600

	DevnetEtnaTime  uint64 = 0
	MainnetEtnaTime uint64 = math.MaxUint64
	HoodiEtnaTime   uint64 = math.MaxUint64
)

// CHANGE(taiko): taikoNetworkConfig returns a per-call copy of the given Taiko
// network's chain config, together with the network's genesis alloc JSON, which
// it leaves undecoded. Unknown network IDs fall back to the internal devnet.
func taikoNetworkConfig(networkID uint64) (*params.ChainConfig, []byte) {
	// CHANGE(taiko): Each call needs an isolated chain config because
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
		chainConfig.EtnaTime = &MainnetEtnaTime
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
		chainConfig.EtnaTime = &DevnetEtnaTime
		chainConfig.CancunTime = &DevnetUnzenTime
		chainConfig.PragueTime = &DevnetUnzenTime
		chainConfig.OsakaTime = &DevnetUnzenTime
		allocJSON = taikoGenesis.InternalGenesisAllocJSON
	case params.TaikoHoodiNetworkID.Uint64():
		chainConfig.ChainID = params.TaikoHoodiNetworkID
		chainConfig.OntakeBlock = TaikoHoodiOntakeBlock
		chainConfig.PacayaBlock = TaikoHoodiPacayaBlock
		chainConfig.ShastaTime = &HoodiShastaTime
		chainConfig.UnzenTime = &HoodiUnzenTime
		chainConfig.EtnaTime = &HoodiEtnaTime
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
		chainConfig.EtnaTime = &DevnetEtnaTime
		chainConfig.CancunTime = &DevnetUnzenTime
		chainConfig.PragueTime = &DevnetUnzenTime
		chainConfig.OsakaTime = &DevnetUnzenTime
		allocJSON = taikoGenesis.InternalGenesisAllocJSON
	}
	return &chainConfig, allocJSON
}

// TaikoGenesisBlock returns the Taiko network genesis block configs.
func TaikoGenesisBlock(networkID uint64) *Genesis {
	chainConfig, allocJSON := taikoNetworkConfig(networkID)

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

// CHANGE(taiko): TaikoChainConfig returns the given Taiko network's chain
// config, equal to TaikoGenesisBlock(networkID).Config, but it does not decode
// the network's genesis alloc, so it is cheap enough for per-block fork checks
// such as IsUnzen and IsEtna. Unknown network IDs fall back to the internal
// devnet. Each call returns its own copy, but its fork-time fields point at this
// package's fork-time variables (such as DevnetEtnaTime) instead of holding
// copies, so overriding those variables later also applies to configs returned
// earlier.
func TaikoChainConfig(networkID uint64) *params.ChainConfig {
	chainConfig, _ := taikoNetworkConfig(networkID)
	return chainConfig
}
