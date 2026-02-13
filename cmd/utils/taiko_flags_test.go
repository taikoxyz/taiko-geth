package utils

import (
	"flag"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	"github.com/urfave/cli/v2"
)

func newTaikoInternalShastaTimeContext(t *testing.T, args []string) *cli.Context {
	t.Helper()

	app := cli.NewApp()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := TaikoInternalShastaTimeFlag.Apply(fs); err != nil {
		t.Fatalf("failed to apply taiko internal shasta time flag: %v", err)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	return cli.NewContext(app, fs, nil)
}

func TestApplyTaikoInternalShastaTimeOverride_Unset(t *testing.T) {
	t.Parallel()

	original := core.InternalShastaTime
	t.Cleanup(func() {
		core.InternalShastaTime = original
	})

	core.InternalShastaTime = 1770966750
	ctx := newTaikoInternalShastaTimeContext(t, nil)

	applyTaikoInternalShastaTimeOverride(ctx)

	if core.InternalShastaTime != 1770966750 {
		t.Fatalf("unexpected override when flag is unset, got %d", core.InternalShastaTime)
	}
}

func TestApplyTaikoInternalShastaTimeOverride_Set(t *testing.T) {
	t.Parallel()

	original := core.InternalShastaTime
	t.Cleanup(func() {
		core.InternalShastaTime = original
	})

	core.InternalShastaTime = 1770966750
	ctx := newTaikoInternalShastaTimeContext(t, []string{"--taiko.internal-shasta-time", "1770967000"})

	applyTaikoInternalShastaTimeOverride(ctx)

	if core.InternalShastaTime != 1770967000 {
		t.Fatalf("expected override to 1770967000, got %d", core.InternalShastaTime)
	}
}
