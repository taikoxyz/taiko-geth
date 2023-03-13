package taiko_genesis

import (
	_ "embed"
)

//go:embed mainnet.json
var MainnetGenesisAllocJSON []byte

//go:embed internal-1.json
var Internal1GenesisAllocJSON []byte

//go:embed internal-2.json
var Internal2GenesisAllocJSON []byte

//go:embed snæfellsjökull.json
var SnæfellsjökullGenesisAllocJSON []byte

//go:embed askja.json
var AskjaGenesisAllocJSON []byte
