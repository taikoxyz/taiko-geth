package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// InternalError is the JSON-RPC -32603 Engine API error. The Taiko Engine API
// returns it when a payload build fails and when Unzen execution exhausts or
// mismatches zk gas.
var InternalError = &EngineAPIError{code: -32603, msg: "Internal error"}

// TaikoPayloadAttributesV3 is the engine_forkchoiceUpdatedV3 attributes object.
//
// The embedded PayloadAttributes is decoded by its generated decoder. The two
// flags record whether "targetGasLimit" and "anchorTransaction" are present
// with a non-null value; key matching for them is exact-case.
//
// The type has no MarshalJSON of its own: json.Marshal uses the promoted
// PayloadAttributes encoder and drops both flags, so a caller that needs either
// key on the wire has to send raw JSON.
type TaikoPayloadAttributesV3 struct {
	PayloadAttributes
	TargetGasLimitSet    bool // "targetGasLimit" present with a non-null value
	AnchorTransactionSet bool // "anchorTransaction" present with a non-null value
}

// UnmarshalJSON decodes the attributes and records the presence of
// targetGasLimit and anchorTransaction. A present, non-null value of either key
// must still be well formed (a hex quantity and hex bytes respectively).
func (a *TaikoPayloadAttributesV3) UnmarshalJSON(input []byte) error {
	var attrs PayloadAttributes
	if err := attrs.UnmarshalJSON(input); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return err
	}
	targetGasLimitSet, err := optionalFieldSet(fields, "targetGasLimit", new(hexutil.Uint64))
	if err != nil {
		return err
	}
	anchorTransactionSet, err := optionalFieldSet(fields, "anchorTransaction", new(hexutil.Bytes))
	if err != nil {
		return err
	}
	*a = TaikoPayloadAttributesV3{
		PayloadAttributes:    attrs,
		TargetGasLimitSet:    targetGasLimitSet,
		AnchorTransactionSet: anchorTransactionSet,
	}
	return nil
}

// optionalFieldSet reports whether key is present with a non-null value, after
// checking that the value decodes into dst.
func optionalFieldSet(fields map[string]json.RawMessage, key string, dst any) (bool, error) {
	raw, ok := fields[key]
	if !ok || bytes.Equal(raw, []byte("null")) {
		return false, nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false, fmt.Errorf("invalid field %q: %w", key, err)
	}
	return true, nil
}

// TaikoExecutionPayloadV3 is the strict engine_newPayloadV4 payload: the 17
// standard ExecutionPayloadV3 properties plus headerDifficulty.
type TaikoExecutionPayloadV3 struct {
	ParentHash       common.Hash
	FeeRecipient     common.Address
	StateRoot        common.Hash
	ReceiptsRoot     common.Hash
	LogsBloom        []byte
	Random           common.Hash
	Number           uint64
	GasLimit         uint64
	GasUsed          uint64
	Timestamp        uint64
	ExtraData        []byte
	BaseFeePerGas    *big.Int
	BlockHash        common.Hash
	Transactions     [][]byte
	Withdrawals      []*types.Withdrawal
	BlobGasUsed      uint64
	ExcessBlobGas    uint64
	HeaderDifficulty uint64
}

// taikoPayloadV3Keys lists the properties of TaikoExecutionPayloadV3 in wire
// order. Every one of them is required and non-null.
var taikoPayloadV3Keys = []string{
	"parentHash", "feeRecipient", "stateRoot", "receiptsRoot", "logsBloom", "prevRandao",
	"blockNumber", "gasLimit", "gasUsed", "timestamp", "extraData", "baseFeePerGas",
	"blockHash", "transactions", "withdrawals", "blobGasUsed", "excessBlobGas", "headerDifficulty",
}

