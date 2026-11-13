//go:build zkgastrace

package miner

// Trace comparison for the shared Unzen zk gas block vectors: each vector's block is built with
// the zk gas trace exporter on, and the trace of that build must equal the alethia-reth trace
// committed in <ZK_GAS_VECTORS_DIR>/traces event by event. Built only with the zkgastrace tag; the
// result comparison in taiko_zk_gas_vectors_test.go runs with and without it.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// runZkBlockTrace runs v like runZkBlockVector with the trace exporter on while its block is
// built, and returns the run's result and the trace of the build.
func runZkBlockTrace(t *testing.T, v zkBlockVector) (zkBlockExpected, []vm.ZkGasTraceEvent) {
	t.Helper()
	var events []vm.ZkGasTraceEvent
	got := runZkBlockVectorSealedBy(t, v, func(t *testing.T, miner *Miner, parent *types.Header, env zkBlockEnv, txs types.Transactions) *types.Block {
		t.Helper()
		trace := vm.StartZkGasTrace()
		defer trace.Stop()
		block := sealZkBlock(t, miner, parent, env, txs)
		trace.Stop()
		var err error
		if events, err = trace.BlockEvents(len(txs)); err != nil {
			t.Fatalf("geth trace: %v", err)
		}
		return block
	})
	return got, events
}

func TestZkGasBlockTraceOfTransferMatchesTheReferenceExample(t *testing.T) {
	_, events := runZkBlockTrace(t, inlineTransferVector(t))
	// traces/smoke.block.transfer.jsonl, as the corpus README quotes it.
	want := `{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"step","d":1,"pc":0,"op":0,"gas":"0xeeff8","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0xeeff8","spawn":false,"zk":"0x0","ok":true,"charged":true}
{"e":"tx_end","index":0,"status":"committed","tx_zk":"0x3b538"}
{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"tx_end","index":1,"status":"committed","tx_zk":"0x3b538"}
`
	if text := vm.RenderZkGasTrace(events); text != want {
		t.Fatalf("geth trace\n%s\nwant\n%s", text, want)
	}
}

// TestZkGasBlockTraceOfTxAboveRemainingGas checks the one normalization of the trace comparison
// on a 3,000,000-gas block whose second tx asks for the whole block gas limit, more than the block
// has left after the anchor (and less than the EIP-7825 tx gas cap). geth charges its intrinsic zk gas before the gas pool rejects it and the next
// reset discards the charge; alethia-reth rejects it before charging. The comparison reduces a
// skipped tx whose only event is an accepted intrinsic charge to its lone tx_end.
func TestZkGasBlockTraceOfTxAboveRemainingGas(t *testing.T) {
	v := inlineTransferVector(t)
	v.Input.Env.GasLimit = 3_000_000
	signer2 := zkTxSigner(2)
	account2, err := signer2.address()
	if err != nil {
		t.Fatal(err)
	}
	v.Input.Alloc[account2] = v.Input.Alloc[common.HexToAddress("0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf")]
	oversized := v.Input.Txs[1]
	oversized.Signer, oversized.Gas = signer2, v.Input.Env.GasLimit
	v.Input.Txs = []zkBlockTx{v.Input.Txs[0], oversized, v.Input.Txs[1]}
	v.Expected = &zkBlockExpected{RawTxs: signZkBlockTxs(t, v.Input)}

	got, events := runZkBlockTrace(t, v)
	if len(got.Included) != 2 || got.Included[1] != 2 {
		t.Fatalf("included %v, want [0 2]", got.Included)
	}
	text := vm.RenderZkGasTrace(events)
	geth := `{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"step","d":1,"pc":0,"op":0,"gas":"0xeeff8","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0xeeff8","spawn":false,"zk":"0x0","ok":true,"charged":true}
{"e":"tx_end","index":0,"status":"committed","tx_zk":"0x3b538"}
{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"tx_end","index":1,"status":"skipped","tx_zk":null}
{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"tx_end","index":2,"status":"committed","tx_zk":"0x3b538"}
`
	if text != geth {
		t.Fatalf("geth trace\n%s\nwant\n%s", text, geth)
	}
	reference, err := vm.ParseZkGasTrace([]byte(`{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"step","d":1,"pc":0,"op":0,"gas":"0xeeff8","static":false,"sl":0,"stack":[],"mem":"0x0","facts":{},"gas_after":"0xeeff8","spawn":false,"zk":"0x0","ok":true,"charged":true}
{"e":"tx_end","index":0,"status":"committed","tx_zk":"0x3b538"}
{"e":"tx_end","index":1,"status":"skipped","tx_zk":null}
{"e":"intrinsic","zk":"0x3b538","ok":true}
{"e":"tx_end","index":2,"status":"committed","tx_zk":"0x3b538"}
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.CompareZkGasTrace(reference, events); err != nil {
		t.Fatalf("the skipped tx's discarded intrinsic charge was compared: %v", err)
	}
}

func TestZkGasBlockTraces(t *testing.T) {
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
	vectors, err := loadZkGasVectors[zkBlockVector](dir, "block")
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	matched, events := 0, 0
	for _, v := range vectors {
		ok := t.Run(v.ID, func(t *testing.T) {
			if known[v.ID] {
				t.Skipf("known divergent, skipped: %s", v.ID)
			}
			if v.Expected == nil {
				t.Fatal("vector has no expected values")
			}
			want, err := vm.ReadZkGasTraceFile(dir, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			got, trace := runZkBlockTrace(t, v)
			if dump := os.Getenv("ZK_GAS_TRACE_DUMP_DIR"); dump != "" {
				if err := os.WriteFile(filepath.Join(dump, v.ID+".jsonl"), []byte(vm.RenderZkGasTrace(trace)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var errs []error
			// The traced build must give the expected values too: reading facts must not change
			// what is metered.
			if err := compareZkBlock(*v.Expected, got); err != nil {
				errs = append(errs, fmt.Errorf("traced run: %w", err))
			}
			if err := vm.CompareZkGasTrace(want, trace); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", v.ID, err))
			}
			if len(errs) > 0 {
				t.Fatal(errs)
			}
			matched++
			events += len(trace)
		})
		if !ok {
			failed = append(failed, v.ID)
		}
	}
	if len(failed) > 0 {
		t.Errorf("%d of %d block traces failed: %v", len(failed), len(vectors), failed)
	}
	t.Logf("%d of %d block traces (%d events) equal to the reference", matched, len(vectors), events)
}
