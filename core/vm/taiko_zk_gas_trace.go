//go:build zkgastrace

package vm

// CHANGE(taiko): zk gas trace exporter for the shared Unzen vector corpus (taiko-mono
// testdata/zk_gas_vectors/README.md, "Trace schema v1"). Test tooling: it compiles only with the
// zkgastrace build tag, which turns zkGasTraceEnabled on; without the tag every hook call in the
// production files sits behind a false constant and is compiled out.
//
// A trace pairs the state each interpreter step starts from with the zk gas charges the meter
// makes. zkGasTraceStepPre reads the state before the step runs, between the opcode fetch and
// the stack validation, where no gas has moved and nothing has been warmed yet; every read is
// side-effect free for metering (it may fill caches, never the access list or the journal). The
// charge hooks in the step tracker, the precompile charge and the per-tx meter methods pair each
// charge with that state, by call depth.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// zkGasTraceEnabled turns the trace hooks in the production files on.
const zkGasTraceEnabled = true

// Trace event kinds, as the "e" field spells them.
const (
	zkTraceIntrinsic  = "intrinsic"
	zkTraceStep       = "step"
	zkTracePrecompile = "precompile"
	zkTraceTxEnd      = "tx_end"
	zkTraceFrameEnd   = "frame_end"
)

// zkGasTraceInputs is revm-bytecode 43.0.0's OpCode::inputs() for every opcode, 0 for opcodes
// revm does not define (taiko-mono testdata/zk_gas_vectors/README.md, "Stack inputs"). A step
// records the top min(stack length, inputs) stack items.
var zkGasTraceInputs = [256]uint8{
	ADD: 2, MUL: 2, SUB: 2, DIV: 2, SDIV: 2, MOD: 2, SMOD: 2, ADDMOD: 3, MULMOD: 3, EXP: 2, SIGNEXTEND: 2,
	LT: 2, GT: 2, SLT: 2, SGT: 2, EQ: 2, ISZERO: 1, AND: 2, OR: 2, XOR: 2, NOT: 1, BYTE: 2, SHL: 2, SHR: 2, SAR: 2, CLZ: 1,
	KECCAK256: 2, BALANCE: 1, CALLDATALOAD: 1, CALLDATACOPY: 3, CODECOPY: 3, EXTCODESIZE: 1, EXTCODECOPY: 4,
	RETURNDATACOPY: 3, EXTCODEHASH: 1, BLOCKHASH: 1, BLOBHASH: 1,
	POP: 1, MLOAD: 1, MSTORE: 2, MSTORE8: 2, SLOAD: 1, SSTORE: 2, JUMP: 1, JUMPI: 2, TLOAD: 1, TSTORE: 2, MCOPY: 3,
	DUP1: 1, DUP2: 2, DUP3: 3, DUP4: 4, DUP5: 5, DUP6: 6, DUP7: 7, DUP8: 8,
	DUP9: 9, DUP10: 10, DUP11: 11, DUP12: 12, DUP13: 13, DUP14: 14, DUP15: 15, DUP16: 16,
	SWAP1: 2, SWAP2: 3, SWAP3: 4, SWAP4: 5, SWAP5: 6, SWAP6: 7, SWAP7: 8, SWAP8: 9,
	SWAP9: 10, SWAP10: 11, SWAP11: 12, SWAP12: 13, SWAP13: 14, SWAP14: 15, SWAP15: 16, SWAP16: 17,
	LOG0: 2, LOG1: 3, LOG2: 4, LOG3: 5, LOG4: 6,
	CREATE: 3, CALL: 7, CALLCODE: 7, RETURN: 2, DELEGATECALL: 6, CREATE2: 4, STATICCALL: 6, REVERT: 2, SELFDESTRUCT: 1,
}

// ZkGasTraceEvent is one line of a zk gas trace. Kind selects which fields the line has; the
// others are zero.
type ZkGasTraceEvent struct {
	// Kind is the event: intrinsic, step, precompile, tx_end or frame_end.
	Kind string

	// Step: the state the step starts from.
	Depth    int              // call depth; a tx's top frame is 1
	PC       uint64           // program counter
	Op       byte             // opcode
	Gas      uint64           // the frame's remaining gas
	Static   bool             // whether the frame is static
	StackLen int              // stack length
	Stack    []uint256.Int    // the top min(StackLen, inputs(Op)) items, top first
	Mem      uint64           // memory size in bytes
	Facts    []ZkGasTraceFact // the opcode's state facts, in schema order

	// Step: the charge.
	GasAfter uint64 // the gas fed to the meter; only for reference when Spawn is set
	Spawn    bool   // the step opened a frame and was charged its spawn estimate
	Charged  bool   // the meter was called for the step

	// Intrinsic, step and precompile: the zk gas charged, or refused when Ok is false.
	Zk uint64
	Ok bool

	// Precompile.
	Addr     common.Address // the call's bytecode (code) address
	Supplied uint64         // the gas it was given
	Left     uint64         // the gas it had left; 0 when it halted
	Success  bool           // it returned without halting

	// Tx_end and frame_end.
	Index   int     // tx_end: the input index of the tx
	Status  string  // tx_end: committed, aborted or skipped; frame_end: the vector status
	TxZk    *uint64 // tx_end: a committed tx's total, intrinsic included
	FrameZk *uint64 // frame_end: the frame's total; nil for zk_limit
}

