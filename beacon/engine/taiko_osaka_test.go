package engine

import (
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
)

// taikoPayloadV3Fields returns the all-zero wire payload: the 17 standard
// properties plus headerDifficulty.
func taikoPayloadV3Fields() map[string]json.RawMessage {
	hash := json.RawMessage(`"0x` + strings.Repeat("0", 64) + `"`)
	return map[string]json.RawMessage{
		"parentHash":       hash,
		"feeRecipient":     json.RawMessage(`"0x` + strings.Repeat("0", 40) + `"`),
		"stateRoot":        hash,
		"receiptsRoot":     hash,
		"logsBloom":        json.RawMessage(`"0x` + strings.Repeat("0", 512) + `"`),
		"prevRandao":       hash,
		"blockNumber":      json.RawMessage(`"0x0"`),
		"gasLimit":         json.RawMessage(`"0x0"`),
		"gasUsed":          json.RawMessage(`"0x0"`),
		"timestamp":        json.RawMessage(`"0x0"`),
		"extraData":        json.RawMessage(`"0x"`),
		"baseFeePerGas":    json.RawMessage(`"0x0"`),
		"blockHash":        hash,
		"transactions":     json.RawMessage(`[]`),
		"withdrawals":      json.RawMessage(`[]`),
		"blobGasUsed":      json.RawMessage(`"0x0"`),
		"excessBlobGas":    json.RawMessage(`"0x0"`),
		"headerDifficulty": json.RawMessage(`0`),
	}
}

// taikoPayloadV3JSONWith encodes the all-zero wire payload after applying edit.
func taikoPayloadV3JSONWith(t *testing.T, edit func(map[string]json.RawMessage)) string {
	t.Helper()
	fields := taikoPayloadV3Fields()
	if edit != nil {
		edit(fields)
	}
	enc, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	return string(enc)
}

