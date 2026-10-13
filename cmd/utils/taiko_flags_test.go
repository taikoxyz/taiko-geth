package utils

import (
	"bytes"
	"flag"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/urfave/cli/v2"
)

// newDevnetEtnaTimeContext parses args, with env as the value of
// TAIKO_DEVNET_ETNA_TIME (nil: unset), against a copy of
// TaikoDevnetEtnaTimeFlag, so the shared flag is never mutated.
func newDevnetEtnaTimeContext(t *testing.T, env *string, args ...string) *cli.Context {
	t.Helper()
	t.Setenv("TAIKO_DEVNET_ETNA_TIME", "") // restored after the test
	if env != nil {
		t.Setenv("TAIKO_DEVNET_ETNA_TIME", *env)
	} else if err := os.Unsetenv("TAIKO_DEVNET_ETNA_TIME"); err != nil {
		t.Fatalf("unset TAIKO_DEVNET_ETNA_TIME: %v", err)
	}
	etnaFlag := TaikoDevnetEtnaTimeFlag
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := etnaFlag.Apply(set); err != nil {
		t.Fatalf("apply flag: %v", err)
	}
	if err := set.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	app := cli.NewApp()
	app.Flags = []cli.Flag{&etnaFlag}
	return cli.NewContext(app, set, nil)
}

func uint64Ptr(v uint64) *uint64 { return &v }

func stringPtr(v string) *string { return &v }

// formatEtnaTime formats an Etna time, nil being never.
func formatEtnaTime(etnaTime *uint64) string {
	if etnaTime == nil {
		return "never"
	}
	return strconv.FormatUint(*etnaTime, 10)
}

// TestTaikoDevnetEtnaTime pins the devnet Etna time of the flag and its
// environment variable: unset means never, and a set value must be a decimal
// u64 as the reference client's decimal argument parses it (an optional
// leading '+', leading zeros allowed). Any other value, an explicitly empty
// one included, is an error.
func TestTaikoDevnetEtnaTime(t *testing.T) {
	devnet := params.TaikoInternalNetworkID.Uint64()
	cases := []struct {
		name    string
		env     *string
		args    []string
		want    *uint64 // nil means never
		wantErr bool
	}{
		{name: "unset leaves etna unscheduled"},
		{name: "zero activates etna at genesis", args: []string{"--taiko.devnet-etna-time", "0"}, want: uint64Ptr(0)},
		{name: "timestamp schedules etna", args: []string{"--taiko.devnet-etna-time", "100"}, want: uint64Ptr(100)},
		{name: "leading zeros are decimal", args: []string{"--taiko.devnet-etna-time", "0100"}, want: uint64Ptr(100)},
		{name: "leading plus", args: []string{"--taiko.devnet-etna-time", "+100"}, want: uint64Ptr(100)},
		{name: "largest timestamp", args: []string{"--taiko.devnet-etna-time", "18446744073709551615"}, want: uint64Ptr(18446744073709551615)},
		{name: "env zero activates etna at genesis", env: stringPtr("0"), want: uint64Ptr(0)},
		{name: "env timestamp schedules etna", env: stringPtr("100"), want: uint64Ptr(100)},
		{name: "env leading zeros are decimal", env: stringPtr("0100"), want: uint64Ptr(100)},
		{name: "flag overrides env", env: stringPtr("100"), args: []string{"--taiko.devnet-etna-time", "200"}, want: uint64Ptr(200)},

		{name: "hex", args: []string{"--taiko.devnet-etna-time", "0x64"}, wantErr: true},
		{name: "octal prefix", args: []string{"--taiko.devnet-etna-time", "0o144"}, wantErr: true},
		{name: "underscores", args: []string{"--taiko.devnet-etna-time", "1_000"}, wantErr: true},
		{name: "negative", args: []string{"--taiko.devnet-etna-time", "-1"}, wantErr: true},
		{name: "two signs", args: []string{"--taiko.devnet-etna-time", "++1"}, wantErr: true},
		{name: "sign only", args: []string{"--taiko.devnet-etna-time", "+"}, wantErr: true},
		{name: "space", args: []string{"--taiko.devnet-etna-time", " 100"}, wantErr: true},
		{name: "overflow", args: []string{"--taiko.devnet-etna-time", "18446744073709551616"}, wantErr: true},
		{name: "explicitly empty flag", args: []string{"--taiko.devnet-etna-time="}, wantErr: true},
		{name: "env hex", env: stringPtr("0x64"), wantErr: true},
		{name: "explicitly empty env", env: stringPtr(""), wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := taikoDevnetEtnaTime(newDevnetEtnaTimeContext(t, c.env, c.args...), devnet)
			switch {
			case c.wantErr:
				if err == nil {
					t.Fatalf("etna time = %s, want an error", formatEtnaTime(got))
				}
				return
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.want == nil && got != nil:
				t.Fatalf("etna time = %d, want nil (never)", *got)
			case c.want != nil && got == nil:
				t.Fatalf("etna time = nil, want %d", *c.want)
			case c.want != nil && *got != *c.want:
				t.Fatalf("etna time = %d, want %d", *got, *c.want)
			}
		})
	}
}

// TestTaikoDevnetEtnaTimeOffDevnet pins that the devnet Etna time is ignored
// with a warning on mainnet and Hoodi, and applies silently to the internal
// devnet and to the unknown network IDs that fall back to it.
func TestTaikoDevnetEtnaTimeOffDevnet(t *testing.T) {
	for _, c := range []struct {
		name      string
		networkID uint64
		wantWarn  bool
	}{
		{"mainnet", params.TaikoMainnetNetworkID.Uint64(), true},
		{"hoodi", params.TaikoHoodiNetworkID.Uint64(), true},
		{"internal devnet", params.TaikoInternalNetworkID.Uint64(), false},
		{"unknown network", 12345, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var logs bytes.Buffer
			defaultLogger := log.Root()
			log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(&logs, log.LevelInfo, false)))
			t.Cleanup(func() { log.SetDefault(defaultLogger) })

			got, err := taikoDevnetEtnaTime(newDevnetEtnaTimeContext(t, nil, "--taiko.devnet-etna-time", "100"), c.networkID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			warned := strings.Contains(logs.String(), "Ignoring --taiko.devnet-etna-time")
			if warned != c.wantWarn {
				t.Fatalf("warned = %v, want %v; logs: %q", warned, c.wantWarn, logs.String())
			}
			if c.wantWarn && got != nil {
				t.Fatalf("etna time = %d, want nil (ignored)", *got)
			}
			if !c.wantWarn && (got == nil || *got != 100) {
				t.Fatalf("etna time = %s, want 100", formatEtnaTime(got))
			}
		})
	}
}

func TestTaikoDevnetEtnaTimeFlagHelp(t *testing.T) {
	if help := TaikoDevnetEtnaTimeFlag.String(); !strings.Contains(help, "(default: never)") {
		t.Fatalf("flag help %q does not show the default as never", help)
	}
}
