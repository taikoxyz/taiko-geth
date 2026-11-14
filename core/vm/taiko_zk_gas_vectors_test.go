package vm

// Runner for the shared Unzen zk gas harness vectors in taiko-mono's testdata/zk_gas_vectors,
// found through ZK_GAS_VECTORS_DIR. alethia-reth generates every expected value; this runner only
// reads the vectors and asserts them against taiko-geth.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Harness statuses, as they appear in a vector's expected values.
const (
	zkHarnessSuccess = "success"  // the tx succeeded
	zkHarnessRevert  = "revert"   // the tx reverted
	zkHarnessHalt    = "halt"     // the tx halted exceptionally
	zkHarnessZkLimit = "zk_limit" // metering exceeded the block zk gas limit and the tx aborted
)

// zkVectorSchemaVersion is the only vector schema version this runner reads.
const zkVectorSchemaVersion = 1

// zkHarnessVector is one harness-layer vector file.
type zkHarnessVector struct {
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	Layer         string             `json:"layer"`
	Cells         []string           `json:"cells"`
	Description   string             `json:"description"`
	Input         zkHarnessInput     `json:"input"`
	Expected      *zkHarnessExpected `json:"expected"`
}

// zkHarnessInput is one tx from Caller to To with empty calldata, value 0, gas price 0 and base
// fee 0, whose top frame gets FrameGas gas.
type zkHarnessInput struct {
	Alloc        map[common.Address]zkVectorAccount `json:"alloc"`
	Caller       common.Address                     `json:"caller"`
	To           common.Address                     `json:"to"`
	FrameGas     hexutil.Uint64                     `json:"frame_gas"`
	ZkBlockLimit hexutil.Uint64                     `json:"zk_block_limit"`
	Number       hexutil.U256                       `json:"number"`
	Timestamp    hexutil.Uint64                     `json:"timestamp"`
	Coinbase     common.Address                     `json:"coinbase"`
}

// zkVectorAccount is the pre-state of one alloc account. Only Balance is required; a missing
// nonce is 0 and missing code or storage is empty.
type zkVectorAccount struct {
	Balance hexutil.U256                  `json:"balance"`
	Nonce   hexutil.Uint64                `json:"nonce"`
	Code    hexutil.Bytes                 `json:"code"`
	Storage map[hexutil.U256]hexutil.U256 `json:"storage"`
}

// zkHarnessExpected is the outcome of a harness run. FrameZkGas excludes the per-tx intrinsic zk
// gas and is nil exactly when Status is zk_limit.
type zkHarnessExpected struct {
	FrameZkGas *big.Int
	Status     string
}

// UnmarshalJSON decodes expected values strictly: unknown fields, an unknown status, and a
// frame_zk_gas that is null for anything but zk_limit (or set for zk_limit) are errors.
func (e *zkHarnessExpected) UnmarshalJSON(data []byte) error {
	var raw struct {
		FrameZkGas *hexutil.Big `json:"frame_zk_gas"`
		Status     string       `json:"status"`
	}
	if err := decodeZkVectorJSON(data, &raw); err != nil {
		return err
	}
	switch raw.Status {
	case zkHarnessSuccess, zkHarnessRevert, zkHarnessHalt, zkHarnessZkLimit:
	default:
		return fmt.Errorf("unknown harness status %q", raw.Status)
	}
	if (raw.FrameZkGas == nil) != (raw.Status == zkHarnessZkLimit) {
		return fmt.Errorf("frame_zk_gas must be null exactly when status is %s", zkHarnessZkLimit)
	}
	e.FrameZkGas, e.Status = (*big.Int)(raw.FrameZkGas), raw.Status
	return nil
}

// String renders e the way it appears in a vector file.
func (e zkHarnessExpected) String() string {
	zkGas := "null"
	if e.FrameZkGas != nil {
		zkGas = strconv.Quote(hexutil.EncodeBig(e.FrameZkGas))
	}
	return fmt.Sprintf(`{"frame_zk_gas":%s,"status":%q}`, zkGas, e.Status)
}

// decodeZkVectorJSON decodes one JSON value into v, rejecting unknown fields and trailing data.
func decodeZkVectorJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