// UnmarshalJSON decodes the payload strictly:
//   - the input is a JSON object with exact-case, non-duplicated keys;
//   - all 18 properties are required and non-null;
//   - quantities are hex strings, logsBloom is exactly 256 bytes, transactions
//     and withdrawals are arrays without null elements;
//   - headerDifficulty is a decimal JSON number that fits u64; strings, hex,
//     negative numbers, fractions and exponents are rejected;
//   - any other property is rejected, even when null.
func (p *TaikoExecutionPayloadV3) UnmarshalJSON(input []byte) error {
	fields, err := readJSONObject(input)
	if err != nil {
		return fmt.Errorf("invalid execution payload: %w", err)
	}
	var (
		dec          TaikoExecutionPayloadV3
		logsBloom    hexutil.Bytes
		number       hexutil.Uint64
		gasLimit     hexutil.Uint64
		gasUsed      hexutil.Uint64
		timestamp    hexutil.Uint64
		extraData    hexutil.Bytes
		baseFee      hexutil.Big
		transactions []hexutil.Bytes
		blobGasUsed  hexutil.Uint64
		excessBlob   hexutil.Uint64
	)
	targets := []struct {
		key string
		dst any
	}{
		{"parentHash", &dec.ParentHash},
		{"feeRecipient", &dec.FeeRecipient},
		{"stateRoot", &dec.StateRoot},
		{"receiptsRoot", &dec.ReceiptsRoot},
		{"logsBloom", &logsBloom},
		{"prevRandao", &dec.Random},
		{"blockNumber", &number},
		{"gasLimit", &gasLimit},
		{"gasUsed", &gasUsed},
		{"timestamp", &timestamp},
		{"extraData", &extraData},
		{"baseFeePerGas", &baseFee},
		{"blockHash", &dec.BlockHash},
		{"transactions", &transactions},
	}
	for _, target := range targets {
		if err := decodeRequiredField(fields, target.key, target.dst); err != nil {
			return err
		}
	}
	if len(logsBloom) != types.BloomByteLength {
		return fmt.Errorf("invalid field \"logsBloom\": have %d bytes, want %d", len(logsBloom), types.BloomByteLength)
	}
	raw, err := requiredField(fields, "withdrawals")
	if err != nil {
		return err
	}
	if dec.Withdrawals, err = decodeWithdrawals(raw); err != nil {
		return fmt.Errorf("invalid field \"withdrawals\": %w", err)
	}
	if err := decodeRequiredField(fields, "blobGasUsed", &blobGasUsed); err != nil {
		return err
	}
	if err := decodeRequiredField(fields, "excessBlobGas", &excessBlob); err != nil {
		return err
	}
	if raw, err = requiredField(fields, "headerDifficulty"); err != nil {
		return err
	}
	if dec.HeaderDifficulty, err = decodeDecimalUint64(raw); err != nil {
		return fmt.Errorf("invalid field \"headerDifficulty\": %w", err)
	}
	var unexpected []string
	for key := range fields {
		if !slices.Contains(taikoPayloadV3Keys, key) {
			unexpected = append(unexpected, key)
		}
	}
	if len(unexpected) > 0 {
		slices.Sort(unexpected)
		return fmt.Errorf("unsupported Osaka payload fields: %s", strings.Join(unexpected, ", "))
	}
	dec.LogsBloom = logsBloom
	dec.Number = uint64(number)
	dec.GasLimit = uint64(gasLimit)
	dec.GasUsed = uint64(gasUsed)
	dec.Timestamp = uint64(timestamp)
	dec.ExtraData = extraData
	dec.BaseFeePerGas = baseFee.ToInt()
	dec.Transactions = make([][]byte, len(transactions))
	for i, tx := range transactions {
		dec.Transactions[i] = tx
	}
	dec.BlobGasUsed = uint64(blobGasUsed)
	dec.ExcessBlobGas = uint64(excessBlob)
	*p = dec
	return nil
}

// MarshalJSON encodes the payload in the wire form UnmarshalJSON accepts:
// the 17 standard properties plus headerDifficulty as a decimal JSON number.
func (p TaikoExecutionPayloadV3) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		taikoPayloadV3JSON
		HeaderDifficulty uint64 `json:"headerDifficulty"`
	}{p.builtPayload().wire(), p.HeaderDifficulty})
}

