package vm

// CHANGE(taiko): Assert Lean-generated meter vectors against the production Unzen meter.
// This runner only reads the shared corpus; it never generates expected values.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type zkMeterVector struct {
	ID         string
	BlockLimit uint64
	Actions    []zkMeterAction
	Expected   []zkMeterExpected
}

type zkMeterAction struct {
	Kind    string
	Op      byte
	Gas     uint64
	Address common.Address
}

type zkMeterExpected struct {
	OK      bool
	TxZk    uint64
	BlockZk uint64
}

// zkMeterUint64 accepts only canonical lowercase hexadecimal quantities that fit a u64.
type zkMeterUint64 uint64

// decodeZkMeterObject additionally rejects duplicate keys and case aliases, which Go's
// struct decoder otherwise accepts even with DisallowUnknownFields enabled.
func decodeZkMeterObject(data []byte, value any, fields ...string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return errors.New("meter schema requires a JSON object")
	}
	seen := make(map[string]bool)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		field := token.(string)
		if !slices.Contains(fields, field) || seen[field] {
			return fmt.Errorf("unknown or duplicate meter field %q", field)
		}
		seen[field] = true
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return decodeZkVectorJSON(data, value)
}

func (n *zkMeterUint64) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	value, err := hexutil.DecodeUint64(text)
	if err != nil {
		return err
	}
	if text != hexutil.EncodeUint64(value) {
		return fmt.Errorf("noncanonical hexadecimal u64 %q", text)
	}
	*n = zkMeterUint64(value)
	return nil
}

func (v *zkMeterVector) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID         string            `json:"id"`
		BlockLimit *zkMeterUint64    `json:"block_limit"`
		Actions    []zkMeterAction   `json:"actions"`
		Expected   []zkMeterExpected `json:"expected"`
	}
	if err := decodeZkMeterObject(data, &raw, "id", "block_limit", "actions", "expected"); err != nil {
		return err
	}
	if raw.ID == "" || raw.BlockLimit == nil {
		return errors.New("meter vector requires a nonempty id and block_limit")
	}
	if len(raw.Actions) == 0 || len(raw.Actions) != len(raw.Expected) {
		return fmt.Errorf("%s: actions must be nonempty and match expected length", raw.ID)
	}
	*v = zkMeterVector{raw.ID, uint64(*raw.BlockLimit), raw.Actions, raw.Expected}
	return nil
}