// ZkGasTraceFact is one entry of a step's facts: its key and its value as JSON (true, false,
// null or a quoted canonical hex quantity).
type ZkGasTraceFact struct {
	Key   string
	Value string
}

// zkGasTraceFactKeys lists the facts keys of the opcodes that record facts, in schema order.
var zkGasTraceFactKeys = map[OpCode][]string{
	JUMP:           {"jumpdest_valid"},
	JUMPI:          {"jumpdest_valid"},
	RETURNDATACOPY: {"returndata_size"},
	SLOAD:          {"slot_warm"},
	SSTORE:         {"slot_warm", "original", "current"},
	BALANCE:        {"account_warm"},
	EXTCODESIZE:    {"account_warm"},
	EXTCODEHASH:    {"account_warm"},
	EXTCODECOPY:    {"account_warm"},
	CALL:           {"account_warm", "account_empty", "delegate_warm"},
	CALLCODE:       {"account_warm", "account_empty", "delegate_warm"},
	DELEGATECALL:   {"account_warm", "account_empty", "delegate_warm"},
	STATICCALL:     {"account_warm", "account_empty", "delegate_warm"},
	SELFDESTRUCT:   {"account_warm", "account_empty", "self_balance"},
}

// zkHex renders v as canonical 0x hex.
func zkHex(v uint64) string { return "0x" + strconv.FormatUint(v, 16) }

// zkBool renders b as a JSON boolean.
func zkBool(b bool) string { return strconv.FormatBool(b) }

// zkQuote renders a hex quantity as a JSON string.
func zkQuote(hex string) string { return `"` + hex + `"` }

// String renders e as a trace line: one JSON object, fields in schema order, without a newline.
func (e ZkGasTraceEvent) String() string {
	var b strings.Builder
	switch e.Kind {
	case zkTraceIntrinsic:
		fmt.Fprintf(&b, `{"e":"intrinsic","zk":"%s","ok":%t}`, zkHex(e.Zk), e.Ok)
	case zkTraceStep:
		stack := make([]string, len(e.Stack))
		for i := range e.Stack {
			stack[i] = zkQuote(e.Stack[i].Hex())
		}
		facts := make([]string, len(e.Facts))
		for i, f := range e.Facts {
			facts[i] = zkQuote(f.Key) + ":" + f.Value
		}
		fmt.Fprintf(&b, `{"e":"step","d":%d,"pc":%d,"op":%d,"gas":"%s","static":%t,"sl":%d,"stack":[%s],"mem":"%s","facts":{%s},"gas_after":"%s","spawn":%t,"zk":"%s","ok":%t,"charged":%t}`,
			e.Depth, e.PC, e.Op, zkHex(e.Gas), e.Static, e.StackLen, strings.Join(stack, ","), zkHex(e.Mem),
			strings.Join(facts, ","), zkHex(e.GasAfter), e.Spawn, zkHex(e.Zk), e.Ok, e.Charged)
	case zkTracePrecompile:
		fmt.Fprintf(&b, `{"e":"precompile","addr":"0x%x","supplied":"%s","left":"%s","success":%t,"zk":"%s","ok":%t}`,
			e.Addr[:], zkHex(e.Supplied), zkHex(e.Left), e.Success, zkHex(e.Zk), e.Ok)
	case zkTraceTxEnd:
		fmt.Fprintf(&b, `{"e":"tx_end","index":%d,"status":"%s","tx_zk":%s}`, e.Index, e.Status, zkOptionalHex(e.TxZk))
	case zkTraceFrameEnd:
		fmt.Fprintf(&b, `{"e":"frame_end","status":"%s","frame_zk":%s}`, e.Status, zkOptionalHex(e.FrameZk))
	default:
		fmt.Fprintf(&b, `{"e":%q}`, e.Kind)
	}
	return b.String()
}

// zkOptionalHex renders v as a quoted hex quantity, or null.
func zkOptionalHex(v *uint64) string {
	if v == nil {
		return "null"
	}
	return zkQuote(zkHex(*v))
}

