package utils

import (
	"flag"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestTaikoDevnetForkTimes(t *testing.T) {
	cases := []struct {
		name      string
		set       map[string]string
		wantUnzen uint64
		wantEtna  uint64
		wantErr   bool
	}{
		{name: "defaults", wantUnzen: 0, wantEtna: 0},
		{
			name:      "etna follows unzen",
			set:       map[string]string{TaikoDevnetUnzenTimeFlag.Name: "100"},
			wantUnzen: 100,
			wantEtna:  100,
		},
		{
			name: "etna after unzen",
			set: map[string]string{
				TaikoDevnetUnzenTimeFlag.Name: "100",
				TaikoDevnetEtnaTimeFlag.Name:  "200",
			},
			wantUnzen: 100,
			wantEtna:  200,
		},
		{
			name:      "etna only",
			set:       map[string]string{TaikoDevnetEtnaTimeFlag.Name: "200"},
			wantUnzen: 0,
			wantEtna:  200,
		},
		{
			name: "etna before unzen",
			set: map[string]string{
				TaikoDevnetUnzenTimeFlag.Name: "100",
				TaikoDevnetEtnaTimeFlag.Name:  "50",
			},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := flag.NewFlagSet("test", flag.ContinueOnError)
			for _, f := range []cli.Flag{&TaikoDevnetUnzenTimeFlag, &TaikoDevnetEtnaTimeFlag} {
				if err := f.Apply(set); err != nil {
					t.Fatalf("failed to apply flag %q: %v", f.Names()[0], err)
				}
			}
			for name, value := range c.set {
				if err := set.Set(name, value); err != nil {
					t.Fatalf("failed to set %q: %v", name, err)
				}
			}
			unzen, etna, err := taikoDevnetForkTimes(cli.NewContext(cli.NewApp(), set, nil))
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got unzen=%d etna=%d", unzen, etna)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if unzen != c.wantUnzen || etna != c.wantEtna {
				t.Fatalf("fork times = (%d, %d), want (%d, %d)", unzen, etna, c.wantUnzen, c.wantEtna)
			}
		})
	}
}