// ToExecutableData converts the payload into the ExecutableData that block
// conversion takes. The blob gas fields are always set, withdrawals and
// transactions are never nil, and HeaderDifficulty carries the finalized zk gas.
func (p *TaikoExecutionPayloadV3) ToExecutableData() ExecutableData {
	var (
		blobGasUsed   = p.BlobGasUsed
		excessBlobGas = p.ExcessBlobGas
		withdrawals   = p.Withdrawals
		transactions  = p.Transactions
		baseFee       = new(big.Int)
	)
	if withdrawals == nil {
		withdrawals = []*types.Withdrawal{}
	}
	if transactions == nil {
		transactions = [][]byte{}
	}
	if p.BaseFeePerGas != nil {
		baseFee.Set(p.BaseFeePerGas)
	}
	return ExecutableData{
		ParentHash:       p.ParentHash,
		FeeRecipient:     p.FeeRecipient,
		StateRoot:        p.StateRoot,
		ReceiptsRoot:     p.ReceiptsRoot,
		LogsBloom:        p.LogsBloom,
		Random:           p.Random,
		Number:           p.Number,
		GasLimit:         p.GasLimit,
		GasUsed:          p.GasUsed,
		Timestamp:        p.Timestamp,
		ExtraData:        p.ExtraData,
		BaseFeePerGas:    baseFee,
		BlockHash:        p.BlockHash,
		Transactions:     transactions,
		Withdrawals:      withdrawals,
		BlobGasUsed:      &blobGasUsed,
		ExcessBlobGas:    &excessBlobGas,
		HeaderDifficulty: new(big.Int).SetUint64(p.HeaderDifficulty),
		TaikoBlock:       true,
	}
}

// builtPayload returns the 17 standard properties of the payload.
func (p *TaikoExecutionPayloadV3) builtPayload() *TaikoBuiltPayloadV3 {
	return &TaikoBuiltPayloadV3{
		ParentHash:    p.ParentHash,
		FeeRecipient:  p.FeeRecipient,
		StateRoot:     p.StateRoot,
		ReceiptsRoot:  p.ReceiptsRoot,
		LogsBloom:     p.LogsBloom,
		Random:        p.Random,
		Number:        p.Number,
		GasLimit:      p.GasLimit,
		GasUsed:       p.GasUsed,
		Timestamp:     p.Timestamp,
		ExtraData:     p.ExtraData,
		BaseFeePerGas: p.BaseFeePerGas,
		BlockHash:     p.BlockHash,
		Transactions:  p.Transactions,
		Withdrawals:   p.Withdrawals,
		BlobGasUsed:   p.BlobGasUsed,
		ExcessBlobGas: p.ExcessBlobGas,
	}
}

// TaikoBuiltPayloadV3 is executionPayload in the engine_getPayloadV5 response:
// exactly the 17 standard ExecutionPayloadV3 properties.
type TaikoBuiltPayloadV3 struct {
	ParentHash    common.Hash
	FeeRecipient  common.Address
	StateRoot     common.Hash
	ReceiptsRoot  common.Hash
	LogsBloom     []byte
	Random        common.Hash
	Number        uint64
	GasLimit      uint64
	GasUsed       uint64
	Timestamp     uint64
	ExtraData     []byte
	BaseFeePerGas *big.Int
	BlockHash     common.Hash
	Transactions  [][]byte
	Withdrawals   []*types.Withdrawal
	BlobGasUsed   uint64
	ExcessBlobGas uint64
}

