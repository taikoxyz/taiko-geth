package eth

import (
	"encoding/json"
	"testing"
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
