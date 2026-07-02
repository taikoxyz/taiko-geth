// CHANGE(taiko): tests for the cross-client debug execution-witness wire format.
package stateless

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestNewExecutionWitness(t *testing.T) {
	hdr := &types.Header{Number: big.NewInt(7)}
	w := &Witness{
		Headers: []*types.Header{hdr},
		State:   map[string]struct{}{"node": {}},
		Codes:   map[string]struct{}{"code": {}},
		Keys:    map[string]struct{}{"key": {}},
	}
	out, err := NewExecutionWitness(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.State) != 1 || len(out.Codes) != 1 || len(out.Keys) != 1 || len(out.Headers) != 1 {
		t.Fatalf("unexpected lengths: %+v", out)
	}
	if string(out.State[0]) != "node" || string(out.Codes[0]) != "code" || string(out.Keys[0]) != "key" {
		t.Fatalf("unexpected witness bytes: %+v", out)
	}
	var decoded types.Header
	if err := rlp.DecodeBytes(out.Headers[0], &decoded); err != nil {
		t.Fatalf("headers must RLP-decode to a header: %v", err)
	}
	if decoded.Number.Uint64() != 7 {
		t.Fatalf("wrong header number: %d", decoded.Number.Uint64())
	}
}

func TestExecutionWitnessToExtWitnessDecodesRLPHeaders(t *testing.T) {
	header := &types.Header{Number: big.NewInt(7)}
	encodedHeader, err := rlp.EncodeToBytes(header)
	if err != nil {
		t.Fatalf("encode header: %v", err)
	}

	wire := ExecutionWitness{
		Headers: []hexutil.Bytes{encodedHeader},
		Codes:   []hexutil.Bytes{[]byte("code")},
		State:   []hexutil.Bytes{[]byte("node")},
		Keys:    []hexutil.Bytes{[]byte("key")},
	}
	ext, err := wire.ToExtWitness()
	if err != nil {
		t.Fatalf("ToExtWitness: %v", err)
	}
	if len(ext.Headers) != 1 {
		t.Fatalf("expected one header, got %d", len(ext.Headers))
	}
	if ext.Headers[0].Number.Uint64() != 7 {
		t.Fatalf("wrong header number: %d", ext.Headers[0].Number.Uint64())
	}
	if string(ext.Codes[0]) != "code" || string(ext.State[0]) != "node" || string(ext.Keys[0]) != "key" {
		t.Fatalf("unexpected witness payload: %+v", ext)
	}
}