func (a *zkMeterAction) UnmarshalJSON(data []byte) error {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	*a = zkMeterAction{Kind: head.Kind}
	switch head.Kind {
	case "opcode":
		var raw struct {
			Kind string         `json:"kind"`
			Op   *uint8         `json:"op"`
			Gas  *zkMeterUint64 `json:"gas"`
		}
		if err := decodeZkMeterObject(data, &raw, "kind", "op", "gas"); err != nil {
			return err
		}
		if raw.Op == nil || raw.Gas == nil {
			return errors.New("opcode action requires op and gas")
		}
		a.Op, a.Gas = *raw.Op, uint64(*raw.Gas)
	case "spawn":
		var raw struct {
			Kind string `json:"kind"`
			Op   *uint8 `json:"op"`
		}
		if err := decodeZkMeterObject(data, &raw, "kind", "op"); err != nil {
			return err
		}
		if raw.Op == nil || !IsSpawnOpcode(OpCode(*raw.Op)) {
			return errors.New("spawn action requires one of the six spawn opcodes")
		}
		a.Op = *raw.Op
	case "precompile":
		var raw struct {
			Kind    string         `json:"kind"`
			Address string         `json:"address"`
			Gas     *zkMeterUint64 `json:"gas"`
		}
		if err := decodeZkMeterObject(data, &raw, "kind", "address", "gas"); err != nil {
			return err
		}
		address, err := hexutil.Decode(raw.Address)
		if err != nil || len(address) != common.AddressLength || raw.Address != hexutil.Encode(address) {
			return fmt.Errorf("precompile address must be lowercase 20-byte hex: %q", raw.Address)
		}
		if raw.Gas == nil {
			return errors.New("precompile action requires gas")
		}
		a.Address, a.Gas = common.BytesToAddress(address), uint64(*raw.Gas)
	case "intrinsic", "commit", "reset":
		if err := decodeZkMeterObject(data, &head, "kind"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown meter action %q", head.Kind)
	}
	return nil
}

func (e *zkMeterExpected) UnmarshalJSON(data []byte) error {
	var raw struct {
		OK      *bool          `json:"ok"`
		TxZk    *zkMeterUint64 `json:"tx_zk"`
		BlockZk *zkMeterUint64 `json:"block_zk"`
	}
	if err := decodeZkMeterObject(data, &raw, "ok", "tx_zk", "block_zk"); err != nil {
		return err
	}
	if raw.OK == nil || raw.TxZk == nil || raw.BlockZk == nil {
		return errors.New("meter expectation requires ok, tx_zk and block_zk")
	}
	*e = zkMeterExpected{*raw.OK, uint64(*raw.TxZk), uint64(*raw.BlockZk)}
	return nil
}

func loadZkMeterVectors(dir string) ([]zkMeterVector, error) {
	path := filepath.Join(dir, "meter-vectors.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var corpus struct {
		SchemaVersion int             `json:"schema_version"`
		Schedule      string          `json:"schedule"`
		Vectors       []zkMeterVector `json:"vectors"`
	}
	if err := decodeZkMeterObject(data, &corpus, "schema_version", "schedule", "vectors"); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if corpus.SchemaVersion != 1 || corpus.Schedule != "unzen" {
		return nil, fmt.Errorf("%s: expected schema_version 1 and schedule unzen", path)
	}
	if len(corpus.Vectors) == 0 {
		return nil, fmt.Errorf("%s: empty meter corpus", path)
	}
	ids := make(map[string]bool)
	for _, vector := range corpus.Vectors {
		if ids[vector.ID] {
			return nil, fmt.Errorf("%s: duplicate meter vector id %q", path, vector.ID)
		}
		ids[vector.ID] = true
	}
	return corpus.Vectors, nil
}

func runZkMeterVector(vector zkMeterVector) error {
	if len(vector.Actions) == 0 || len(vector.Actions) != len(vector.Expected) {
		return errors.New("actions must be nonempty and match expected length")
	}
	schedule := UnzenZkGasSchedule
	schedule.BlockLimit = vector.BlockLimit
	meter := NewZkGasMeter(&schedule)
	for i, action := range vector.Actions {
		var err error
		switch action.Kind {
		case "opcode":
			err = meter.ChargeOpcode(action.Op, action.Gas)
		case "spawn":
			if !IsSpawnOpcode(OpCode(action.Op)) {
				return fmt.Errorf("action %d: not a spawn opcode: %d", i, action.Op)
			}
			err = meter.ChargeOpcode(action.Op, meter.SpawnEstimate(action.Op))
		case "precompile":
			err = meter.ChargePrecompile(action.Address, action.Gas, 0, nil)
		case "intrinsic":
			err = meter.ChargeTxIntrinsic()
		case "commit":
			err = meter.CommitTransaction()
		case "reset":
			meter.ResetTransaction()
		default:
			return fmt.Errorf("action %d: unknown meter action %q", i, action.Kind)
		}
		if err != nil && !errors.Is(err, ErrZkGasLimitExceeded) {
			return fmt.Errorf("action %d (%s): unexpected meter error: %w", i, action.Kind, err)
		}
		got := zkMeterExpected{err == nil, meter.TxZkGasUsed(), meter.BlockZkGasUsed()}
		if want := vector.Expected[i]; got != want {
			return fmt.Errorf("action %d (%s): got %+v, want %+v (meter error: %v)", i, action.Kind, got, want, err)
		}
	}
	return nil
}

// The literal ADD fixture is independent of the production schedule: gas 3 costs 57 zk gas.
const zkMeterAddVectorJSON = `{"id":"add","block_limit":"0x39","actions":[{"kind":"opcode","op":1,"gas":"0x3"}],"expected":[{"ok":true,"tx_zk":"0x39","block_zk":"0x0"}]}`

func zkMeterCorpusJSON(vector string) string {
	return `{"schema_version":1,"schedule":"unzen","vectors":[` + vector + `]}`
}

func writeZkMeterCorpus(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "meter-vectors.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestZkGasMeterVectorReader(t *testing.T) {
	vectors, err := loadZkMeterVectors(writeZkMeterCorpus(t, zkMeterCorpusJSON(zkMeterAddVectorJSON)))
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || vectors[0].ID != "add" || vectors[0].BlockLimit != 57 {
		t.Fatalf("decoded vectors: %+v", vectors)
	}
	if err := runZkMeterVector(vectors[0]); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"spawn", "precompile", "intrinsic", "commit", "reset"} {
		t.Run(kind, func(t *testing.T) {
			action := `{"kind":"` + kind + `"}`
			if kind == "spawn" {
				action = `{"kind":"spawn","op":240}`
			}
			if kind == "precompile" {
				action = `{"kind":"precompile","address":"0x1234567890123456789012345678901234567890","gas":"0x0"}`
			}
			body := strings.Replace(zkMeterAddVectorJSON, `{"kind":"opcode","op":1,"gas":"0x3"}`, action, 1)
			if _, err := loadZkMeterVectors(writeZkMeterCorpus(t, zkMeterCorpusJSON(body))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestZkGasMeterVectorReaderRejectsMalformed(t *testing.T) {
	base := zkMeterCorpusJSON(zkMeterAddVectorJSON)
	mutate := func(old, replacement string) string { return strings.Replace(base, old, replacement, 1) }
	tests := map[string]string{
		"invalid JSON":             `{`,
		"trailing JSON":            base + `{}`,
		"null corpus":              `null`,
		"unknown schema":           mutate(`"schema_version":1`, `"schema_version":2`),
		"missing schema":           mutate(`"schema_version":1,`, ``),
		"unknown schedule":         mutate(`"unzen"`, `"other"`),
		"missing schedule":         mutate(`"schedule":"unzen",`, ``),
		"missing vectors":          `{"schema_version":1,"schedule":"unzen"}`,
		"empty vectors":            `{"schema_version":1,"schedule":"unzen","vectors":[]}`,
		"null vectors":             `{"schema_version":1,"schedule":"unzen","vectors":null}`,
		"duplicate IDs":            zkMeterCorpusJSON(zkMeterAddVectorJSON + `,` + zkMeterAddVectorJSON),
		"missing ID":               mutate(`"id":"add",`, ``),
		"empty ID":                 mutate(`"add"`, `""`),
		"missing block limit":      mutate(`"block_limit":"0x39",`, ``),
		"null block limit":         mutate(`"block_limit":"0x39"`, `"block_limit":null`),
		"missing actions":          mutate(`"actions":[{"kind":"opcode","op":1,"gas":"0x3"}],`, ``),
		"empty actions":            mutate(`[{"kind":"opcode","op":1,"gas":"0x3"}]`, `[]`),
		"null actions":             mutate(`[{"kind":"opcode","op":1,"gas":"0x3"}]`, `null`),
		"missing expected":         mutate(`,"expected":[{"ok":true,"tx_zk":"0x39","block_zk":"0x0"}]`, ``),
		"empty expected":           mutate(`[{"ok":true,"tx_zk":"0x39","block_zk":"0x0"}]`, `[]`),
		"length mismatch":          mutate(`"expected":[`, `"expected":[{"ok":true,"tx_zk":"0x39","block_zk":"0x0"},`),
		"unknown kind":             mutate(`"kind":"opcode"`, `"kind":"other"`),
		"missing kind":             mutate(`"kind":"opcode",`, ``),
		"missing opcode":           mutate(`"op":1,`, ``),
		"null opcode":              mutate(`"op":1`, `"op":null`),
		"negative opcode":          mutate(`"op":1`, `"op":-1`),
		"opcode overflow":          mutate(`"op":1`, `"op":256`),
		"fractional opcode":        mutate(`"op":1`, `"op":1.5`),
		"noninteger opcode":        mutate(`"op":1`, `"op":1.0`),
		"string opcode":            mutate(`"op":1`, `"op":"1"`),
		"missing gas":              mutate(`,"gas":"0x3"`, ``),
		"null gas":                 mutate(`"gas":"0x3"`, `"gas":null`),
		"noncanonical hex":         mutate(`"gas":"0x3"`, `"gas":"0x03"`),
		"uppercase hex":            mutate(`"gas":"0x3"`, `"gas":"0xA"`),
		"uppercase hex prefix":     mutate(`"gas":"0x3"`, `"gas":"0X3"`),
		"missing hex prefix":       mutate(`"gas":"0x3"`, `"gas":"3"`),
		"overflow hex":             mutate(`"gas":"0x3"`, `"gas":"0x10000000000000000"`),
		"nonspawn opcode":          mutate(`"kind":"opcode","op":1,"gas":"0x3"`, `"kind":"spawn","op":1`),
		"spawn extra gas":          mutate(`"kind":"opcode","op":1`, `"kind":"spawn","op":240`),
		"missing address":          mutate(`"kind":"opcode","op":1`, `"kind":"precompile"`),
		"short address":            mutate(`"kind":"opcode","op":1`, `"kind":"precompile","address":"0x04"`),
		"uppercase address":        mutate(`"kind":"opcode","op":1`, `"kind":"precompile","address":"0x000000000000000000000000000000000000000A"`),
		"invalid address":          mutate(`"kind":"opcode","op":1`, `"kind":"precompile","address":"0x000000000000000000000000000000000000000g"`),
		"lifecycle extra fields":   mutate(`"kind":"opcode"`, `"kind":"reset"`),
		"missing ok":               mutate(`"ok":true,`, ``),
		"null ok":                  mutate(`"ok":true`, `"ok":null`),
		"missing tx total":         mutate(`"tx_zk":"0x39",`, ``),
		"missing block total":      mutate(`,"block_zk":"0x0"`, ``),
		"null tx total":            mutate(`"tx_zk":"0x39"`, `"tx_zk":null`),
		"noncanonical tx total":    mutate(`"tx_zk":"0x39"`, `"tx_zk":"0x039"`),
		"unknown top field":        mutate(`"schema_version":1`, `"schema_version":1,"extra":null`),
		"unknown vector field":     mutate(`"id":"add"`, `"id":"add","extra":null`),
		"unknown action field":     mutate(`"op":1`, `"op":1,"extra":null`),
		"unknown expected field":   mutate(`"ok":true`, `"ok":true,"extra":null`),
		"noncanonical field name":  mutate(`"schema_version":1`, `"SCHEMA_VERSION":1`),
		"duplicate top field":      mutate(`"schema_version":1`, `"schema_version":2,"schema_version":1`),
		"duplicate vector field":   mutate(`"id":"add"`, `"id":"other","id":"add"`),
		"duplicate action field":   mutate(`"op":1`, `"op":0,"op":1`),
		"duplicate expected field": mutate(`"ok":true`, `"ok":false,"ok":true`),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := loadZkMeterVectors(writeZkMeterCorpus(t, body)); err == nil {
				t.Fatal("malformed meter corpus accepted")
			}
		})
	}
	if _, err := loadZkMeterVectors(t.TempDir()); err == nil {
		t.Fatal("missing meter corpus accepted")
	}
	if _, err := loadZkMeterVectors(writeZkMeterCorpus(t, "")); err == nil {
		t.Fatal("empty meter corpus accepted")
	}
}

func TestZkGasMeterVectorRejectsChangedExpected(t *testing.T) {
	for _, replacement := range []struct{ old, next string }{
		{`"ok":true`, `"ok":false`},
		{`"tx_zk":"0x39"`, `"tx_zk":"0x3a"`},
		{`"block_zk":"0x0"`, `"block_zk":"0x1"`},
	} {
		body := strings.Replace(zkMeterAddVectorJSON, replacement.old, replacement.next, 1)
		vectors, err := loadZkMeterVectors(writeZkMeterCorpus(t, zkMeterCorpusJSON(body)))
		if err != nil {
			t.Fatal(err)
		}
		if err := runZkMeterVector(vectors[0]); err == nil {
			t.Fatalf("changed expected accepted: %s", body)
		}
	}
}

func TestZkGasMeterVectorSnapshots(t *testing.T) {
	vectors := []zkMeterVector{
		{
			ID: "exact budget and rejection recovery", BlockLimit: 114,
			Actions: []zkMeterAction{
				{Kind: "opcode", Op: 1, Gas: 3}, {Kind: "commit"},
				{Kind: "opcode", Op: 1, Gas: 3}, {Kind: "opcode", Op: 1, Gas: 3},
				{Kind: "reset"}, {Kind: "opcode", Op: 1, Gas: 3}, {Kind: "commit"}, {Kind: "intrinsic"},
			},
			Expected: []zkMeterExpected{
				{true, 57, 0}, {true, 0, 57}, {true, 57, 57}, {false, 57, 57},
				{true, 0, 57}, {true, 57, 57}, {true, 0, 114}, {false, 0, 114},
			},
		},
		{
			ID: "production action dispatch", BlockLimit: 1_000_000,
			Actions: []zkMeterAction{
				{Kind: "opcode", Op: 1, Gas: 3},
				{Kind: "precompile", Address: common.Address{19: 4}, Gas: 3},
				{Kind: "spawn", Op: 240}, {Kind: "intrinsic"}, {Kind: "reset"}, {Kind: "commit"},
			},
			Expected: []zkMeterExpected{
				{true, 57, 0}, {true, 75, 0}, {true, 37_075, 0},
				{true, 280_075, 0}, {true, 0, 0}, {true, 0, 0},
			},
		},
	}
	for _, vector := range vectors {
		t.Run(vector.ID, func(t *testing.T) {
			if err := runZkMeterVector(vector); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestZkGasMeterVectors(t *testing.T) {
	dir, err := zkGasCorpusDir(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Skip("ZK_GAS_VECTORS_DIR is not set")
	}
	vectors, err := loadZkMeterVectors(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		t.Run(vector.ID, func(t *testing.T) {
			if err := runZkMeterVector(vector); err != nil {
				t.Fatal(err)
			}
		})
	}
}
