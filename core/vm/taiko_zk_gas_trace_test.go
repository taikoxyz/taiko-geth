//go:build zkgastrace

package vm

// Trace comparison for the shared Unzen zk gas harness vectors: each vector runs with the zk gas
// trace exporter on, and its trace must equal the alethia-reth trace committed in
// <ZK_GAS_VECTORS_DIR>/traces event by event. Built only with the zkgastrace tag; the result
// comparison in taiko_zk_gas_vectors_test.go runs with and without it.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/holiman/uint256"
)

// zkTraceFixture is a harness trace in trace schema v1: three steps of smoke.harness.add, then
// the STATICCALL step and the point evaluation run of precompile.point_evaluation.bad_input, and
// a frame_end. It is not one coherent run; the comparison tests only need its lines.
const zkTraceFixture = `{"e":"step","d":1,"pc":0,"op":96,"gas":"0x186a0","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x1869d","spawn":false,"zk":"0x1b","ok":true,"charged":true}
{"e":"step","d":1,"pc":4,"op":1,"gas":"0x1869a","static":false,"sl":2,"stack":["0x2","0x1"],"mem":"0x0","facts":{},"gas_after":"0x18697","spawn":false,"zk":"0x39","ok":true,"charged":true}
{"e":"step","d":1,"pc":18,"op":250,"gas":"0xf4202","static":false,"sl":6,"stack":["0xc738","0xa","0x0","0xbf","0x0","0x0"],"mem":"0xc0","facts":{"account_warm":true,"account_empty":true,"delegate_warm":null},"gas_after":"0xe7a66","spawn":true,"zk":"0x13a74","ok":true,"charged":true}
{"e":"precompile","addr":"0x000000000000000000000000000000000000000a","supplied":"0xc738","left":"0x0","success":false,"zk":"0x29c78e8","ok":true}
{"e":"frame_end","status":"halt","frame_zk":"0x29e0dbc"}
`

// zkBlockTraceFixture is a block trace with a committed anchor, a tx skipped before its intrinsic
// charge (a lone tx_end) and a committed transfer.
const zkBlockTraceFixture = `{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"step","d":1,"pc":0,"op":0,"gas":"0xeeff8","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0xeeff8","spawn":false,"zk":"0x0","ok":true,"charged":true}
{"e":"tx_end","index":0,"status":"committed","tx_zk":"0x3b538"}
{"e":"tx_end","index":1,"status":"skipped","tx_zk":null}
{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"tx_end","index":2,"status":"committed","tx_zk":"0x3b538"}
`

// zkEmptyInitcodeTraceFixture is the trace of create.create.zero_size_exact_gas: three PUSH0,
// a CREATE of empty initcode that spawns, the STOP step its frame runs at depth 2 (event 4), then
// the top frame's STOP.
const zkEmptyInitcodeTraceFixture = `{"e":"step","d":1,"pc":0,"op":95,"gas":"0x7d06","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x7d04","spawn":false,"zk":"0x1a","ok":true,"charged":true}
{"e":"step","d":1,"pc":1,"op":95,"gas":"0x7d04","static":false,"sl":1,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x7d02","spawn":false,"zk":"0x1a","ok":true,"charged":true}
{"e":"step","d":1,"pc":2,"op":95,"gas":"0x7d02","static":false,"sl":2,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x7d00","spawn":false,"zk":"0x1a","ok":true,"charged":true}
{"e":"step","d":1,"pc":3,"op":240,"gas":"0x7d00","static":false,"sl":3,"stack":["0x0","0x0","0x0"],"mem":"0x0","facts":{},"gas_after":"0x0","spawn":true,"zk":"0x9088","ok":true,"charged":true}
{"e":"step","d":2,"pc":0,"op":0,"gas":"0x0","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x0","spawn":false,"zk":"0x0","ok":true,"charged":true}
{"e":"step","d":1,"pc":4,"op":0,"gas":"0x0","static":false,"sl":1,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x0","spawn":false,"zk":"0x0","ok":true,"charged":true}
{"e":"frame_end","status":"success","frame_zk":"0x90d6"}
`