// taikoPayloadV3JSON is the wire form of the 17 standard properties, in the
// standard ExecutionPayloadV3 order.
type taikoPayloadV3JSON struct {
	ParentHash    common.Hash         `json:"parentHash"`
	FeeRecipient  common.Address      `json:"feeRecipient"`
	StateRoot     common.Hash         `json:"stateRoot"`
	ReceiptsRoot  common.Hash         `json:"receiptsRoot"`
	LogsBloom     hexutil.Bytes       `json:"logsBloom"`
	Random        common.Hash         `json:"prevRandao"`
	Number        hexutil.Uint64      `json:"blockNumber"`
	GasLimit      hexutil.Uint64      `json:"gasLimit"`
	GasUsed       hexutil.Uint64      `json:"gasUsed"`
	Timestamp     hexutil.Uint64      `json:"timestamp"`
	ExtraData     hexutil.Bytes       `json:"extraData"`
	BaseFeePerGas *hexutil.Big        `json:"baseFeePerGas"`
	BlockHash     common.Hash         `json:"blockHash"`
	Transactions  []hexutil.Bytes     `json:"transactions"`
	Withdrawals   []*types.Withdrawal `json:"withdrawals"`
	BlobGasUsed   hexutil.Uint64      `json:"blobGasUsed"`
	ExcessBlobGas hexutil.Uint64      `json:"excessBlobGas"`
}

// wire converts the payload into its wire form. Nil slices encode as empty
// arrays and a nil base fee encodes as "0x0".
func (p *TaikoBuiltPayloadV3) wire() taikoPayloadV3JSON {
	baseFee := new(big.Int)
	if p.BaseFeePerGas != nil {
		baseFee.Set(p.BaseFeePerGas)
	}
	transactions := make([]hexutil.Bytes, len(p.Transactions))
	for i, tx := range p.Transactions {
		transactions[i] = tx
	}
	withdrawals := p.Withdrawals
	if withdrawals == nil {
		withdrawals = []*types.Withdrawal{}
	}
	return taikoPayloadV3JSON{
		ParentHash:    p.ParentHash,
		FeeRecipient:  p.FeeRecipient,
		StateRoot:     p.StateRoot,
		ReceiptsRoot:  p.ReceiptsRoot,
		LogsBloom:     p.LogsBloom,
		Random:        p.Random,
		Number:        hexutil.Uint64(p.Number),
		GasLimit:      hexutil.Uint64(p.GasLimit),
		GasUsed:       hexutil.Uint64(p.GasUsed),
		Timestamp:     hexutil.Uint64(p.Timestamp),
		ExtraData:     p.ExtraData,
		BaseFeePerGas: (*hexutil.Big)(baseFee),
		BlockHash:     p.BlockHash,
		Transactions:  transactions,
		Withdrawals:   withdrawals,
		BlobGasUsed:   hexutil.Uint64(p.BlobGasUsed),
		ExcessBlobGas: hexutil.Uint64(p.ExcessBlobGas),
	}
}

// MarshalJSON encodes exactly the 17 standard ExecutionPayloadV3 properties.
func (p TaikoBuiltPayloadV3) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.wire())
}

// TaikoExecutionPayloadEnvelopeV5 is the engine_getPayloadV5 response. The
// field order follows the standard ExecutionPayloadEnvelopeV5 key order.
type TaikoExecutionPayloadEnvelopeV5 struct {
	ExecutionPayload      *TaikoBuiltPayloadV3 `json:"executionPayload"`
	BlockValue            *hexutil.Big         `json:"blockValue"`
	BlobsBundle           *BlobsBundle         `json:"blobsBundle"`
	ShouldOverrideBuilder bool                 `json:"shouldOverrideBuilder"`
	ExecutionRequests     []hexutil.Bytes      `json:"executionRequests"`
}

