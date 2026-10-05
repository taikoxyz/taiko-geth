package utils

import (
	"flag"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

// newDevnetEtnaTimeContext parses args, with env as the value of
// TAIKO_DEVNET_ETNA_TIME, against a copy of TaikoDevnetEtnaTimeFlag, so the
// shared flag is never mutated.
func newDevnetEtnaTimeContext(t *testing.T, env string, args ...string) *cli.Context {
	t.Helper()
	t.Setenv("TAIKO_DEVNET_ETNA_TIME", env)
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

func TestTaikoDevnetEtnaTime(t *testing.T) {
	cases := []struct {
		name string
		env  string
		args []string
		want *uint64 // nil means never
	}{
		{name: "unset leaves etna unscheduled"},
		{name: "zero activates etna at genesis", args: []string{"--taiko.devnet-etna-time", "0"}, want: uint64Ptr(0)},
		{name: "timestamp schedules etna", args: []string{"--taiko.devnet-etna-time", "100"}, want: uint64Ptr(100)},
		{name: "env zero activates etna at genesis", env: "0", want: uint64Ptr(0)},
		{name: "env timestamp schedules etna", env: "100", want: uint64Ptr(100)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := taikoDevnetEtnaTime(newDevnetEtnaTimeContext(t, c.env, c.args...))
			switch {
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

func TestTaikoDevnetEtnaTimeFlagHelp(t *testing.T) {
	if help := TaikoDevnetEtnaTimeFlag.String(); !strings.Contains(help, "(default: never)") {
		t.Fatalf("flag help %q does not show the default as never", help)
	}
}
