package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

// TestTaikoDevnetUnzenTimeFlagRemoved pins that the devnet Unzen override is
// gone: neither the flag nor its environment variable is registered, and
// passing the flag fails startup as an undefined flag.
func TestTaikoDevnetUnzenTimeFlagRemoved(t *testing.T) {
	for _, f := range app.Flags {
		if slices.Contains(f.Names(), "taiko.devnet-unzen-time") {
			t.Fatalf("flag %v is still registered", f.Names())
		}
		if df, ok := f.(cli.DocGenerationFlag); ok && slices.Contains(df.GetEnvVars(), "TAIKO_DEVNET_UNZEN_TIME") {
			t.Fatalf("flag %v still reads TAIKO_DEVNET_UNZEN_TIME", f.Names())
		}
	}
	geth := runGeth(t, "--taiko.devnet-unzen-time", "0", "version")
	geth.WaitExit()
	if status := geth.ExitStatus(); status == 0 {
		t.Fatal("geth accepted --taiko.devnet-unzen-time, want a startup failure")
	}
	if stderr := geth.StderrText(); !strings.Contains(stderr, "flag provided but not defined: -taiko.devnet-unzen-time") {
		t.Fatalf("stderr = %q, want an undefined-flag error", stderr)
	}
}

// TestTaikoDevnetEtnaTimeStartup pins how startup treats the devnet Etna
// time: a value that is not a decimal timestamp, from the flag or an
// explicitly empty environment variable, stops startup, and a valid value on
// a network other than the internal devnet is ignored with a warning.
func TestTaikoDevnetEtnaTimeStartup(t *testing.T) {
	const warning = "Ignoring --taiko.devnet-etna-time: it applies only to the Taiko internal devnet"
	for _, tt := range []struct {
		name     string
		env      *string
		args     []string
		wantFail bool
		wantWarn bool
	}{
		{name: "hex flag", args: []string{"--taiko.devnet-etna-time", "0x64"}, wantFail: true},
		{name: "underscore flag", args: []string{"--taiko.devnet-etna-time", "1_000"}, wantFail: true},
		{name: "empty env", env: new(string), wantFail: true},
		{name: "devnet", args: []string{"--taiko.devnet-etna-time", "0100"}},
		{name: "mainnet", args: []string{"--networkid", "167000", "--taiko.devnet-etna-time", "100"}, wantWarn: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != nil {
				t.Setenv("TAIKO_DEVNET_ETNA_TIME", *tt.env)
			}
			geth := runGeth(t, append(append([]string{"--taiko"}, tt.args...), "dumpconfig")...)
			geth.Output()
			geth.WaitExit()
			stderr := geth.StderrText()
			if failed := geth.ExitStatus() != 0; failed != tt.wantFail {
				t.Fatalf("exit status %d, want a failure: %v; stderr = %q", geth.ExitStatus(), tt.wantFail, stderr)
			}
			if tt.wantFail && !strings.Contains(stderr, "invalid --taiko.devnet-etna-time value") {
				t.Fatalf("stderr = %q, want an invalid-value error", stderr)
			}
			if warned := strings.Contains(stderr, warning); warned != tt.wantWarn {
				t.Fatalf("warned = %v, want %v; stderr = %q", warned, tt.wantWarn, stderr)
			}
		})
	}
}