// loadZkGasVectors decodes every vector of layer from <dir>/cases/*.json, in file name order. It
// fails when a file cannot be read or decoded, and when no vector of layer exists.
func loadZkGasVectors[T any](dir, layer string) ([]T, error) {
	cases := filepath.Join(dir, "cases")
	entries, err := os.ReadDir(cases)
	if err != nil {
		return nil, err
	}
	var vectors []T
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		path := filepath.Join(cases, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var head struct {
			SchemaVersion int    `json:"schema_version"`
			ID            string `json:"id"`
			Layer         string `json:"layer"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		switch {
		case head.SchemaVersion != zkVectorSchemaVersion:
			return nil, fmt.Errorf("%s: schema_version %d, want %d", path, head.SchemaVersion, zkVectorSchemaVersion)
		case head.Layer == "":
			return nil, fmt.Errorf("%s: missing layer", path)
		case head.ID != strings.TrimSuffix(name, ".json"):
			return nil, fmt.Errorf("%s: id %q does not match the file name", path, head.ID)
		case head.Layer != layer:
			continue
		}
		var vector T
		if err := decodeZkVectorJSON(data, &vector); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		vectors = append(vectors, vector)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("no %s vectors in %s", layer, cases)
	}
	return vectors, nil
}

// zkGasCorpusDir returns ZK_GAS_VECTORS_DIR, read through lookup. An empty dir with a nil error
// means the corpus test skips; with ZK_GAS_VECTORS_REQUIRED=1 an unset directory is an error
// instead, so that a run that must check the corpus cannot pass without it.
func zkGasCorpusDir(lookup func(string) (string, bool)) (string, error) {
	if dir, _ := lookup("ZK_GAS_VECTORS_DIR"); dir != "" {
		return dir, nil
	}
	if required, _ := lookup("ZK_GAS_VECTORS_REQUIRED"); required == "1" {
		return "", errors.New("ZK_GAS_VECTORS_REQUIRED=1 but ZK_GAS_VECTORS_DIR is not set")
	}
	return "", nil
}

// zkGasKnownDivergent returns the vector ids listed in ZK_GAS_VECTORS_KNOWN_DIVERGENT, read
// through lookup. The variable is for local investigation only and is an error when CI is set.
func zkGasKnownDivergent(lookup func(string) (string, bool)) (map[string]bool, error) {
	list, set := lookup("ZK_GAS_VECTORS_KNOWN_DIVERGENT")
	if _, ci := lookup("CI"); set && ci {
		return nil, errors.New("ZK_GAS_VECTORS_KNOWN_DIVERGENT is refused because CI is set: it is for local investigation only")
	}
	ids := make(map[string]bool)
	for _, id := range strings.Split(list, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids[id] = true
		}
	}
	return ids, nil
}

// newZkHarnessState returns a state holding exactly the alloc accounts, committed and reopened
// so that every account is pre-existing state rather than created in the tx.
func newZkHarnessState(t *testing.T, alloc map[common.Address]zkVectorAccount) *state.StateDB {
	t.Helper()

	db := state.NewDatabaseForTesting()
	statedb, err := state.New(types.EmptyRootHash, db)
	if err != nil {
		t.Fatal(err)
	}
	for addr, account := range alloc {
		statedb.SetBalance(addr, new(uint256.Int).Set((*uint256.Int)(&account.Balance)), tracing.BalanceChangeUnspecified)
		statedb.SetNonce(addr, uint64(account.Nonce), tracing.NonceChangeUnspecified)
		if len(account.Code) > 0 {
			statedb.SetCode(addr, account.Code, tracing.CodeChangeUnspecified)
		}
		for slot, value := range account.Storage {
			statedb.SetState(addr, (*uint256.Int)(&slot).Bytes32(), (*uint256.Int)(&value).Bytes32())
		}
	}
	root, err := statedb.Commit(0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	statedb, err = state.New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	return statedb
}

// runZkHarnessVector runs in on an Osaka EVM metered with the Unzen schedule, whose block limit
// is in.ZkBlockLimit. It performs the steps of a call tx that the state transition does before
// EVM.Call (access-list warming, the caller nonce bump, delegation-target warming) but does not
// charge the per-tx intrinsic zk gas.
func runZkHarnessVector(t *testing.T, in zkHarnessInput) zkHarnessExpected {
	t.Helper()

	statedb := newZkHarnessState(t, in.Alloc)
	blockCtx := BlockContext{
		CanTransfer: func(db StateDB, addr common.Address, amount *uint256.Int) bool {
			return db.GetBalance(addr).Cmp(amount) >= 0
		},
		Transfer: func(db StateDB, sender, recipient common.Address, amount *uint256.Int, _ *params.Rules) {
			db.SubBalance(sender, amount, tracing.BalanceChangeTransfer)
			db.AddBalance(recipient, amount, tracing.BalanceChangeTransfer)
		},
		GetHash: func(n uint64) common.Hash {
			return crypto.Keccak256Hash([]byte(strconv.FormatUint(n, 10)))
		},
		Coinbase:    in.Coinbase,
		GasLimit:    math.MaxUint64,
		BlockNumber: (*uint256.Int)(&in.Number).ToBig(),
		Time:        uint64(in.Timestamp),
		Difficulty:  new(big.Int),
		BaseFee:     new(big.Int),
		BlobBaseFee: big.NewInt(1),
		Random:      &common.Hash{},
	}
	evm := NewEVM(blockCtx, statedb, params.MergedTestChainConfig, Config{})
	evm.SetTxContext(TxContext{Origin: in.Caller, GasPrice: new(uint256.Int)})
	schedule := UnzenZkGasSchedule
	schedule.BlockLimit = uint64(in.ZkBlockLimit)
	meter := NewZkGasMeter(&schedule)
	evm.SetZkGasMeter(meter)

	rules := evm.chainRules
	statedb.Prepare(rules, in.Caller, in.Coinbase, &in.To, ActivePrecompiles(rules), nil)
	statedb.SetNonce(in.Caller, statedb.GetNonce(in.Caller)+1, tracing.NonceChangeEoACall)
	if target, ok := types.ParseDelegation(statedb.GetCode(in.To)); ok {
		statedb.AddAddressToAccessList(target)
	}

	_, _, err := evm.Call(in.Caller, in.To, nil, uint64(in.FrameGas), new(uint256.Int))
	status, err := zkHarnessStatus(err, evm.zkGasErr)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if status == zkHarnessZkLimit {
		return zkHarnessExpected{Status: zkHarnessZkLimit}
	}
	return zkHarnessExpected{FrameZkGas: new(big.Int).SetUint64(meter.TxZkGasUsed()), Status: status}
}

// zkHarnessStatus maps the error EVM.Call returned to a harness status. Like block processing and
// block building, which abort a tx only when the error it returns is ErrZkGasLimitExceeded, the
// status is zk_limit exactly when callErr is that error. A returned zk gas limit error without the
// sticky one recorded by the meter (stickyErr), or the reverse, is a runner error.
func zkHarnessStatus(callErr, stickyErr error) (string, error) {
	limit := errors.Is(callErr, ErrZkGasLimitExceeded)
	if limit != (stickyErr != nil) {
		return "", fmt.Errorf("EVM.Call returned %v but the sticky zk gas error is %v", callErr, stickyErr)
	}
	switch {
	case callErr == nil:
		return zkHarnessSuccess, nil
	case errors.Is(callErr, ErrExecutionReverted):
		return zkHarnessRevert, nil
	case limit:
		return zkHarnessZkLimit, nil
	default:
		return zkHarnessHalt, nil
	}
}

// compareZkHarness reports whether got matches want. For zk_limit only the status is compared,
// because the clients record the partial zk gas of an aborted tx differently by design.
func compareZkHarness(want, got zkHarnessExpected) error {
	if want.Status != got.Status {
		return fmt.Errorf("expected %s, got %s", want, got)
	}
	if want.Status == zkHarnessZkLimit {
		return nil
	}
	if want.FrameZkGas == nil || got.FrameZkGas == nil || want.FrameZkGas.Cmp(got.FrameZkGas) != 0 {
		return fmt.Errorf("expected %s, got %s", want, got)
	}
	return nil
}

// zkHarnessAddInputJSON is the input of the corpus vector smoke.harness.add.
const zkHarnessAddInputJSON = `{
	"alloc": {
		"0x000000000000000000000000000000000000c0de": { "balance": "0x0", "code": "0x600160020100" }
	},
	"caller": "0x000000000000000000000000000000000000ca11",
	"to": "0x000000000000000000000000000000000000c0de",
	"frame_gas": "0x186a0",
	"zk_block_limit": "0x5f5e100",
	"number": "0x1",
	"timestamp": "0xc",
	"coinbase": "0x0000000000000000000000000000000000c0ffee"
}`

// inlineAddInput returns the input of smoke.harness.add without reading the corpus.
func inlineAddInput() zkHarnessInput {
	var in zkHarnessInput
	if err := decodeZkVectorJSON([]byte(zkHarnessAddInputJSON), &in); err != nil {
		panic(err)
	}
	return in
}

// writeZkVectorFile writes one vector file into <dir>/cases.
func writeZkVectorFile(t *testing.T, dir, id, body string) {
	t.Helper()
	cases := filepath.Join(dir, "cases")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cases, id+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestZkGasHarnessCompareRejectsMutations(t *testing.T) {
	got := runZkHarnessVector(t, inlineAddInput())
	if got.Status != zkHarnessSuccess {
		t.Fatalf("status %s", got.Status)
	}
	if err := compareZkHarness(got, got); err != nil {
		t.Fatalf("identical result rejected: %v", err)
	}
	m := got
	m.FrameZkGas = new(big.Int).Add(got.FrameZkGas, common.Big1)
	if compareZkHarness(m, got) == nil {
		t.Fatal("bumped zk gas accepted")
	}
	m = got
	m.Status = zkHarnessRevert
	if compareZkHarness(m, got) == nil {
		t.Fatal("changed status accepted")
	}
}

func TestZkGasHarnessCompareZkLimitIgnoresZkGas(t *testing.T) {
	want := zkHarnessExpected{Status: zkHarnessZkLimit}
	if err := compareZkHarness(want, zkHarnessExpected{FrameZkGas: big.NewInt(7), Status: zkHarnessZkLimit}); err != nil {
		t.Fatalf("zk_limit compared zk gas: %v", err)
	}
	if compareZkHarness(want, zkHarnessExpected{FrameZkGas: big.NewInt(7), Status: zkHarnessHalt}) == nil {
		t.Fatal("zk_limit matched a halt")
	}
}

func TestZkGasHarnessStatusMapping(t *testing.T) {
	to := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	tests := []struct {
		name    string
		code    string
		zkLimit uint64
		want    string
	}{
		{"success", "0x600160020100", 100_000_000, zkHarnessSuccess},
		{"revert", "0x60006000fd", 100_000_000, zkHarnessRevert}, // PUSH1 0, PUSH1 0, REVERT
		{"halt", "0x600056", 100_000_000, zkHarnessHalt},         // PUSH1 0, JUMP to a non-JUMPDEST
		{"zk_limit", "0x600160020100", 100, zkHarnessZkLimit},    // the ADD step crosses the limit
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := inlineAddInput()
			in.Alloc[to] = zkVectorAccount{Code: hexutil.MustDecode(tt.code)}
			in.ZkBlockLimit = hexutil.Uint64(tt.zkLimit)
			got := runZkHarnessVector(t, in)
			if got.Status != tt.want {
				t.Fatalf("status %s, want %s", got.Status, tt.want)
			}
			if (got.FrameZkGas == nil) != (tt.want == zkHarnessZkLimit) {
				t.Fatalf("frame zk gas %v for status %s", got.FrameZkGas, got.Status)
			}
		})
	}
}

func TestZkGasVectorLoaderRejectsEmptyLayer(t *testing.T) {
	if _, err := loadZkGasVectors[zkHarnessVector](t.TempDir(), "harness"); err == nil {
		t.Fatal("empty dir accepted")
	}
	dir := t.TempDir()
	writeZkVectorFile(t, dir, "only.block", `{"schema_version": 1, "id": "only.block", "layer": "block"}`)
	if _, err := loadZkGasVectors[zkHarnessVector](dir, "harness"); err == nil {
		t.Fatal("corpus without harness vectors accepted")
	}
}

func TestZkGasVectorLoaderRejectsUnknownFields(t *testing.T) {
	vector := func(input, expected, extra string) string {
		return `{"schema_version": 1, "id": "v", "layer": "harness", "cells": ["c"], "description": "d",` +
			extra + `"input": ` + input + `, "expected": ` + expected + `}`
	}
	expected := `{"frame_zk_gas": "0x6f", "status": "success"}`

	dir := t.TempDir()
	writeZkVectorFile(t, dir, "v", vector(zkHarnessAddInputJSON, expected, ""))
	vectors, err := loadZkGasVectors[zkHarnessVector](dir, "harness")
	if err != nil {
		t.Fatalf("valid vector rejected: %v", err)
	}
	if len(vectors) != 1 || vectors[0].ID != "v" || vectors[0].Expected == nil || vectors[0].Expected.FrameZkGas.Uint64() != 0x6f {
		t.Fatalf("decoded %+v", vectors)
	}

	withAccountField := `{
		"alloc": { "0x000000000000000000000000000000000000c0de": { "balance": "0x0", "bogus": "0x1" } },
		"caller": "0x000000000000000000000000000000000000ca11",
		"to": "0x000000000000000000000000000000000000c0de",
		"frame_gas": "0x186a0", "zk_block_limit": "0x5f5e100", "number": "0x1", "timestamp": "0xc",
		"coinbase": "0x0000000000000000000000000000000000c0ffee"
	}`
	for name, body := range map[string]string{
		"top level": vector(zkHarnessAddInputJSON, expected, `"bogus": 1,`),
		"account":   vector(withAccountField, expected, ""),
		"expected":  vector(zkHarnessAddInputJSON, `{"frame_zk_gas": "0x6f", "status": "success", "bogus": 1}`, ""),
	} {
		dir := t.TempDir()
		writeZkVectorFile(t, dir, "v", body)
		if _, err := loadZkGasVectors[zkHarnessVector](dir, "harness"); err == nil {
			t.Errorf("unknown field in %s accepted", name)
		}
	}
}

func TestZkGasHarnessStatusRejectsStickyMismatch(t *testing.T) {
	limit := fmt.Errorf("frame: %w", ErrZkGasLimitExceeded)
	tests := []struct {
		name              string
		callErr, sticky   error
		want              string
		wantRunnerFailure bool
	}{
		{"success", nil, nil, zkHarnessSuccess, false},
		{"revert", ErrExecutionReverted, nil, zkHarnessRevert, false},
		{"halt", ErrOutOfGas, nil, zkHarnessHalt, false},
		{"zk_limit", limit, ErrZkGasLimitExceeded, zkHarnessZkLimit, false},
		{"sticky error, call succeeded", nil, ErrZkGasLimitExceeded, "", true},
		{"sticky error, call halted", ErrOutOfGas, ErrZkGasLimitExceeded, "", true},
		{"returned limit error, no sticky error", limit, nil, "", true},
	}
	for _, tt := range tests {
		got, err := zkHarnessStatus(tt.callErr, tt.sticky)
		if (err != nil) != tt.wantRunnerFailure || got != tt.want {
			t.Errorf("%s: got status %q, error %v", tt.name, got, err)
		}
	}
}

func TestZkGasCorpusEnvironment(t *testing.T) {
	env := func(vars map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			value, ok := vars[key]
			return value, ok
		}
	}
	if dir, err := zkGasCorpusDir(env(nil)); dir != "" || err != nil {
		t.Errorf("unset corpus: dir %q, error %v; want a skip", dir, err)
	}
	if dir, err := zkGasCorpusDir(env(map[string]string{"ZK_GAS_VECTORS_DIR": "/vectors", "ZK_GAS_VECTORS_REQUIRED": "1"})); dir != "/vectors" || err != nil {
		t.Errorf("set corpus: dir %q, error %v", dir, err)
	}
	if _, err := zkGasCorpusDir(env(map[string]string{"ZK_GAS_VECTORS_REQUIRED": "1"})); err == nil {
		t.Error("ZK_GAS_VECTORS_REQUIRED=1 without ZK_GAS_VECTORS_DIR accepted")
	}
	ids, err := zkGasKnownDivergent(env(map[string]string{"ZK_GAS_VECTORS_KNOWN_DIVERGENT": " a, b ,"}))
	if err != nil || len(ids) != 2 || !ids["a"] || !ids["b"] {
		t.Errorf("known divergent ids %v, error %v", ids, err)
	}
	if _, err := zkGasKnownDivergent(env(map[string]string{"CI": "true"})); err != nil {
		t.Errorf("CI without known divergent ids refused: %v", err)
	}
	if _, err := zkGasKnownDivergent(env(map[string]string{"CI": "true", "ZK_GAS_VECTORS_KNOWN_DIVERGENT": "a"})); err == nil {
		t.Error("ZK_GAS_VECTORS_KNOWN_DIVERGENT accepted with CI set")
	}
}

func TestZkGasHarnessVectors(t *testing.T) {
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
	for _, v := range vectors {
		ok := t.Run(v.ID, func(t *testing.T) {
			if v.Expected == nil {
				t.Fatal("vector has no expected values")
			}
			got := runZkHarnessVector(t, v.Input)
			err := compareZkHarness(*v.Expected, got)
			switch {
			case known[v.ID]:
				t.Logf("known divergent, not asserted: geth %s, expected %s, match %t", got, v.Expected, err == nil)
			case err != nil:
				t.Fatal(err)
			default:
				t.Logf("geth %s", got)
			}
		})
		if !ok {
			failed = append(failed, v.ID)
		}
	}
	if len(failed) > 0 {
		t.Errorf("%d of %d harness vectors failed: %v", len(failed), len(vectors), failed)
	}
}
