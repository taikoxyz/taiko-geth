// Copyright 2019 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

// Package utils contains internal helper functions for go-ethereum commands.
package utils

import (
	"flag"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/urfave/cli/v2"
)

func Test_SplitTagsFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args string
		want map[string]string
	}{
		{
			"2 tags case",
			"host=localhost,bzzkey=123",
			map[string]string{
				"host":   "localhost",
				"bzzkey": "123",
			},
		},
		{
			"1 tag case",
			"host=localhost123",
			map[string]string{
				"host": "localhost123",
			},
		},
		{
			"empty case",
			"",
			map[string]string{},
		},
		{
			"garbage",
			"smth=smthelse=123",
			map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SplitTagsFlag(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitTagsFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetEthConfigUsesNetworkIDForTaikoGenesis(t *testing.T) {
	t.Parallel()

	app := cli.NewApp()
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range []cli.Flag{
		CacheFlag,
		CryptoKZGFlag,
		FDLimitFlag,
		GCModeFlag,
		NetworkIdFlag,
		&TaikoFlag,
	} {
		if err := f.Apply(set); err != nil {
			t.Fatalf("failed to apply flag %q: %v", f.Names()[0], err)
		}
	}
	if err := set.Set(NetworkIdFlag.Name, params.MasayaDevnetNetworkID.String()); err != nil {
		t.Fatalf("failed to set %q: %v", NetworkIdFlag.Name, err)
	}
	if err := set.Set(TaikoFlag.Name, "true"); err != nil {
		t.Fatalf("failed to set %q: %v", TaikoFlag.Name, err)
	}
	ctx := cli.NewContext(app, set, nil)

	stack, err := node.New(&node.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	defer stack.Close()

	cfg := ethconfig.Defaults
	SetEthConfig(ctx, stack, &cfg)

	if cfg.NetworkId != params.MasayaDevnetNetworkID.Uint64() {
		t.Fatalf("network ID mismatch: have %d want %d", cfg.NetworkId, params.MasayaDevnetNetworkID.Uint64())
	}

	want := core.TaikoGenesisBlock(params.MasayaDevnetNetworkID.Uint64()).ToBlock().Hash()
	if got := cfg.Genesis.ToBlock().Hash(); got != want {
		t.Fatalf("genesis hash mismatch: have %s want %s", got, want)
	}
}