// RenderZkGasTrace renders events as a trace file: one line each, every line ending with a newline.
func RenderZkGasTrace(events []ZkGasTraceEvent) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString(e.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// zkTraceFields lists each event kind's fields in schema order.
var zkTraceFields = map[string][]string{
	zkTraceIntrinsic:  {"e", "zk", "ok"},
	zkTraceStep:       {"e", "d", "pc", "op", "gas", "static", "sl", "stack", "mem", "facts", "gas_after", "spawn", "zk", "ok", "charged"},
	zkTracePrecompile: {"e", "addr", "supplied", "left", "success", "zk", "ok"},
	zkTraceTxEnd:      {"e", "index", "status", "tx_zk"},
	zkTraceFrameEnd:   {"e", "status", "frame_zk"},
}

var (
	zkCanonicalHex = regexp.MustCompile(`^0x(0|[1-9a-f][0-9a-f]*)$`)
	zkJSONInteger  = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	zkLowerAddress = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
)

// zkTraceObject is a JSON object read in key order.
type zkTraceObject struct {
	keys   []string
	values map[string]json.RawMessage
}

// readZkTraceObject reads one JSON object from data, keeping its key order; duplicate keys and
// trailing data are errors.
func readZkTraceObject(data []byte) (zkTraceObject, error) {
	obj := zkTraceObject{values: make(map[string]json.RawMessage)}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return obj, fmt.Errorf("not a JSON object: %s", data)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return obj, err
		}
		key := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return obj, err
		}
		if _, dup := obj.values[key]; dup {
			return obj, fmt.Errorf("duplicate key %q", key)
		}
		obj.keys = append(obj.keys, key)
		obj.values[key] = raw
	}
	if _, err := dec.Token(); err != nil {
		return obj, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return obj, errors.New("trailing data after the JSON object")
	}
	return obj, nil
}

// zkTraceDecoder decodes the fields of one trace line, keeping the first error.
type zkTraceDecoder struct {
	obj zkTraceObject
	err error
}

func (d *zkTraceDecoder) fail(key, format string, args ...any) {
	if d.err == nil {
		d.err = fmt.Errorf("%s: %s", key, fmt.Sprintf(format, args...))
	}
}

// str decodes a JSON string.
func (d *zkTraceDecoder) str(key string) string {
	var s string
	if err := json.Unmarshal(d.obj.values[key], &s); err != nil {
		d.fail(key, "not a string: %s", d.obj.values[key])
	}
	return s
}

// boolean decodes a JSON boolean.
func (d *zkTraceDecoder) boolean(key string) bool {
	switch string(d.obj.values[key]) {
	case "true":
		return true
	case "false":
		return false
	}
	d.fail(key, "not a boolean: %s", d.obj.values[key])
	return false
}

// integer decodes a non-negative JSON integer (not a string).
func (d *zkTraceDecoder) integer(key string) uint64 {
	raw := string(d.obj.values[key])
	v, err := strconv.ParseUint(raw, 10, 64)
	if !zkJSONInteger.MatchString(raw) || err != nil {
		d.fail(key, "not a JSON integer: %s", raw)
	}
	return v
}

// hexU256 decodes a canonical hex quantity of at most 256 bits from raw.
func (d *zkTraceDecoder) hexU256(key string, raw json.RawMessage) uint256.Int {
	var s string
	var v uint256.Int
	if err := json.Unmarshal(raw, &s); err != nil || !zkCanonicalHex.MatchString(s) || len(s) > 66 {
		d.fail(key, "not a canonical hex quantity: %s", raw)
		return v
	}
	if err := v.SetFromHex(s); err != nil {
		d.fail(key, "%v", err)
	}
	return v
}

// hex decodes a canonical hex quantity that fits in 64 bits.
func (d *zkTraceDecoder) hex(key string) uint64 {
	v := d.hexU256(key, d.obj.values[key])
	if !v.IsUint64() {
		d.fail(key, "%s does not fit in 64 bits", v.Hex())
	}
	return v.Uint64()
}

// optionalHex decodes null or a canonical hex quantity.
func (d *zkTraceDecoder) optionalHex(key string) *uint64 {
	if string(d.obj.values[key]) == "null" {
		return nil
	}
	v := d.hex(key)
	return &v
}

// ParseZkGasTrace parses a trace file: one event per line, each line ending with a newline, every
// event with exactly its schema fields in schema order and canonical encodings.
func ParseZkGasTrace(data []byte) ([]ZkGasTraceEvent, error) {
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, errors.New("the trace does not end with a newline")
	}
	if len(data) == 0 {
		return nil, nil
	}
	var events []ZkGasTraceEvent
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		e, err := parseZkGasTraceLine([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i, err)
		}
		events = append(events, e)
	}
	return events, nil
}

