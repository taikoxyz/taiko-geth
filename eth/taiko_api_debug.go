// CHANGE(taiko): debug_executionWitnessForTxList replays an explicit
// transaction list on a block's parent state and returns a cross-client
// execution witness (headers RLP-encoded, key preimages populated).
package eth

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// Block-level transaction-list size limits. The raw guard bounds the work of
// the compression check; the authoritative limit is the compressed size that
// must fit one blob of data availability.
const (
	maxTxListRawBytes        = 16 * 1024 * 1024
	maxTxListCompressedBytes = params.BlobTxFieldElementsPerBlob * params.BlobTxBytesPerFieldElement
)

// txListWitnessOptions holds the optional flags for debug_executionWitnessForTxList.
type txListWitnessOptions struct {
	// SkipZkGasDifficultyCheck skips matching the recomputed block zk gas against
	// the block header difficulty. Only useful for debug experiments that replay
	// non-canonical transaction lists on top of a canonical parent state.
	SkipZkGasDifficultyCheck bool `json:"skipZkGasDifficultyCheck"`
}

// txListExecutionWitness is the cross-client execution-witness wire format.
type txListExecutionWitness struct {
	State   []hexutil.Bytes `json:"state"`
	Codes   []hexutil.Bytes `json:"codes"`
	Keys    []hexutil.Bytes `json:"keys"`
	Headers []hexutil.Bytes `json:"headers"`
}

// newTxListExecutionWitness converts the internal witness to the cross-client
// wire format, RLP-encoding each ancestor header.
func newTxListExecutionWitness(w *stateless.Witness) (*txListExecutionWitness, error) {
	out := &txListExecutionWitness{
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

// decodeTxListWitnessTxs decodes an RLP list of transactions. It errors only on
// a malformed top-level list; unrecoverable-signer transactions are filtered
// later during replay, matching block-building behavior.
func decodeTxListWitnessTxs(txListRLP []byte) (types.Transactions, error) {
	var txs types.Transactions
	if err := rlp.DecodeBytes(txListRLP, &txs); err != nil {
		return nil, fmt.Errorf("failed to decode tx list: %w", err)
	}
	return txs, nil
}

// checkTxListSize rejects a transaction list too large to be a valid block list.
func checkTxListSize(raw []byte) error {
	if len(raw) > maxTxListRawBytes {
		return fmt.Errorf("tx list size %d exceeds raw limit %d", len(raw), maxTxListRawBytes)
	}
	compressed, err := zlibCompressedLen(raw)
	if err != nil {
		return err
	}
	if compressed > maxTxListCompressedBytes {
		return fmt.Errorf("tx list compressed size %d exceeds block data limit %d", compressed, maxTxListCompressedBytes)
	}
	return nil
}

func zlibCompressedLen(raw []byte) (int, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return 0, err
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	return buf.Len(), nil
}

// zkGasDifficultyMismatch reports whether the recomputed block zk gas differs
// from the block header difficulty.
func zkGasDifficultyMismatch(headerDifficulty *big.Int, recomputed uint64) bool {
	return headerDifficulty == nil || headerDifficulty.Cmp(new(big.Int).SetUint64(recomputed)) != 0
}
