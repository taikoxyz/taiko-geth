package taiko_genesis

import (
	_ "embed"
)

//go:embed internal.json
var InternalGenesisAllocJSON []byte

//go:embed mainnet.json
var MainnetGenesisAllocJSON []byte

//go:embed taiko_hoodi.json
var TaikoHoodiGenesisAllocJSON []byte