// parseZkGasTraceLine parses one trace line.
func parseZkGasTraceLine(line []byte) (ZkGasTraceEvent, error) {
	obj, err := readZkTraceObject(line)
	if err != nil {
		return ZkGasTraceEvent{}, err
	}
	d := &zkTraceDecoder{obj: obj}
	e := ZkGasTraceEvent{Kind: d.str("e")}
	fields, ok := zkTraceFields[e.Kind]
	if d.err != nil || !ok {
		return e, fmt.Errorf("unknown event %s", obj.values["e"])
	}
	if !slices.Equal(obj.keys, fields) {
		return e, fmt.Errorf("%s fields %v, want %v", e.Kind, obj.keys, fields)
	}
	switch e.Kind {
	case zkTraceIntrinsic:
		e.Zk, e.Ok = d.hex("zk"), d.boolean("ok")
	case zkTraceStep:
		op := d.integer("op")
		if op > math.MaxUint8 {
			d.fail("op", "not an opcode: %d", op)
		}
		e.Depth, e.PC, e.Op = int(d.integer("d")), d.integer("pc"), byte(op)
		e.Gas, e.Static, e.StackLen = d.hex("gas"), d.boolean("static"), int(d.integer("sl"))
		var stack []json.RawMessage
		if err := json.Unmarshal(obj.values["stack"], &stack); err != nil {
			d.fail("stack", "not an array")
		}
		for _, item := range stack {
			e.Stack = append(e.Stack, d.hexU256("stack", item))
		}
		e.Mem = d.hex("mem")
		e.Facts = d.facts(OpCode(e.Op), e.StackLen < int(zkGasTraceInputs[e.Op]))
		e.GasAfter, e.Spawn = d.hex("gas_after"), d.boolean("spawn")
		e.Zk, e.Ok, e.Charged = d.hex("zk"), d.boolean("ok"), d.boolean("charged")
	case zkTracePrecompile:
		if addr := d.str("addr"); zkLowerAddress.MatchString(addr) {
			e.Addr = common.HexToAddress(addr)
		} else {
			d.fail("addr", "not a lowercase 20-byte address: %q", addr)
		}
		e.Supplied, e.Left, e.Success = d.hex("supplied"), d.hex("left"), d.boolean("success")
		e.Zk, e.Ok = d.hex("zk"), d.boolean("ok")
	case zkTraceTxEnd:
		e.Index, e.Status, e.TxZk = int(d.integer("index")), d.str("status"), d.optionalHex("tx_zk")
	case zkTraceFrameEnd:
		e.Status, e.FrameZk = d.str("status"), d.optionalHex("frame_zk")
	}
	return e, d.err
}

// zkGasTraceHexFacts are the facts whose values are hex quantities; delegate_warm is a boolean or
// null, and every other fact a boolean.
var zkGasTraceHexFacts = map[string]bool{"returndata_size": true, "original": true, "current": true, "self_balance": true}

// facts decodes the facts object of a step with opcode op: empty on stack underflow, else
// exactly the opcode's keys in order, each with a value of its type.
func (d *zkTraceDecoder) facts(op OpCode, underflow bool) []ZkGasTraceFact {
	obj, err := readZkTraceObject(d.obj.values["facts"])
	if err != nil {
		d.fail("facts", "%v", err)
		return nil
	}
	want := zkGasTraceFactKeys[op]
	if underflow {
		want = nil
	}
	if len(obj.keys) != len(want) || !slices.Equal(obj.keys, want) {
		d.fail("facts", "keys %v, want %v", obj.keys, want)
		return nil
	}
	var facts []ZkGasTraceFact
	for _, key := range obj.keys {
		raw := obj.values[key]
		value := string(raw)
		switch {
		case zkGasTraceHexFacts[key]:
			v := d.hexU256("facts."+key, raw)
			value = zkQuote(v.Hex())
		case value == "true" || value == "false":
		case value == "null" && key == "delegate_warm":
		default:
			d.fail("facts."+key, "not a boolean: %s", raw)
		}
		facts = append(facts, ZkGasTraceFact{Key: key, Value: value})
	}
	return facts
}

