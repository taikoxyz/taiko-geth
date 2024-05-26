package mevcommit

type Config struct {
	MevCommitProviderEnabled  bool   `toml:",omitempty"`
	MevCommitProviderEndpoint string `toml:",omitempty"`
}

var DefaultConfig = Config{
	MevCommitProviderEnabled:  false,
	MevCommitProviderEndpoint: "127.0.0.1:13524",
}
