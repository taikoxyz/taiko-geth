package taiko_genesis

import (
	_ "embed"
)

//go:embed internal.json
var InternalGenesisAllocJSON []byte

//go:embed snaefellsjokull.json
var SnaefellsjokullGenesisAllocJSON []byte

//go:embed askja.json
var AskjaGenesisAllocJSON []byte

//go:embed grimsvotn.json
var GrimsvotnGenesisAllocJSON []byte

//go:embed eldfell.json
var EldfellGenesisAllocJSON []byte
