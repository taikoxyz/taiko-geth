package taiko_genesis

import (
	_ "embed"
)

//go:embed internal.json
var InternalGenesisAllocJSON []byte

//go:embed mainnet.json
var MainnetGenesisAllocJSON []byte

//go:embed preconf_devnet.json
var PreconfDevnetGenesisAllocJSON []byte

//go:embed masaya.json
var MasayaGenesisAllocJSON []byte

//go:embed tolba.json
var TolbaGenesisAllocJSON []byte