// mustParseZkTrace parses a fixture trace.
func mustParseZkTrace(t *testing.T, text string) []ZkGasTraceEvent {
	t.Helper()
	events, err := ParseZkGasTrace([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// cloneZkTrace deep-copies events so a mutation does not reach the original.
func cloneZkTrace(events []ZkGasTraceEvent) []ZkGasTraceEvent {
	out := make([]ZkGasTraceEvent, len(events))
	for i, e := range events {
		e.Stack = slices.Clone(e.Stack)
		e.Facts = slices.Clone(e.Facts)
		out[i] = e
	}
	return out
}

func TestZkGasTraceParseRendersCanonicalLines(t *testing.T) {
	for _, fixture := range []string{zkTraceFixture, zkBlockTraceFixture} {
		events := mustParseZkTrace(t, fixture)
		if got := RenderZkGasTrace(events); got != fixture {
			t.Fatalf("parse and render changed the trace:\n%s\nwant\n%s", got, fixture)
		}
	}
	bad := map[string]string{
		"field order":         `{"e":"intrinsic","ok":true,"zk":"0x3b538"}`,
		"unknown field":       `{"e":"intrinsic","zk":"0x3b538","ok":true,"x":1}`,
		"missing field":       `{"e":"intrinsic","zk":"0x3b538"}`,
		"leading zero":        `{"e":"intrinsic","zk":"0x03b538","ok":true}`,
		"uppercase hex":       `{"e":"intrinsic","zk":"0x3B538","ok":true}`,
		"string integer":      `{"e":"tx_end","index":"1","status":"skipped","tx_zk":null}`,
		"unknown event":       `{"e":"bogus"}`,
		"unknown fact":        strings.Replace(strings.SplitN(zkTraceFixture, "\n", 4)[2], "delegate_warm", "delegate_cold", 1),
		"trailing data":       `{"e":"intrinsic","zk":"0x3b538","ok":true} 1`,
		"mixed case address":  `{"e":"precompile","addr":"0x000000000000000000000000000000000000000A","supplied":"0x1","left":"0x0","success":false,"zk":"0x1","ok":true}`,
		"no trailing newline": `{"e":"intrinsic","zk":"0x3b538","ok":true}`,
	}
	for name, text := range bad {
		if name != "no trailing newline" {
			text += "\n"
		}
		if _, err := ParseZkGasTrace([]byte(text)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestZkGasTraceCompareRejectsMutations(t *testing.T) {
	ref := mustParseZkTrace(t, zkTraceFixture)
	if err := CompareZkGasTrace(ref, cloneZkTrace(ref)); err != nil {
		t.Fatalf("identical trace rejected: %v", err)
	}
	const spawnStep, precompile = 2, 3
	if !ref[spawnStep].Spawn || ref[precompile].Kind != "precompile" {
		t.Fatal("fixture changed")
	}
	// Each mutation of the reference and the prefix its error must start with; "" means the
	// mutated reference must still match.
	mutations := []struct {
		name   string
		mutate func([]ZkGasTraceEvent) []ZkGasTraceEvent
		want   string
	}{
		{"zk + 1", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[1].Zk++; return e }, "zk gas mismatch at event 1"},
		{"one bit of a pre stack item", func(e []ZkGasTraceEvent) []ZkGasTraceEvent {
			e[spawnStep].Stack[0].Xor(&e[spawnStep].Stack[0], uint256.NewInt(1))
			return e
		}, "setup divergence at event 2"},
		{"a pre fact", func(e []ZkGasTraceEvent) []ZkGasTraceEvent {
			e[spawnStep].Facts[0].Value = "false"
			return e
		}, "setup divergence at event 2"},
		{"pre gas", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[0].Gas++; return e }, "setup divergence at event 0"},
		{"precompile before its CALL step", func(e []ZkGasTraceEvent) []ZkGasTraceEvent {
			e[spawnStep], e[precompile] = e[precompile], e[spawnStep]
			return e
		}, "zk gas mismatch at event 2"},
		{"a step deleted", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { return slices.Delete(e, 1, 2) }, "zk gas mismatch at event 1"},
		{"a step added", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { return slices.Insert(e, 1, e[0]) }, "zk gas mismatch at event 1"},
		{"the precompile deleted", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { return slices.Delete(e, precompile, precompile+1) }, "zk gas mismatch at event 3"},
		{"the last event deleted", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { return e[:len(e)-1] }, "zk gas mismatch at event 4"},
		{"non-spawn gas_after", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[0].GasAfter--; return e }, "zk gas mismatch at event 0"},
		{"spawn flag", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[spawnStep].Spawn = false; return e }, "zk gas mismatch at event 2"},
		{"charged flag", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[0].Charged = false; return e }, "zk gas mismatch at event 0"},
		{"ok flag", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[1].Ok = false; return e }, "zk gas mismatch at event 1"},
		{"precompile left", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[precompile].Left++; return e }, "zk gas mismatch at event 3"},
		{"precompile address", func(e []ZkGasTraceEvent) []ZkGasTraceEvent {
			e[precompile].Addr = common.Address{19: 0x09}
			return e
		}, "zk gas mismatch at event 3"},
		{"frame_end status", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[4].Status = "success"; return e }, "zk gas mismatch at event 4"},
		{"frame_end total", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[4].FrameZk = nil; return e }, "zk gas mismatch at event 4"},
		{"spawn step gas_after only", func(e []ZkGasTraceEvent) []ZkGasTraceEvent { e[spawnStep].GasAfter++; return e }, ""},
	}
	for _, m := range mutations {
		want := m.mutate(cloneZkTrace(ref))
		err := CompareZkGasTrace(want, ref)
		switch {
		case m.want == "" && err != nil:
			t.Errorf("%s: rejected: %v", m.name, err)
		case m.want == "":
		case err == nil:
			t.Errorf("%s: accepted", m.name)
		case !strings.HasPrefix(err.Error(), m.want+":"):
			t.Errorf("%s: reported as %q, want %q", m.name, err, m.want)
		}
	}
}

