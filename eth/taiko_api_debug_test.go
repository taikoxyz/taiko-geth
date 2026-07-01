package eth

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestDecodeTxListWitnessTxsEmpty(t *testing.T) {
	txs, err := decodeTxListWitnessTxs([]byte{0xc0}) // RLP empty list
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txs) != 0 {
		t.Fatalf("expected 0 txs, got %d", len(txs))
	}
}

func TestDecodeTxListWitnessTxsMalformed(t *testing.T) {
	if _, err := decodeTxListWitnessTxs([]byte{0xff, 0xff}); err == nil {
		t.Fatalf("expected error on malformed rlp")
	}
}

func TestCheckTxListSizeRawLimit(t *testing.T) {
	big := make([]byte, maxTxListRawBytes+1)
	if err := checkTxListSize(big); err == nil {
		t.Fatalf("expected raw-limit error")
	}
}

func TestTxListWitnessOptionsCamelCase(t *testing.T) {
	var opts txListWitnessOptions
	if err := json.Unmarshal([]byte(`{"skipZkGasDifficultyCheck":true}`), &opts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !opts.SkipZkGasDifficultyCheck {
		t.Fatalf("expected skip flag to be true")
	}
}

func TestZkGasDifficultyMismatch(t *testing.T) {
	if zkGasDifficultyMismatch(big.NewInt(100), 100) {
		t.Fatalf("equal values must not be a mismatch")
	}
	if !zkGasDifficultyMismatch(big.NewInt(99), 100) {
		t.Fatalf("unequal values must be a mismatch")
	}
	if !zkGasDifficultyMismatch(nil, 0) {
		t.Fatalf("nil difficulty must be a mismatch")
	}
}

func TestNewTxListExecutionWitness(t *testing.T) {
	hdr := &types.Header{Number: big.NewInt(7)}
	w := &stateless.Witness{
		Headers: []*types.Header{hdr},
		State:   map[string]struct{}{"node": {}},
		Codes:   map[string]struct{}{"code": {}},
		Keys:    map[string]struct{}{"key": {}},
	}
	out, err := newTxListExecutionWitness(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.State) != 1 || len(out.Codes) != 1 || len(out.Keys) != 1 || len(out.Headers) != 1 {
		t.Fatalf("unexpected lengths: %+v", out)
	}
	var decoded types.Header
	if err := rlp.DecodeBytes(out.Headers[0], &decoded); err != nil {
		t.Fatalf("headers must RLP-decode to a header: %v", err)
	}
	if decoded.Number.Uint64() != 7 {
		t.Fatalf("wrong header decoded: %d", decoded.Number.Uint64())
	}
}