func TestTaikoExecutionPayloadV3Decoding(t *testing.T) {
	set := func(key, value string) func(map[string]json.RawMessage) {
		return func(f map[string]json.RawMessage) { f[key] = json.RawMessage(value) }
	}
	remove := func(key string) func(map[string]json.RawMessage) {
		return func(f map[string]json.RawMessage) { delete(f, key) }
	}
	base := taikoPayloadV3JSONWith(t, nil)
	type testcase struct {
		name    string
		input   string
		wantErr string // empty: decoding succeeds
	}
	tests := []testcase{
		{name: "all-zero payload", input: base},
		{name: "whitespace around values", input: strings.Replace(base, `"headerDifficulty":0`, `"headerDifficulty" : 7 `, 1)},
		{name: "max u64 headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", "18446744073709551615"))},
		{name: "unknown keys inside a withdrawal are ignored", input: taikoPayloadV3JSONWith(t, set("withdrawals",
			`[{"index":"0x1","validatorIndex":"0x2","address":"0x0000000000000000000000000000000000000003","amount":"0x4","extra":null}]`))},

		// headerDifficulty: a required decimal JSON number that fits u64.
		{name: "missing headerDifficulty", input: taikoPayloadV3JSONWith(t, remove("headerDifficulty")), wantErr: `missing required field "headerDifficulty"`},
		{name: "null headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", "null")), wantErr: `field "headerDifficulty" must not be null`},
		{name: "overflowing headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", "18446744073709551616")), wantErr: "value out of range"},
		{name: "hex string headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", `"0x1"`)), wantErr: "want a decimal JSON number"},
		{name: "decimal string headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", `"1"`)), wantErr: "want a decimal JSON number"},
		{name: "negative headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", "-1")), wantErr: "want a decimal JSON number"},
		{name: "negative zero headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", "-0")), wantErr: "want a decimal JSON number"},
		{name: "fractional headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", "1.0")), wantErr: "want a decimal JSON number"},
		{name: "exponent headerDifficulty", input: taikoPayloadV3JSONWith(t, set("headerDifficulty", "1e2")), wantErr: "want a decimal JSON number"},

		// transactions: a required array that may be empty.
		{name: "missing transactions", input: taikoPayloadV3JSONWith(t, remove("transactions")), wantErr: `missing required field "transactions"`},
		{name: "null transactions", input: taikoPayloadV3JSONWith(t, set("transactions", "null")), wantErr: `field "transactions" must not be null`},
		{name: "null transaction element", input: taikoPayloadV3JSONWith(t, set("transactions", "[null]")), wantErr: `invalid field "transactions"`},
		{name: "non-array transactions", input: taikoPayloadV3JSONWith(t, set("transactions", `"0x"`)), wantErr: `invalid field "transactions"`},

		// withdrawals: a required array of complete withdrawal objects.
		{name: "missing withdrawals", input: taikoPayloadV3JSONWith(t, remove("withdrawals")), wantErr: `missing required field "withdrawals"`},
		{name: "null withdrawals", input: taikoPayloadV3JSONWith(t, set("withdrawals", "null")), wantErr: `field "withdrawals" must not be null`},
		{name: "null withdrawal element", input: taikoPayloadV3JSONWith(t, set("withdrawals", "[null]")), wantErr: "withdrawal 0: want a JSON object"},
		{name: "withdrawal without amount", input: taikoPayloadV3JSONWith(t, set("withdrawals",
			`[{"index":"0x1","validatorIndex":"0x2","address":"0x0000000000000000000000000000000000000003"}]`)), wantErr: `withdrawal 0: missing required field "amount"`},
		{name: "withdrawal with a case-variant key", input: taikoPayloadV3JSONWith(t, set("withdrawals",
			`[{"index":"0x1","validatorIndex":"0x2","address":"0x0000000000000000000000000000000000000003","Amount":"0x4"}]`)), wantErr: `withdrawal 0: missing required field "amount"`},

		// Field encodings.
		{name: "short logsBloom", input: taikoPayloadV3JSONWith(t, set("logsBloom", `"0x`+strings.Repeat("0", 510)+`"`)), wantErr: "have 255 bytes, want 256"},
		{name: "long logsBloom", input: taikoPayloadV3JSONWith(t, set("logsBloom", `"0x`+strings.Repeat("0", 514)+`"`)), wantErr: "have 257 bytes, want 256"},
		{name: "numeric quantity", input: taikoPayloadV3JSONWith(t, set("blockNumber", "1")), wantErr: `invalid field "blockNumber"`},
		{name: "quantity with leading zero", input: taikoPayloadV3JSONWith(t, set("gasLimit", `"0x01"`)), wantErr: `invalid field "gasLimit"`},
		{name: "overflowing quantity", input: taikoPayloadV3JSONWith(t, set("timestamp", `"0x10000000000000000"`)), wantErr: `invalid field "timestamp"`},
		{name: "short hash", input: taikoPayloadV3JSONWith(t, set("blockHash", `"0x00"`)), wantErr: `invalid field "blockHash"`},
		{name: "base fee above 256 bits", input: taikoPayloadV3JSONWith(t, set("baseFeePerGas", `"0x1`+strings.Repeat("0", 64)+`"`)), wantErr: `invalid field "baseFeePerGas"`},

		// Unknown keys are rejected even when null, and keys match exactly.
		{name: "unknown key with a null value", input: taikoPayloadV3JSONWith(t, set("txHash", "null")), wantErr: "unsupported Osaka payload fields: txHash"},
		{name: "unknown keys listed sorted", input: taikoPayloadV3JSONWith(t, func(f map[string]json.RawMessage) {
			f["withdrawalsHash"] = json.RawMessage(`"0x01"`)
			f["blockAccessList"] = json.RawMessage(`"0x"`)
		}), wantErr: "unsupported Osaka payload fields: blockAccessList, withdrawalsHash"},
		{name: "case-variant standard key", input: taikoPayloadV3JSONWith(t, func(f map[string]json.RawMessage) {
			f["ParentHash"] = f["parentHash"]
			delete(f, "parentHash")
		}), wantErr: `missing required field "parentHash"`},
		{name: "case-variant headerDifficulty", input: taikoPayloadV3JSONWith(t, func(f map[string]json.RawMessage) {
			f["HeaderDifficulty"] = f["headerDifficulty"]
			delete(f, "headerDifficulty")
		}), wantErr: `missing required field "headerDifficulty"`},
		{name: "duplicate key", input: strings.Replace(base, `"headerDifficulty":0`, `"headerDifficulty":0,"headerDifficulty":1`, 1), wantErr: `duplicate field "headerDifficulty"`},

		// The payload itself must be an object.
		{name: "null payload", input: "null", wantErr: "want a JSON object"},
		{name: "array payload", input: "[]", wantErr: "want a JSON object"},
	}
	// Every unsupported property the spec names is rejected on its own, as the
	// shared cross-client test does for the legacy override and Amsterdam fields.
	for key, value := range map[string]string{
		"txHash":          `"0x` + strings.Repeat("0", 63) + `1"`,
		"withdrawalsHash": `"0x` + strings.Repeat("0", 63) + `1"`,
		"taikoBlock":      "true",
		"slotNumber":      `"0x1"`,
		"blockAccessList": `"0x"`,
		"targetGasLimit":  `"0x1"`,
	} {
		tests = append(tests, testcase{
			name:    "unsupported " + key,
			input:   taikoPayloadV3JSONWith(t, set(key, value)),
			wantErr: "unsupported Osaka payload fields: " + key,
		})
	}
	// Every one of the 18 properties is required and non-null.
	for _, key := range taikoPayloadV3Keys {
		tests = append(tests,
			testcase{name: "missing " + key, input: taikoPayloadV3JSONWith(t, remove(key)), wantErr: `missing required field "` + key + `"`},
			testcase{name: "null " + key, input: taikoPayloadV3JSONWith(t, set(key, "null")), wantErr: `field "` + key + `" must not be null`},
		)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload TaikoExecutionPayloadV3
			err := json.Unmarshal([]byte(tt.input), &payload)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("decoding succeeded, want error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestTaikoExecutionPayloadV3DecodesValues(t *testing.T) {
	// headerDifficulty 73 and blob gas 5/7 are the values the shared
	// cross-client normalization test pins.
	input := taikoPayloadV3JSONWith(t, func(f map[string]json.RawMessage) {
		f["headerDifficulty"] = json.RawMessage(`73`)
		f["blobGasUsed"] = json.RawMessage(`"0x5"`)
		f["excessBlobGas"] = json.RawMessage(`"0x7"`)
		f["blockNumber"] = json.RawMessage(`"0x2a"`)
		f["baseFeePerGas"] = json.RawMessage(`"0x3b9aca00"`)
		f["extraData"] = json.RawMessage(`"0x0102"`)
		f["transactions"] = json.RawMessage(`["0x02c0"]`)
		f["withdrawals"] = json.RawMessage(`[{"index":"0x1","validatorIndex":"0x2","address":"0x0000000000000000000000000000000000000003","amount":"0x4"}]`)
	})
	var payload TaikoExecutionPayloadV3
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := TaikoExecutionPayloadV3{
		LogsBloom:        make([]byte, types.BloomByteLength),
		Number:           42,
		ExtraData:        []byte{1, 2},
		BaseFeePerGas:    big.NewInt(1_000_000_000),
		Transactions:     [][]byte{{0x02, 0xc0}},
		Withdrawals:      []*types.Withdrawal{{Index: 1, Validator: 2, Address: common.Address{19: 3}, Amount: 4}},
		BlobGasUsed:      5,
		ExcessBlobGas:    7,
		HeaderDifficulty: 73,
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("decoded payload mismatch:\nhave %+v\nwant %+v", payload, want)
	}
	// MarshalJSON produces the wire form the strict decoder accepts.
	enc, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var roundTrip TaikoExecutionPayloadV3
	if err := json.Unmarshal(enc, &roundTrip); err != nil {
		t.Fatalf("decode encoded payload %s: %v", enc, err)
	}
	if !reflect.DeepEqual(roundTrip, want) {
		t.Fatalf("round trip mismatch:\nhave %+v\nwant %+v", roundTrip, want)
	}
	if !strings.HasSuffix(string(enc), `"headerDifficulty":73}`) {
		t.Fatalf("headerDifficulty must encode as a decimal number: %s", enc)
	}
}

func TestTaikoExecutionPayloadV3ToExecutableData(t *testing.T) {
	payload := TaikoExecutionPayloadV3{
		Number:           1,
		Timestamp:        100,
		BaseFeePerGas:    big.NewInt(1),
		BlobGasUsed:      5,
		ExcessBlobGas:    7,
		HeaderDifficulty: 73,
	}
	data := payload.ToExecutableData()
	if !data.TaikoBlock {
		t.Fatal("TaikoBlock must be set")
	}
	if data.HeaderDifficulty == nil || data.HeaderDifficulty.Uint64() != 73 {
		t.Fatalf("HeaderDifficulty = %v, want 73", data.HeaderDifficulty)
	}
	if data.BlobGasUsed == nil || *data.BlobGasUsed != 5 {
		t.Fatalf("BlobGasUsed = %v, want 5", data.BlobGasUsed)
	}
	if data.ExcessBlobGas == nil || *data.ExcessBlobGas != 7 {
		t.Fatalf("ExcessBlobGas = %v, want 7", data.ExcessBlobGas)
	}
	if data.Withdrawals == nil || len(data.Withdrawals) != 0 {
		t.Fatalf("Withdrawals = %v, want a non-nil empty list", data.Withdrawals)
	}
	if data.Transactions == nil || len(data.Transactions) != 0 {
		t.Fatalf("Transactions = %v, want a non-nil empty list", data.Transactions)
	}
	// A zero headerDifficulty still restores the Unzen header fields.
	payload.HeaderDifficulty = 0
	if data := payload.ToExecutableData(); data.HeaderDifficulty == nil || data.HeaderDifficulty.Sign() != 0 {
		t.Fatalf("HeaderDifficulty = %v, want a non-nil zero", data.HeaderDifficulty)
	}
}

// taikoAttributesJSON encodes a complete attributes object after applying edit.
func taikoAttributesJSON(t *testing.T, edit func(map[string]json.RawMessage)) []byte {
	t.Helper()
	attrs := PayloadAttributes{
		Timestamp:     100,
		Withdrawals:   []*types.Withdrawal{},
		BeaconRoot:    &common.Hash{},
		BaseFeePerGas: big.NewInt(1),
		BlockMetadata: &BlockMetadata{GasLimit: 30_000_000, Timestamp: 100, TxList: []byte{}, ExtraData: []byte{}},
		L1Origin:      &rawdb.L1Origin{BlockID: big.NewInt(100)},
	}
	enc, err := json.Marshal(attrs)
	if err != nil {
		t.Fatalf("encode attributes: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(enc, &fields); err != nil {
		t.Fatalf("split attributes: %v", err)
	}
	if edit != nil {
		edit(fields)
	}
	if enc, err = json.Marshal(fields); err != nil {
		t.Fatalf("encode attributes: %v", err)
	}
	return enc
}

func TestTaikoPayloadAttributesV3Decoding(t *testing.T) {
	set := func(key, value string) func(map[string]json.RawMessage) {
		return func(f map[string]json.RawMessage) { f[key] = json.RawMessage(value) }
	}
	tests := []struct {
		name               string
		edit               func(map[string]json.RawMessage)
		wantTargetGasLimit bool
		wantAnchorTx       bool
		wantSlotNumber     bool
		wantErr            string
	}{
		{name: "neither key"},
		{name: "targetGasLimit present", edit: set("targetGasLimit", `"0x1c9c380"`), wantTargetGasLimit: true},
		{name: "targetGasLimit null", edit: set("targetGasLimit", "null")},
		{name: "targetGasLimit case variant", edit: set("TargetGasLimit", `"0x1"`)},
		{name: "targetGasLimit malformed", edit: set("targetGasLimit", "true"), wantErr: `invalid field "targetGasLimit"`},
		{name: "anchorTransaction present", edit: set("anchorTransaction", `"0x"`), wantAnchorTx: true},
		{name: "anchorTransaction null", edit: set("anchorTransaction", "null")},
		{name: "anchorTransaction case variant", edit: set("AnchorTransaction", `"0x"`)},
		{name: "anchorTransaction malformed", edit: set("anchorTransaction", "1"), wantErr: `invalid field "anchorTransaction"`},
		{name: "slotNumber present", edit: set("slotNumber", `"0x1"`), wantSlotNumber: true},
		{name: "missing required field", edit: func(f map[string]json.RawMessage) { delete(f, "l1Origin") }, wantErr: "missing required field 'l1Origin'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attrs TaikoPayloadAttributesV3
			err := json.Unmarshal(taikoAttributesJSON(t, tt.edit), &attrs)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if attrs.TargetGasLimitSet != tt.wantTargetGasLimit {
				t.Errorf("TargetGasLimitSet = %v, want %v", attrs.TargetGasLimitSet, tt.wantTargetGasLimit)
			}
			if attrs.AnchorTransactionSet != tt.wantAnchorTx {
				t.Errorf("AnchorTransactionSet = %v, want %v", attrs.AnchorTransactionSet, tt.wantAnchorTx)
			}
			if (attrs.SlotNumber != nil) != tt.wantSlotNumber {
				t.Errorf("SlotNumber = %v, want set %v", attrs.SlotNumber, tt.wantSlotNumber)
			}
			if attrs.Timestamp != 100 || attrs.L1Origin == nil || attrs.L1Origin.BlockID.Uint64() != 100 {
				t.Errorf("embedded attributes not decoded: %+v", attrs.PayloadAttributes)
			}
		})
	}
}

func TestTaikoExecutionPayloadEnvelopeV5JSON(t *testing.T) {
	hash := func(b byte) common.Hash { return common.Hash{31: b} }
	data := &ExecutableData{
		ParentHash:    hash(0x11),
		FeeRecipient:  common.Address{19: 0x22},
		StateRoot:     hash(0x33),
		ReceiptsRoot:  hash(0x44),
		LogsBloom:     make([]byte, types.BloomByteLength),
		Random:        hash(0x55),
		Number:        1,
		GasLimit:      30_000_000,
		GasUsed:       0,
		Timestamp:     100,
		ExtraData:     []byte{},
		BaseFeePerGas: big.NewInt(1),
		BlockHash:     hash(0x66),
	}
	bloom := `"0x` + strings.Repeat("0", 512) + `"`
	payloadJSON := func(transactions, withdrawals string) string {
		return `{"parentHash":"` + hash(0x11).Hex() + `","feeRecipient":"0x0000000000000000000000000000000000000022",` +
			`"stateRoot":"` + hash(0x33).Hex() + `","receiptsRoot":"` + hash(0x44).Hex() + `","logsBloom":` + bloom + `,` +
			`"prevRandao":"` + hash(0x55).Hex() + `","blockNumber":"0x1","gasLimit":"0x1c9c380","gasUsed":"0x0",` +
			`"timestamp":"0x64","extraData":"0x","baseFeePerGas":"0x1","blockHash":"` + hash(0x66).Hex() + `",` +
			`"transactions":` + transactions + `,"withdrawals":` + withdrawals + `,"blobGasUsed":"0x0","excessBlobGas":"0x0"}`
	}
	const tail = `"blobsBundle":{"commitments":[],"proofs":[],"blobs":[]},"shouldOverrideBuilder":false,"executionRequests":[]}`

	// Nil transactions, withdrawals, blob gas fields and header difficulty all
	// encode as their empty or zero forms.
	enc, err := json.Marshal(NewTaikoExecutionPayloadEnvelopeV5(data))
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	want := `{"executionPayload":` + payloadJSON("[]", "[]") + `,"blockValue":"0x0",` + tail
	if string(enc) != want {
		t.Fatalf("envelope mismatch:\nhave %s\nwant %s", enc, want)
	}

	// blockValue is the header difficulty (7 in the shared cross-client test);
	// set fields keep their values.
	blobGas, excessBlobGas := uint64(0), uint64(0)
	data.HeaderDifficulty = big.NewInt(7)
	data.BlobGasUsed, data.ExcessBlobGas = &blobGas, &excessBlobGas
	data.Transactions = [][]byte{{0x02, 0xc0}}
	data.Withdrawals = []*types.Withdrawal{{Index: 1, Validator: 2, Address: common.Address{19: 3}, Amount: 4}}
	if enc, err = json.Marshal(NewTaikoExecutionPayloadEnvelopeV5(data)); err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	want = `{"executionPayload":` + payloadJSON(`["0x02c0"]`,
		`[{"index":"0x1","validatorIndex":"0x2","address":"0x0000000000000000000000000000000000000003","amount":"0x4"}]`) +
		`,"blockValue":"0x7",` + tail
	if string(enc) != want {
		t.Fatalf("envelope mismatch:\nhave %s\nwant %s", enc, want)
	}
	if data.HeaderDifficulty.Int64() != 7 || common.Big0.Sign() != 0 {
		t.Fatal("building the envelope must not alias the header difficulty")
	}
}

func TestInternalErrorCode(t *testing.T) {
	if InternalError.ErrorCode() != -32603 || InternalError.Error() != "Internal error" {
		t.Fatalf("InternalError = %d %q, want -32603 \"Internal error\"", InternalError.ErrorCode(), InternalError.Error())
	}
}