func TestZkGasTraceCompareNormalizesOnlyDiscardedIntrinsicOfSkippedTx(t *testing.T) {
	ref := mustParseZkTrace(t, zkBlockTraceFixture)
	intrinsic := ref[0]
	charged := slices.Insert(cloneZkTrace(ref), 3, intrinsic) // tx 1 charged its intrinsic before it was skipped
	if err := CompareZkGasTrace(ref, charged); err != nil {
		t.Fatalf("a skipped tx's discarded intrinsic charge was compared: %v", err)
	}
	if err := CompareZkGasTrace(charged, ref); err != nil {
		t.Fatalf("normalization is not symmetric: %v", err)
	}
	// A later difference names the event's line in each trace.
	bumped := cloneZkTrace(ref)
	bumped[4].Zk++
	if err := CompareZkGasTrace(charged, bumped); err == nil || !strings.HasPrefix(err.Error(), "zk gas mismatch at event 5 (geth event 4):") {
		t.Fatalf("difference after a normalized tx reported as %v", err)
	}
	refused := intrinsic
	refused.Ok = false
	step := ref[1]
	for name, got := range map[string][]ZkGasTraceEvent{
		"a refused intrinsic in a skipped tx":  slices.Insert(cloneZkTrace(ref), 3, refused),
		"a step in a skipped tx":               slices.Insert(cloneZkTrace(ref), 3, intrinsic, step),
		"a committed tx without its intrinsic": slices.Delete(cloneZkTrace(ref), 4, 5),
	} {
		if err := CompareZkGasTrace(ref, got); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestZkGasTraceCompareSkipsOnlyTheEmptyCodeStop(t *testing.T) {
	ref := mustParseZkTrace(t, zkEmptyInitcodeTraceFixture)
	const stop = 4
	geth := slices.Delete(cloneZkTrace(ref), stop, stop+1) // no step for the empty initcode
	if err := CompareZkGasTrace(ref, geth); err != nil {
		t.Fatalf("the empty-code STOP step was compared: %v", err)
	}
	// At the end of a trace too: a CREATE that is the frame's last step before frame_end.
	if err := CompareZkGasTrace(ref[:stop+1], geth[:stop]); err != nil {
		t.Fatalf("a trailing empty-code STOP step was compared: %v", err)
	}
	// Each change to the reference STOP keeps it compared, and so does a geth-only STOP.
	cases := []struct {
		name   string
		mutate func(e *ZkGasTraceEvent)
	}{
		{"zk not 0", func(e *ZkGasTraceEvent) { e.Zk = 1 }},
		{"pc not 0", func(e *ZkGasTraceEvent) { e.PC = 1 }},
		{"gas_after not gas", func(e *ZkGasTraceEvent) { e.Gas, e.GasAfter = 5, 4 }},
		{"not STOP", func(e *ZkGasTraceEvent) { e.Op = byte(INVALID) }},
		{"not one depth below the spawn", func(e *ZkGasTraceEvent) { e.Depth = 3 }},
	}
	for _, c := range cases {
		want := cloneZkTrace(ref)
		c.mutate(&want[stop])
		if err := CompareZkGasTrace(want, geth); err == nil || !strings.HasPrefix(err.Error(), "zk gas mismatch at event 4") {
			t.Errorf("reference STOP with %s: reported as %v", c.name, err)
		}
	}
	// Not right after a spawn step: the CREATE did not spawn.
	notSpawned := cloneZkTrace(ref)
	notSpawned[stop-1].Spawn = false
	gethNotSpawned := slices.Delete(cloneZkTrace(notSpawned), stop, stop+1)
	if err := CompareZkGasTrace(notSpawned, gethNotSpawned); err == nil {
		t.Error("a STOP step after a step that did not spawn was skipped")
	}
	// geth has a step of that depth at that position: it is compared with the STOP.
	gethStep := cloneZkTrace(ref)
	gethStep[stop].Op = byte(INVALID)
	if err := CompareZkGasTrace(ref, gethStep); err == nil || !strings.HasPrefix(err.Error(), "setup divergence at event 4") {
		t.Errorf("a geth step in place of the STOP reported as %v", err)
	}
	// A STOP step only geth has is never skipped.
	if err := CompareZkGasTrace(geth, ref); err == nil || !strings.HasPrefix(err.Error(), "zk gas mismatch at event 4") {
		t.Errorf("a geth-only STOP step reported as %v", err)
	}
}

// runZkHarnessTrace runs in like runZkHarnessVector with the trace exporter on, and returns the
// run's result and its trace.
func runZkHarnessTrace(t *testing.T, in zkHarnessInput) (zkHarnessExpected, []ZkGasTraceEvent) {
	t.Helper()
	trace := StartZkGasTrace()
	defer trace.Stop()
	got := runZkHarnessVector(t, in)
	trace.Stop()
	var frameZk *uint64
	if got.Status != zkHarnessZkLimit {
		zk := got.FrameZkGas.Uint64()
		frameZk = &zk
	}
	events, err := trace.HarnessEvents(got.Status, frameZk)
	if err != nil {
		t.Fatalf("geth trace: %v", err)
	}
	return got, events
}

func TestZkGasTraceOfAddMatchesTheReferenceExample(t *testing.T) {
	got, events := runZkHarnessTrace(t, inlineAddInput())
	if got.Status != zkHarnessSuccess || got.FrameZkGas.Uint64() != 0x6f {
		t.Fatalf("result %s", got)
	}
	// traces/smoke.harness.add.jsonl, as the corpus README quotes it.
	want := `{"e":"step","d":1,"pc":0,"op":96,"gas":"0x186a0","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x1869d","spawn":false,"zk":"0x1b","ok":true,"charged":true}
{"e":"step","d":1,"pc":2,"op":96,"gas":"0x1869d","static":false,"sl":1,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x1869a","spawn":false,"zk":"0x1b","ok":true,"charged":true}
{"e":"step","d":1,"pc":4,"op":1,"gas":"0x1869a","static":false,"sl":2,"stack":["0x2","0x1"],"mem":"0x0","facts":{},"gas_after":"0x18697","spawn":false,"zk":"0x39","ok":true,"charged":true}
{"e":"step","d":1,"pc":5,"op":0,"gas":"0x18697","static":false,"sl":1,"stack":[],"mem":"0x0","facts":{},"gas_after":"0x18697","spawn":false,"zk":"0x0","ok":true,"charged":true}
{"e":"frame_end","status":"success","frame_zk":"0x6f"}
`
	if text := RenderZkGasTrace(events); text != want {
		t.Fatalf("geth trace\n%s\nwant\n%s", text, want)
	}
}

func TestZkGasTraceOfEmptyCodeTargetIsTheFrameEndAlone(t *testing.T) {
	in := inlineAddInput()
	to := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	in.Alloc[to] = zkVectorAccount{Balance: hexutil.U256(*uint256.NewInt(1))}
	_, events := runZkHarnessTrace(t, in)
	if text := RenderZkGasTrace(events); text != `{"e":"frame_end","status":"success","frame_zk":"0x0"}`+"\n" {
		t.Fatalf("geth trace\n%s", text)
	}
}

func TestZkGasTracePreFactsHaveNoSideEffects(t *testing.T) {
	// PUSH1 0, SLOAD, POP, PUSH1 0, SLOAD, STOP with slot 0 = 7.
	in := inlineAddInput()
	to := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	in.Alloc[to] = zkVectorAccount{
		Code:    hexutil.MustDecode("0x6000545060005400"),
		Storage: map[hexutil.U256]hexutil.U256{{}: hexutil.U256(*uint256.NewInt(7))},
	}
	untraced := runZkHarnessVector(t, in)
	traced, events := runZkHarnessTrace(t, in)
	if err := compareZkHarness(untraced, traced); err != nil {
		t.Fatalf("reading facts changed the result: %v", err)
	}
	var warm []string
	for _, e := range events {
		if e.Kind == "step" && OpCode(e.Op) == SLOAD {
			warm = append(warm, e.Facts[0].Value)
		}
	}
	if !slices.Equal(warm, []string{"false", "true"}) {
		t.Fatalf("slot_warm of the two SLOADs %v, want cold then warm", warm)
	}
	// Negative control: a reader that warmed the slot before the first SLOAD would change the
	// frame zk gas, so the result comparison above catches a fact read with side effects.
	zkGasTraceTestBeforePre = func(evm *EVM, contract *Contract, op OpCode) {
		if op == SLOAD {
			evm.StateDB.AddSlotToAccessList(contract.Address(), common.Hash{})
		}
	}
	defer func() { zkGasTraceTestBeforePre = nil }()
	if warmed, _ := runZkHarnessTrace(t, in); compareZkHarness(untraced, warmed) == nil {
		t.Fatalf("a slot-warming reader left the result at %s", warmed)
	}
}

func TestZkGasTraceFilesRoundTrip(t *testing.T) {
	dir, err := zkGasCorpusDir(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Skip("ZK_GAS_VECTORS_DIR is not set")
	}
	entries, err := os.ReadDir(dir + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".jsonl")
		if !ok {
			continue
		}
		data, err := os.ReadFile(dir + "/traces/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		events, err := ParseZkGasTrace(data)
		if err != nil {
			t.Fatalf("traces/%s.jsonl: %v", id, err)
		}
		if RenderZkGasTrace(events) != string(data) {
			t.Fatalf("traces/%s.jsonl: rendering the parsed trace changes it", id)
		}
		for i, e := range events {
			if e.Kind == "step" && len(e.Stack) != min(e.StackLen, int(zkGasTraceInputs[e.Op])) {
				t.Fatalf("traces/%s.jsonl event %d: %d stack items for op %#x with %d on the stack", id, i, len(e.Stack), e.Op, e.StackLen)
			}
		}
		n++
	}
	if n == 0 {
		t.Fatal("no traces")
	}
	t.Logf("%d traces round-trip", n)
}

// dumpZkGasTrace writes the geth trace of vector id to <ZK_GAS_TRACE_DUMP_DIR>/<id>.jsonl when
// that variable is set, for diffing against the reference by hand.
func dumpZkGasTrace(t *testing.T, id string, events []ZkGasTraceEvent) {
	t.Helper()
	dir := os.Getenv("ZK_GAS_TRACE_DUMP_DIR")
	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(RenderZkGasTrace(events)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestZkGasHarnessTraces(t *testing.T) {
	dir, err := zkGasCorpusDir(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Skip("ZK_GAS_VECTORS_DIR is not set")
	}
	known, err := zkGasKnownDivergent(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := loadZkGasVectors[zkHarnessVector](dir, "harness")
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	matched, events := 0, 0
	for _, v := range vectors {
		ok := t.Run(v.ID, func(t *testing.T) {
			if v.Expected == nil {
				t.Fatal("vector has no expected values")
			}
			want, err := ReadZkGasTraceFile(dir, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			got, trace := runZkHarnessTrace(t, v.Input)
			dumpZkGasTrace(t, v.ID, trace)
			var errs []error
			// The traced run must give the expected values too: reading facts must not change
			// what is metered.
			if err := compareZkHarness(*v.Expected, got); err != nil {
				errs = append(errs, fmt.Errorf("traced run: %w", err))
			}
			if err := CompareZkGasTrace(want, trace); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", v.ID, err))
			}
			switch {
			case known[v.ID]:
				t.Logf("known divergent, not asserted: %v", errs)
			case len(errs) > 0:
				t.Fatal(errs)
			default:
				matched++
				events += len(trace)
			}
		})
		if !ok {
			failed = append(failed, v.ID)
		}
	}
	if len(failed) > 0 {
		t.Errorf("%d of %d harness traces failed: %v", len(failed), len(vectors), failed)
	}
	t.Logf("%d of %d harness traces (%d events) equal to the reference", matched, len(vectors), events)
}
