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
