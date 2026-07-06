package eth

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
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

func TestIsRecoverableNonAnchorTxError(t *testing.T) {
	recoverable := []error{
		vm.ErrZkGasLimitExceeded,
		core.ErrNonceTooLow,
		core.ErrNonceTooHigh,
		core.ErrInsufficientFunds,
		core.ErrIntrinsicGas,
		core.ErrGasLimitReached,
		core.ErrFeeCapTooLow,
	}
	for _, err := range recoverable {
		if !isRecoverableNonAnchorTxError(err) {
			t.Fatalf("expected %v to be recoverable", err)
		}
		wrapped := fmt.Errorf("apply tx: %w", err)
		if !isRecoverableNonAnchorTxError(wrapped) {
			t.Fatalf("expected wrapped %v to be recoverable", wrapped)
		}
	}

	if isRecoverableNonAnchorTxError(errors.New("boom")) {
		t.Fatalf("unrelated error must not be recoverable")
	}
	if isRecoverableNonAnchorTxError(fmt.Errorf("wrapped: %w", errors.New("boom"))) {
		t.Fatalf("wrapped unrelated error must not be recoverable")
	}
}

func TestIsRecoverableNonAnchorTxErrorIncludesInitcodeLimit(t *testing.T) {
	// The reference replay skips over-limit initcode creations like any other
	// invalid-transaction precheck failure.
	if !isRecoverableNonAnchorTxError(fmt.Errorf("apply tx: %w", vm.ErrMaxInitCodeSizeExceeded)) {
		t.Fatalf("expected max-initcode-size errors to be recoverable")
	}
}

func TestDecodeTxListWitnessTxsDropsUnrecoverableSigners(t *testing.T) {
	key, _ := crypto.GenerateKey()
	signer := types.LatestSignerForChainID(big.NewInt(167000))
	valid, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(167000), Nonce: 0, GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(1), Gas: 21000, To: &common.Address{}, Value: big.NewInt(0),
	}), signer, key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	junk, err := valid.WithSignature(signer, make([]byte, 65))
	if err != nil {
		t.Fatalf("junk signature: %v", err)
	}

	encoded, err := rlp.EncodeToBytes(types.Transactions{junk, valid})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	txs, err := decodeTxListWitnessTxs(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(txs) != 1 || txs[0].Hash() != valid.Hash() {
		t.Fatalf("expected only the recoverable tx to survive decode, got %d", len(txs))
	}
}
