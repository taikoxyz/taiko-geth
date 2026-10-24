package engine

import (
	"bytes"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
)

// legacyEnvelope mirrors the generated encoder that wrote ExecutionPayloadEnvelope
// before upstream replaced it with a hand-written MarshalJSON. Taiko clients
// read that layout, so the hand-written encoder must produce the same bytes.
type legacyEnvelope struct {
	ExecutionPayload *ExecutableData `json:"executionPayload"`
	BlockValue       *hexutil.Big    `json:"blockValue"`
	BlobsBundle      *BlobsBundle    `json:"blobsBundle"`
	Requests         []hexutil.Bytes `json:"executionRequests"`
	Override         bool            `json:"shouldOverrideBuilder"`
	Witness          *hexutil.Bytes  `json:"witness,omitempty"`
}

// taikoEnvelopeTestBlock returns a Taiko block holding one transfer. With
// unzen, the header carries the Unzen fields and the given zk gas as its
// difficulty; otherwise it is a Shanghai header with zero difficulty.
func taikoEnvelopeTestBlock(t *testing.T, unzen bool, zkGas int64) *types.Block {
	t.Helper()
	chainID := big.NewInt(167000)
	key, err := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	if err != nil {
		t.Fatalf("test key: %v", err)
	}
	to := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	tx := types.MustSignNewTx(key, types.LatestSignerForChainID(chainID), &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(20_000_000),
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})
	header := &types.Header{
		ParentHash: common.HexToHash("0x01"),
		Coinbase:   common.HexToAddress("0x02"),
		Root:       common.HexToHash("0x03"),
		MixDigest:  common.HexToHash("0x04"),
		Difficulty: big.NewInt(0),
		Number:     big.NewInt(10),
		GasLimit:   30_000_000,
		GasUsed:    21_000,
		Time:       1_800_000_000,
		Extra:      []byte{0x32, 0, 0, 0, 0, 0, 1},
		BaseFee:    big.NewInt(10_000_000),
	}
	if unzen {
		zero := uint64(0)
		beaconRoot := common.Hash{}
		requestsHash := types.EmptyRequestsHash
		header.Difficulty = big.NewInt(zkGas)
		header.BlobGasUsed = &zero
		header.ExcessBlobGas = &zero
		header.ParentBeaconRoot = &beaconRoot
		header.RequestsHash = &requestsHash
	}
	body := &types.Body{Transactions: types.Transactions{tx}, Withdrawals: types.Withdrawals{}}
	return types.NewBlock(header, body, nil, trie.NewStackTrie(nil))
}

// TestBlockToExecutableData_TaikoEnvelopeRoundTrip covers what getPayload
// returns to a Taiko client. Unzen carries the zk gas in the header difficulty,
// which engine payloads have no field for: the Taiko headerDifficulty field
// and the envelope's blockValue transport it. The test pins the JSON of the
// Taiko fields, checks that the envelope bytes match the legacy layout, and
// decodes the envelope back into a block with the original hash.
func TestBlockToExecutableData_TaikoEnvelopeRoundTrip(t *testing.T) {
	const zkGas = 123456
	fees := big.NewInt(7)
	zeroHash, err := json.Marshal(common.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                 string
		unzen                bool
		wantBlockValue       *big.Int
		wantBlockValueJSON   string // raw JSON of the envelope's blockValue
		wantHeaderDifficulty string // raw JSON of the payload's headerDifficulty
	}{
		// Unzen sends the zk gas instead of the fees as blockValue.
		{name: "Unzen", unzen: true, wantBlockValue: big.NewInt(zkGas), wantBlockValueJSON: `"0x1e240"`, wantHeaderDifficulty: `123456`},
		{name: "pre-Unzen", unzen: false, wantBlockValue: fees, wantBlockValueJSON: `"0x7"`, wantHeaderDifficulty: `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := taikoEnvelopeTestBlock(t, tt.unzen, zkGas)
			env := BlockToExecutableData(block, fees, nil, nil)
			enc, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}

			var fields map[string]json.RawMessage
			if err := json.Unmarshal(enc, &fields); err != nil {
				t.Fatalf("envelope is not a JSON object: %v", err)
			}
			if got, ok := fields["blockValue"]; !ok {
				t.Errorf("envelope has no blockValue field: %s", enc)
			} else if string(got) != tt.wantBlockValueJSON {
				t.Errorf("blockValue = %s, want %s", got, tt.wantBlockValueJSON)
			}
			var payloadFields map[string]json.RawMessage
			if err := json.Unmarshal(fields["executionPayload"], &payloadFields); err != nil {
				t.Fatalf("executionPayload is not a JSON object: %v", err)
			}
			// getPayload never fills txHash or withdrawalsHash, and TaikoBlock
			// has no JSON tag, but all three stay on the wire.
			wantPayload := map[string]string{
				"headerDifficulty": tt.wantHeaderDifficulty,
				"txHash":           string(zeroHash),
				"withdrawalsHash":  string(zeroHash),
				"TaikoBlock":       `false`,
			}
			for key, want := range wantPayload {
				got, ok := payloadFields[key]
				if !ok {
					t.Errorf("executionPayload has no %q field: %s", key, enc)
					continue
				}
				if string(got) != want {
					t.Errorf("executionPayload %q = %s, want %s", key, got, want)
				}
			}

			legacy, err := json.Marshal(&legacyEnvelope{
				ExecutionPayload: env.ExecutionPayload,
				BlockValue:       (*hexutil.Big)(env.BlockValue),
				BlobsBundle:      env.BlobsBundle,
				Override:         env.Override,
				Witness:          env.Witness,
			})
			if err != nil {
				t.Fatalf("marshal legacy envelope: %v", err)
			}
			if !bytes.Equal(enc, legacy) {
				t.Errorf("envelope bytes differ from the legacy layout\n got: %s\nwant: %s", enc, legacy)
			}

			var dec ExecutionPayloadEnvelope
			if err := json.Unmarshal(enc, &dec); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if dec.BlockValue.Cmp(tt.wantBlockValue) != 0 {
				t.Errorf("decoded blockValue = %v, want %v", dec.BlockValue, tt.wantBlockValue)
			}
			// engine_newPayload marks every payload on a Taiko chain.
			dec.ExecutionPayload.TaikoBlock = true
			got, err := ExecutableDataToBlock(*dec.ExecutionPayload, nil, nil, nil)
			if err != nil {
				t.Fatalf("ExecutableDataToBlock: %v", err)
			}
			if got.Hash() != block.Hash() {
				t.Fatalf("round-tripped block hash = %v, want %v", got.Hash(), block.Hash())
			}
			if got.Difficulty().Cmp(block.Difficulty()) != 0 {
				t.Fatalf("round-tripped difficulty = %v, want %v", got.Difficulty(), block.Difficulty())
			}
		})
	}
}
