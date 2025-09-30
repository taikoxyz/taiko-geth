package taiko_genesis

import (
	_ "embed"
)

//go:embed internal_l2a.json
var InternalL2AGenesisAllocJSON []byte

//go:embed mainnet.json
var MainnetGenesisAllocJSON []byte

//go:embed preconf_devnet.json
var PreconfDevnetGenesisAllocJSON []byte

//go:embed masaya.json
var MasayaGenesisAllocJSON []byte

//go:embed tolba.json
var TolbaGenesisAllocJSON []byte