// NewTaikoExecutionPayloadEnvelopeV5 builds the engine_getPayloadV5 response for
// a built payload. blockValue carries the header difficulty (the finalized zk
// gas, zero when unset); the blobs bundle and the execution requests are always
// empty, and shouldOverrideBuilder is false.
func NewTaikoExecutionPayloadEnvelopeV5(data *ExecutableData) *TaikoExecutionPayloadEnvelopeV5 {
	var blobGasUsed, excessBlobGas uint64
	if data.BlobGasUsed != nil {
		blobGasUsed = *data.BlobGasUsed
	}
	if data.ExcessBlobGas != nil {
		excessBlobGas = *data.ExcessBlobGas
	}
	return &TaikoExecutionPayloadEnvelopeV5{
		ExecutionPayload: &TaikoBuiltPayloadV3{
			ParentHash:    data.ParentHash,
			FeeRecipient:  data.FeeRecipient,
			StateRoot:     data.StateRoot,
			ReceiptsRoot:  data.ReceiptsRoot,
			LogsBloom:     data.LogsBloom,
			Random:        data.Random,
			Number:        data.Number,
			GasLimit:      data.GasLimit,
			GasUsed:       data.GasUsed,
			Timestamp:     data.Timestamp,
			ExtraData:     data.ExtraData,
			BaseFeePerGas: data.BaseFeePerGas,
			BlockHash:     data.BlockHash,
			Transactions:  data.Transactions,
			Withdrawals:   data.Withdrawals,
			BlobGasUsed:   blobGasUsed,
			ExcessBlobGas: excessBlobGas,
		},
		BlockValue: (*hexutil.Big)(new(big.Int).Set(data.HeaderDifficultyOrZero())),
		BlobsBundle: &BlobsBundle{
			Commitments: []hexutil.Bytes{},
			Proofs:      []hexutil.Bytes{},
			Blobs:       []hexutil.Bytes{},
		},
		ShouldOverrideBuilder: false,
		ExecutionRequests:     []hexutil.Bytes{},
	}
}

// readJSONObject splits a JSON object into its members. Keys keep their exact
// case, and a repeated key is an error.
func readJSONObject(input []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(input))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("want a JSON object")
	}
	fields := make(map[string]json.RawMessage)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected object key %v", tok)
		}
		if _, dup := fields[key]; dup {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return fields, nil
}

// requiredField returns the raw value of key, which must be present and non-null.
func requiredField(fields map[string]json.RawMessage, key string) (json.RawMessage, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, fmt.Errorf("missing required field %q", key)
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, fmt.Errorf("field %q must not be null", key)
	}
	return raw, nil
}

// decodeRequiredField decodes the present, non-null value of key into dst.
func decodeRequiredField(fields map[string]json.RawMessage, key string, dst any) error {
	raw, err := requiredField(fields, key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid field %q: %w", key, err)
	}
	return nil
}

// decodeWithdrawals decodes a withdrawals array. Every element must be an
// object with non-null index, validatorIndex, address and amount; other keys
// inside an element are ignored.
func decodeWithdrawals(raw json.RawMessage) ([]*types.Withdrawal, error) {
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil, err
	}
	withdrawals := make([]*types.Withdrawal, 0, len(elems))
	for i, elem := range elems {
		fields, err := readJSONObject(elem)
		if err != nil {
			return nil, fmt.Errorf("withdrawal %d: %w", i, err)
		}
		var (
			index, validator, amount hexutil.Uint64
			address                  common.Address
		)
		targets := []struct {
			key string
			dst any
		}{
			{"index", &index},
			{"validatorIndex", &validator},
			{"address", &address},
			{"amount", &amount},
		}
		for _, target := range targets {
			if err := decodeRequiredField(fields, target.key, target.dst); err != nil {
				return nil, fmt.Errorf("withdrawal %d: %w", i, err)
			}
		}
		withdrawals = append(withdrawals, &types.Withdrawal{
			Index:     uint64(index),
			Validator: uint64(validator),
			Address:   address,
			Amount:    uint64(amount),
		})
	}
	return withdrawals, nil
}

// decodeDecimalUint64 decodes a JSON number made only of decimal digits that
// fits in a uint64.
func decodeDecimalUint64(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 {
		return 0, errors.New("empty value")
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("want a decimal JSON number, have %s", raw)
		}
	}
	return strconv.ParseUint(string(raw), 10, 64)
}