// ReadZkGasTraceFile reads and parses <dir>/traces/<id>.jsonl.
func ReadZkGasTraceFile(dir, id string) ([]ZkGasTraceEvent, error) {
	path := filepath.Join(dir, "traces", id+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reference trace: %w", err)
	}
	events, err := ParseZkGasTrace(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return events, nil
}

// CompareZkGasTrace compares the geth trace got with the reference trace want event by event and
// reports the first difference with its index in the reference (lines counted from 0, and the
// geth index too when the normalization below shifted them apart). Every field must be equal,
// except a step's gas_after when the step spawned on both sides, which is only for reference.
// A difference in a step's pre-state (d, pc, op, gas, static, sl, stack, mem, facts) is a
// "setup divergence"; any other difference (a step's gas_after, spawn, zk, ok or charged, any
// field of another event, or a missing, extra or reordered event) is a "zk gas mismatch". An
// event that one trace has and the other lacks, with the next event equal again, is reported as
// such rather than as a difference between two unrelated events.
//
// One normalization applies to both traces first: a skipped tx carries no zk gas, and a client
// may reject it either before or after charging its intrinsic zk gas, which is then discarded.
// So a tx whose tx_end is skipped and whose only other event is an accepted intrinsic charge is
// reduced to its lone tx_end. Nothing else is normalized: a refused charge or a step inside a
// skipped tx is still compared.
func CompareZkGasTrace(want, got []ZkGasTraceEvent) error {
	wantN, wantAt := normalizeZkGasTrace(want)
	gotN, gotAt := normalizeZkGasTrace(got)
	for i := 0; i < max(len(wantN), len(gotN)); i++ {
		// The event's index in each trace as given.
		at := fmt.Sprintf("event %d", wantAt[i])
		if wantAt[i] != gotAt[i] {
			at += fmt.Sprintf(" (geth event %d)", gotAt[i])
		}
		switch {
		case i >= len(gotN):
			return fmt.Errorf("zk gas mismatch at %s: reference %s, geth trace ends (%d reference events, %d geth events)", at, wantN[i], len(want), len(got))
		case i >= len(wantN):
			return fmt.Errorf("zk gas mismatch at %s: reference trace ends, geth %s (%d reference events, %d geth events)", at, gotN[i], len(want), len(got))
		}
		label := zkTraceEventDifference(wantN[i], gotN[i])
		switch {
		case label == "":
			continue
		case i+1 < len(wantN) && zkTraceEventDifference(wantN[i+1], gotN[i]) == "":
			return fmt.Errorf("zk gas mismatch at %s: the reference has an event the geth trace lacks: %s", at, wantN[i])
		case i+1 < len(gotN) && zkTraceEventDifference(wantN[i], gotN[i+1]) == "":
			return fmt.Errorf("zk gas mismatch at %s: the geth trace has an event the reference lacks: %s", at, gotN[i])
		}
		return fmt.Errorf("%s at %s: reference %s, geth %s", label, at, wantN[i], gotN[i])
	}
	return nil
}

// zkTraceEventDifference classifies how a differs from b: "" when they match.
func zkTraceEventDifference(a, b ZkGasTraceEvent) string {
	if a.Kind != b.Kind {
		return "zk gas mismatch"
	}
	if a.Kind == zkTraceStep {
		pre := func(e ZkGasTraceEvent) string {
			e.GasAfter, e.Spawn, e.Zk, e.Ok, e.Charged = 0, false, 0, false, false
			return e.String()
		}
		if pre(a) != pre(b) {
			return "setup divergence"
		}
		if a.Spawn && b.Spawn {
			a.GasAfter, b.GasAfter = 0, 0
		}
	}
	if a.String() != b.String() {
		return "zk gas mismatch"
	}
	return ""
}

// normalizeZkGasTrace drops the intrinsic event of every skipped tx whose only other event it is
// (see CompareZkGasTrace). It also returns each kept event's index in events, plus a final entry
// len(events) for the end of the trace.
func normalizeZkGasTrace(events []ZkGasTraceEvent) ([]ZkGasTraceEvent, []int) {
	out := make([]ZkGasTraceEvent, 0, len(events))
	var at []int
	for i, e := range events {
		if e.Kind == zkTraceIntrinsic && e.Ok && i+1 < len(events) &&
			events[i+1].Kind == zkTraceTxEnd && events[i+1].Status == "skipped" {
			continue
		}
		out = append(out, e)
		at = append(at, i)
	}
	return out, append(at, len(events))
}

// zkGasTraceRecordKind is the kind of one meter record.
type zkGasTraceRecordKind int

const (
	zkGasTraceRecordEvent  zkGasTraceRecordKind = iota // an intrinsic, step or precompile event
	zkGasTraceRecordReset                              // ResetTransaction
	zkGasTraceRecordCommit                             // a successful CommitTransaction
)

// zkGasTraceRecord is one entry of a meter's record.
type zkGasTraceRecord struct {
	kind  zkGasTraceRecordKind
	event ZkGasTraceEvent // zkGasTraceRecordEvent
	txZk  uint64          // zkGasTraceRecordCommit: the committed tx total
}

// zkGasTraceOpen is a step whose pre-state was read and whose charge has not been seen yet.
type zkGasTraceOpen struct {
	event    ZkGasTraceEvent
	contract *Contract
}

// zkGasTraceRecorder records the events of one meter.
type zkGasTraceRecorder struct {
	meter   *ZkGasMeter
	records []zkGasTraceRecord
	open    []zkGasTraceOpen // in execution order
	lastTx  uint64           // the meter's in-flight tx total after the last recorded event
	fresh   bool             // no event recorded yet, so lastTx is not known to be a baseline
	errs    []error
}

// ZkGasTrace records the zk gas trace of every meter that reports an event while it runs. At
// most one trace runs at a time; the hooks only read the running trace, so tests that use it must
// not run in parallel with other EVM executions.
type ZkGasTrace struct {
	recorders []*zkGasTraceRecorder // in the order their meters first reported
	byMeter   map[*ZkGasMeter]*zkGasTraceRecorder
}

// zkGasTraceRunning is the running trace, if any.
var zkGasTraceRunning atomic.Pointer[ZkGasTrace]

// zkGasTraceTestBeforePre, when set, runs right before a step's pre-state is read. Tests use it
// to show that a reader with side effects is caught.
var zkGasTraceTestBeforePre func(evm *EVM, contract *Contract, op OpCode)

// StartZkGasTrace starts recording. It panics when a trace is already running.
func StartZkGasTrace() *ZkGasTrace {
	t := &ZkGasTrace{byMeter: make(map[*ZkGasMeter]*zkGasTraceRecorder)}
	if !zkGasTraceRunning.CompareAndSwap(nil, t) {
		panic("a zk gas trace is already running")
	}
	return t
}

// Stop stops recording and closes the steps still open as steps the meter never charged. It may
// be called more than once.
func (t *ZkGasTrace) Stop() {
	if zkGasTraceRunning.CompareAndSwap(t, nil) {
		for _, r := range t.recorders {
			r.closeOpen(0)
		}
	}
}

// zkGasTraceRecorderOf returns the running trace's recorder of meter m, creating it on m's first
// event; nil when no trace runs.
func zkGasTraceRecorderOf(m *ZkGasMeter) *zkGasTraceRecorder {
	t := zkGasTraceRunning.Load()
	if t == nil || m == nil {
		return nil
	}
	r := t.byMeter[m]
	if r == nil {
		r = &zkGasTraceRecorder{meter: m, lastTx: m.txZkGasUsed, fresh: true}
		t.byMeter[m] = r
		t.recorders = append(t.recorders, r)
	}
	return r
}

// closeOpen records every open step at depth >= depth, in execution order, as a step the meter
// never charged.
func (r *zkGasTraceRecorder) closeOpen(depth int) {
	kept := r.open[:0]
	for _, o := range r.open {
		if o.event.Depth < depth {
			kept = append(kept, o)
			continue
		}
		e := o.event
		e.GasAfter, e.Spawn, e.Zk, e.Ok, e.Charged = e.Gas, false, 0, true, false
		r.records = append(r.records, zkGasTraceRecord{kind: zkGasTraceRecordEvent, event: e})
	}
	r.open = kept
}

// take removes and returns the open step of a charge at depth for opcode, which started with
// gasBefore. Deeper open steps are closed first: their frames have ended.
func (r *zkGasTraceRecorder) take(depth int, opcode byte, gasBefore uint64) (zkGasTraceOpen, bool) {
	r.closeOpen(depth + 1)
	if n := len(r.open); n > 0 {
		o := r.open[n-1]
		if o.event.Depth == depth && o.event.Op == opcode && o.event.Gas == gasBefore {
			r.open = r.open[:n-1]
			return o, true
		}
	}
	r.errs = append(r.errs, fmt.Errorf("a charge at depth %d for opcode %#x with gas %d has no recorded pre-state", depth, opcode, gasBefore))
	return zkGasTraceOpen{}, false
}

// charged returns the zk gas of a charge of base * multiplier that ended with err: the change in
// the meter's in-flight total when it succeeded, and the amount it tried to charge (saturated at
// 2^64 - 1, as alethia-reth records it) when it was refused. It checks that the change is the
// product, so that no charge escapes the hooks.
func (r *zkGasTraceRecorder) charged(base, multiplier uint64, err error) (uint64, bool) {
	cost, overflow := safeMul(base, multiplier)
	if overflow {
		cost = math.MaxUint64
	}
	now := r.meter.txZkGasUsed
	if r.fresh && err == nil {
		// The meter's first event is this charge, so the recorder was created after it.
		r.lastTx = now - cost
	}
	r.fresh = false
	delta := now - r.lastTx
	r.lastTx = now
	switch {
	case err == nil && delta != cost:
		r.errs = append(r.errs, fmt.Errorf("a charge of %d * %d moved the tx zk gas by %d", base, multiplier, delta))
	case err != nil && delta != 0:
		r.errs = append(r.errs, fmt.Errorf("a refused charge of %d * %d moved the tx zk gas by %d", base, multiplier, delta))
	}
	return cost, err == nil
}

// zkGasTraceStepPre reads the state step op at pc of contract starts from, with gasBefore gas,
// and opens the step. It runs in the interpreter loop after the opcode fetch and before the stack
// validation.
func zkGasTraceStepPre(evm *EVM, contract *Contract, stack *Stack, mem *Memory, pc uint64, op OpCode, gasBefore uint64) {
	if evm.zkGasTracker == nil {
		return
	}
	r := zkGasTraceRecorderOf(evm.zkGasTracker.meter)
	if r == nil {
		return
	}
	if zkGasTraceTestBeforePre != nil {
		zkGasTraceTestBeforePre(evm, contract, op)
	}
	r.fresh = false
	// A new step at this depth ends the open steps of this depth and of the deeper frames.
	r.closeOpen(evm.depth)
	inputs := int(zkGasTraceInputs[op])
	e := ZkGasTraceEvent{
		Kind:     zkTraceStep,
		Depth:    evm.depth,
		PC:       pc,
		Op:       byte(op),
		Gas:      gasBefore,
		Static:   evm.readOnly,
		StackLen: stack.len(),
		Mem:      uint64(mem.Len()),
	}
	for i := 0; i < min(e.StackLen, inputs); i++ {
		e.Stack = append(e.Stack, *stack.Back(i))
	}
	if e.StackLen >= inputs {
		e.Facts = zkGasTraceFacts(evm, contract, stack, op)
	}
	r.open = append(r.open, zkGasTraceOpen{event: e, contract: contract})
}

// zkGasTraceFacts reads the facts of opcode op, whose stack holds its inputs, without side
// effects for metering. It follows alethia-reth's definitions: every fact is the value before
// the step runs.
func zkGasTraceFacts(evm *EVM, contract *Contract, stack *Stack, op OpCode) []ZkGasTraceFact {
	db := evm.StateDB
	keys := zkGasTraceFactKeys[op]
	var values []string
	account := func(word *uint256.Int) common.Address { return common.Address(word.Bytes20()) }
	switch op {
	case JUMP, JUMPI:
		values = []string{zkBool(contract.validJumpdest(stack.Back(0)))}
	case RETURNDATACOPY:
		values = []string{zkQuote(zkHex(uint64(len(evm.returnData))))}
	case SLOAD, SSTORE:
		slot := common.Hash(stack.Back(0).Bytes32())
		_, warm := db.SlotInAccessList(contract.Address(), slot)
		values = []string{zkBool(warm)}
		if op == SSTORE {
			current, original := db.GetStateAndCommittedState(contract.Address(), slot)
			values = append(values, zkQuote(new(uint256.Int).SetBytes32(original[:]).Hex()), zkQuote(new(uint256.Int).SetBytes32(current[:]).Hex()))
		}
	case BALANCE, EXTCODESIZE, EXTCODEHASH, EXTCODECOPY:
		values = []string{zkBool(db.AddressInAccessList(account(stack.Back(0))))}
	case CALL, CALLCODE, DELEGATECALL, STATICCALL:
		target := account(stack.Back(1))
		delegate := "null"
		if to, ok := types.ParseDelegation(db.GetCode(target)); ok {
			delegate = zkBool(db.AddressInAccessList(to))
		}
		values = []string{zkBool(db.AddressInAccessList(target)), zkBool(db.Empty(target)), delegate}
	case SELFDESTRUCT:
		beneficiary := account(stack.Back(0))
		values = []string{zkBool(db.AddressInAccessList(beneficiary)), zkBool(db.Empty(beneficiary)), zkQuote(db.GetBalance(contract.Address()).Hex())}
	default:
		return nil
	}
	facts := make([]ZkGasTraceFact, len(keys))
	for i, key := range keys {
		facts[i] = ZkGasTraceFact{Key: key, Value: values[i]}
	}
	return facts
}

// zkGasTraceSpawnCharged records the spawn charge of step at depth, made before its child frame
// or precompile runs, which ended with err. gas_after is the frame's gas at that point (after the
// gas forwarded to the child), for reference only.
func zkGasTraceSpawnCharged(m *ZkGasMeter, depth int, step *zkGasPendingStep, err error) {
	r := zkGasTraceRecorderOf(m)
	if r == nil {
		return
	}
	o, ok := r.take(depth, step.opcode, step.gasBefore)
	if !ok {
		return
	}
	e := o.event
	e.GasAfter, e.Spawn, e.Charged = o.contract.Gas, true, true
	e.Zk, e.Ok = r.charged(m.SpawnEstimate(step.opcode), uint64(m.schedule.OpcodeMultipliers[step.opcode]), err)
	r.records = append(r.records, zkGasTraceRecord{kind: zkGasTraceRecordEvent, event: e})
}

// zkGasTraceStepCharged records the charge of step at depth made after it ran: rawGas, its
// spawn estimate when it spawned without reaching a child, else the gas it used (gasAfter fed
// to the meter); err is the meter's verdict.
func zkGasTraceStepCharged(m *ZkGasMeter, depth int, step *zkGasPendingStep, gasAfter, rawGas uint64, err error) {
	r := zkGasTraceRecorderOf(m)
	if r == nil {
		return
	}
	o, ok := r.take(depth, step.opcode, step.gasBefore)
	if !ok {
		return
	}
	e := o.event
	e.GasAfter, e.Spawn, e.Charged = gasAfter, IsSpawnOpcode(OpCode(step.opcode)) && step.spawned, true
	e.Zk, e.Ok = r.charged(rawGas, uint64(m.schedule.OpcodeMultipliers[step.opcode]), err)
	r.records = append(r.records, zkGasTraceRecord{kind: zkGasTraceRecordEvent, event: e})
}

// zkGasTracePrecompileCharged records the charge of the precompile at addr, which was supplied
// gas, had left gas left and returned runErr; err is the meter's verdict. A precompile that
// halted (any runErr but a revert) leaves no gas: the call that ran it zeroes the frame gas.
func zkGasTracePrecompileCharged(m *ZkGasMeter, addr common.Address, supplied, left uint64, runErr, err error) {
	r := zkGasTraceRecorderOf(m)
	if r == nil {
		return
	}
	success := runErr == nil || runErr == ErrExecutionReverted
	if !success {
		left = 0
	}
	e := ZkGasTraceEvent{Kind: zkTracePrecompile, Addr: addr, Supplied: supplied, Left: left, Success: success}
	e.Zk, e.Ok = r.charged(precompileZkGasUsed(supplied, left, runErr), uint64(m.schedule.PrecompileMultiplier(addr)), err)
	r.records = append(r.records, zkGasTraceRecord{kind: zkGasTraceRecordEvent, event: e})
}

// zkGasTraceIntrinsicCharged records the per-tx intrinsic charge, which ended with err.
func zkGasTraceIntrinsicCharged(m *ZkGasMeter, err error) {
	r := zkGasTraceRecorderOf(m)
	if r == nil {
		return
	}
	r.closeOpen(0)
	e := ZkGasTraceEvent{Kind: zkTraceIntrinsic}
	e.Zk, e.Ok = r.charged(m.schedule.TxIntrinsicZkGas, 1, err)
	r.records = append(r.records, zkGasTraceRecord{kind: zkGasTraceRecordEvent, event: e})
}

// zkGasTraceCommitted records a successful CommitTransaction, before the in-flight total is
// cleared.
func zkGasTraceCommitted(m *ZkGasMeter) {
	r := zkGasTraceRecorderOf(m)
	if r == nil {
		return
	}
	r.closeOpen(0)
	if m.txZkGasUsed != r.lastTx {
		r.errs = append(r.errs, fmt.Errorf("committed %d tx zk gas, the charges add up to %d", m.txZkGasUsed, r.lastTx))
	}
	r.records = append(r.records, zkGasTraceRecord{kind: zkGasTraceRecordCommit, txZk: m.txZkGasUsed})
	r.lastTx, r.fresh = 0, false
}

// zkGasTraceReset records a ResetTransaction.
func zkGasTraceReset(m *ZkGasMeter) {
	r := zkGasTraceRecorderOf(m)
	if r == nil {
		return
	}
	r.closeOpen(0)
	r.records = append(r.records, zkGasTraceRecord{kind: zkGasTraceRecordReset})
	r.lastTx, r.fresh = 0, false
}

// only returns the recorder of the trace's single meter. A run that charged nothing and ran no
// step leaves no recorder; with allowNone that is an empty recorder.
func (t *ZkGasTrace) only(allowNone bool) (*zkGasTraceRecorder, error) {
	if zkGasTraceRunning.Load() == t {
		return nil, errors.New("the trace is still running")
	}
	if allowNone && len(t.recorders) == 0 {
		return &zkGasTraceRecorder{}, nil
	}
	if len(t.recorders) != 1 {
		return nil, fmt.Errorf("the trace recorded %d meters, want 1", len(t.recorders))
	}
	r := t.recorders[0]
	if err := errors.Join(r.errs...); err != nil {
		return nil, err
	}
	return r, nil
}

// HarnessEvents returns the trace of a harness run, which ended with status and the frame zk
// gas frameZk (nil for zk_limit): its step and precompile events and the frame_end.
func (t *ZkGasTrace) HarnessEvents(status string, frameZk *uint64) ([]ZkGasTraceEvent, error) {
	r, err := t.only(true)
	if err != nil {
		return nil, err
	}
	var events []ZkGasTraceEvent
	for _, rec := range r.records {
		if rec.kind != zkGasTraceRecordEvent || rec.event.Kind == zkTraceIntrinsic {
			return nil, fmt.Errorf("a harness run recorded a per-tx meter event (%+v)", rec)
		}
		events = append(events, rec.event)
	}
	return append(events, ZkGasTraceEvent{Kind: zkTraceFrameEnd, Status: status, FrameZk: frameZk}), nil
}

// BlockEvents returns the trace of a sealed block built from txs input txs: for each tx, in
// order, its intrinsic charge, its step and precompile events and its tx_end, up to the tx that
// exceeded the block zk gas limit or the last one. Each tx's events start at the meter reset
// before it; a tx that ends with a commit is committed, one with a refused charge is aborted
// (the block ends there), and any other is skipped.
func (t *ZkGasTrace) BlockEvents(txs int) ([]ZkGasTraceEvent, error) {
	r, err := t.only(false)
	if err != nil {
		return nil, err
	}
	if len(r.records) == 0 || r.records[0].kind != zkGasTraceRecordReset {
		return nil, errors.New("a block trace starts with the meter reset before the first tx")
	}
	// Split the records at each reset: one group per tx.
	var groups [][]zkGasTraceRecord
	for _, rec := range r.records {
		if rec.kind == zkGasTraceRecordReset {
			groups = append(groups, nil)
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], rec)
	}
	var events []ZkGasTraceEvent
	for index, group := range groups {
		end := ZkGasTraceEvent{Kind: zkTraceTxEnd, Index: index, Status: "skipped"}
		if len(group) > 0 {
			if group[0].kind != zkGasTraceRecordEvent || group[0].event.Kind != zkTraceIntrinsic {
				return nil, fmt.Errorf("tx %d does not start with its intrinsic charge", index)
			}
			if last := group[len(group)-1]; last.kind == zkGasTraceRecordCommit {
				end.Status, end.TxZk = "committed", &last.txZk
				group = group[:len(group)-1]
			}
		}
		refused := false
		for j, rec := range group {
			if rec.kind != zkGasTraceRecordEvent || (j > 0 && rec.event.Kind == zkTraceIntrinsic) {
				return nil, fmt.Errorf("tx %d: unexpected meter record %+v", index, rec)
			}
			refused = refused || !rec.event.Ok
			events = append(events, rec.event)
		}
		switch {
		case refused && end.Status == "committed":
			return nil, fmt.Errorf("tx %d committed after a refused charge", index)
		case refused:
			end.Status = "aborted"
		}
		events = append(events, end)
		if end.Status == "aborted" {
			// Building stops at the aborted tx; only the reset that discards its zk gas follows.
			for _, rest := range groups[index+1:] {
				if len(rest) > 0 {
					return nil, fmt.Errorf("tx %d was aborted but the meter recorded more txs", index)
				}
			}
			if len(groups) > index+2 {
				return nil, fmt.Errorf("tx %d was aborted but the meter was reset %d more times", index, len(groups)-index-1)
			}
			return events, nil
		}
	}
	if len(groups) != txs {
		return nil, fmt.Errorf("the meter recorded %d txs, the block was built from %d", len(groups), txs)
	}
	return events, nil
}
