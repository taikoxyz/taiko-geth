// CHANGE(taiko): the cross-client debug execution-witness wire format, shared
// by the eth debug RPC producer and in-tree RPC consumers.
package stateless

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// ExecutionWitness is the cross-client debug execution-witness wire format.
// All fields are byte arrays in JSON; headers are RLP-encoded.
type ExecutionWitness struct {
	State   []hexutil.Bytes `json:"state"`
	Codes   []hexutil.Bytes `json:"codes"`
	Keys    []hexutil.Bytes `json:"keys"`
	Headers []hexutil.Bytes `json:"headers"`
}

// NewExecutionWitness converts the internal witness to the cross-client debug
// RPC wire format, RLP-encoding each ancestor header.
func NewExecutionWitness(w *Witness) (*ExecutionWitness, error) {
	out := &ExecutionWitness{
		State:   make([]hexutil.Bytes, 0, len(w.State)),
		Codes:   make([]hexutil.Bytes, 0, len(w.Codes)),
		Keys:    make([]hexutil.Bytes, 0, len(w.Keys)),
		Headers: make([]hexutil.Bytes, 0, len(w.Headers)),
	}
	for node := range w.State {
		out.State = append(out.State, []byte(node))
	}
	for code := range w.Codes {
		out.Codes = append(out.Codes, []byte(code))
	}
	for key := range w.Keys {
		out.Keys = append(out.Keys, []byte(key))
	}
	for _, header := range w.Headers {
		enc, err := rlp.EncodeToBytes(header)
		if err != nil {
			return nil, fmt.Errorf("failed to rlp-encode witness header %v: %w", header.Number, err)
		}
		out.Headers = append(out.Headers, enc)
	}
	return out, nil
}

// ToExtWitness converts the wire format into the external witness shape,
// RLP-decoding each header.
func (w *ExecutionWitness) ToExtWitness() (*ExtWitness, error) {
	ext := &ExtWitness{
		State: w.State,
		Codes: w.Codes,
		Keys:  w.Keys,
	}
	ext.Headers = make([]*types.Header, 0, len(w.Headers))
	for i, enc := range w.Headers {
		var header types.Header
		if err := rlp.DecodeBytes(enc, &header); err != nil {
			return nil, fmt.Errorf("header %d: %w", i, err)
		}
		ext.Headers = append(ext.Headers, &header)
	}
	return ext, nil
}
