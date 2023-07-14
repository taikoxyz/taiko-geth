package params

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

func u64(val uint64) *uint64 { return &val }

// Network IDs
var (
	TaikoMainnetNetworkID   = big.NewInt(167)
	TaikoInternal1NetworkID = big.NewInt(167001)
	TaikoInternal2NetworkID = big.NewInt(167002)
	SnæfellsjökullNetworkID = big.NewInt(167003)
	AskjaNetworkID          = big.NewInt(167004)
	GrimsvotnNetworkID      = big.NewInt(167005)
	EldfellNetworkID        = big.NewInt(167006)
)

var TaikoChainConfig = &ChainConfig{
	ChainID:                       TaikoMainnetNetworkID, // Use mainnet network ID by default.
	HomesteadBlock:                common.Big0,
	EIP150Block:                   common.Big0,
	EIP155Block:                   common.Big0,
	EIP158Block:                   common.Big0,
	ByzantiumBlock:                common.Big0,
	ConstantinopleBlock:           common.Big0,
	PetersburgBlock:               common.Big0,
	IstanbulBlock:                 common.Big0,
	BerlinBlock:                   common.Big0,
	LondonBlock:                   common.Big0,
	ShanghaiTime:                  u64(0),
	MergeNetsplitBlock:            nil,
	TerminalTotalDifficulty:       common.Big0,
	TerminalTotalDifficultyPassed: true,
	Taiko:                         true,
	Treasury:                      common.HexToAddress("0xdf09A0afD09a63fb04ab3573922437e1e637dE8b"),
}
