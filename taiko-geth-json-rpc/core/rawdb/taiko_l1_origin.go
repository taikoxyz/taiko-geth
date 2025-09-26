package rawdb

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// Database key prefix for L2 block's L1Origin.
	l1OriginPrefix         = []byte("TKO:L1O")
	batchToLastBlockPrefix = []byte("TKO:B2B")
	headL1OriginKey        = []byte("TKO:LastL1O")
)

// l1OriginKey calculates the L1Origin key.
// l1OriginPrefix + l2HeaderHash -> l1OriginKey
func l1OriginKey(blockID *big.Int) []byte {
	data, _ := (*math.HexOrDecimal256)(blockID).MarshalText()
	return append(l1OriginPrefix, data...)
}

// batchToLastBlockKey calculates the batch to block key.
// batchToBlockPrefix + batch ID -> batchToLastBlockKey
func batchToLastBlockKey(batch *big.Int) []byte {
	data, _ := (*math.HexOrDecimal256)(batch).MarshalText()
	return append(batchToLastBlockPrefix, data...)
}

//go:generate go run github.com/fjl/gencodec -type L1Origin -field-override l1OriginMarshaling -out gen_taiko_l1_origin.go

// L1Origin represents a L1Origin of a L2 block.
type L1Origin struct {
	BlockID            *big.Int    `json:"blockID" gencodec:"required"`
	L2BlockHash        common.Hash `json:"l2BlockHash"`
	L1BlockHeight      *big.Int    `json:"l1BlockHeight" rlp:"optional"`
	L1BlockHash        common.Hash `json:"l1BlockHash" rlp:"optional"`
	BuildPayloadArgsID [8]byte     `json:"buildPayloadArgsID" rlp:"optional"`
	IsForcedInclusion  bool        `json:"isForcedInclusion" rlp:"optional"`
	Signature          [65]byte    `json:"signature"         rlp:"optional"`
}

// L1OriginLegacy represents a legacy L1Origin of a L2 block.
type L1OriginLegacy struct {
	BlockID       *big.Int    `json:"blockID" gencodec:"required"`
	L2BlockHash   common.Hash `json:"l2BlockHash"`
	L1BlockHeight *big.Int    `json:"l1BlockHeight" rlp:"optional"`
	L1BlockHash   common.Hash `json:"l1BlockHash" rlp:"optional"`
}

type l1OriginMarshaling struct {
	BlockID       *math.HexOrDecimal256
	L1BlockHeight *math.HexOrDecimal256
}

// IsPreconfBlock returns true if the L1Origin is for a preconfirmation block.
// A preconfirmation block is defined as one where the L1BlockHeight is either nil or zero.
func (l *L1Origin) IsPreconfBlock() bool {
	return l.L1BlockHeight == nil || l.L1BlockHeight.Cmp(common.Big0) == 0
}

// WriteL1Origin stores a L1Origin into the database.
func WriteL1Origin(db ethdb.KeyValueWriter, blockID *big.Int, l1Origin *L1Origin) {
	data, err := rlp.EncodeToBytes(l1Origin)
	if err != nil {
		log.Crit("Failed to encode L1Origin", "err", err)
	}

	if err := db.Put(l1OriginKey(blockID), data); err != nil {
		log.Crit("Failed to store L1Origin", "err", err)
	}
}

func ReadL1Origin(db ethdb.KeyValueReader, blockID *big.Int) (*L1Origin, error) {
	data, _ := db.Get(l1OriginKey(blockID))
	if len(data) == 0 {
		return nil, nil
	}

	// First try to decode the new version (with new fields).
	l1Origin := new(L1Origin)
	if err := rlp.Decode(bytes.NewReader(data), l1Origin); err != nil {
		// If decoding the new version fails, try to decode the legacy version (without new fields).
		l1OriginLegacy := new(L1OriginLegacy)
		if err := rlp.Decode(bytes.NewReader(data), &l1OriginLegacy); err != nil {
			return nil, fmt.Errorf("invalid legacy L1Origin RLP bytes: %w", err)
		}

		l1Origin = &L1Origin{
			BlockID:            l1OriginLegacy.BlockID,
			L2BlockHash:        l1OriginLegacy.L2BlockHash,
			L1BlockHeight:      l1OriginLegacy.L1BlockHeight,
			L1BlockHash:        l1OriginLegacy.L1BlockHash,
			BuildPayloadArgsID: [8]byte{},
			// These will be zero values
			IsForcedInclusion: false,
			Signature:         [65]byte{},
		}
	}

	return l1Origin, nil
}

// WriteHeadL1Origin stores the given L1Origin as the last L1Origin.
func WriteHeadL1Origin(db ethdb.KeyValueWriter, blockID *big.Int) {
	data, _ := (*math.HexOrDecimal256)(blockID).MarshalText()
	if err := db.Put(headL1OriginKey, data); err != nil {
		log.Crit("Failed to store head L1Origin", "error", err)
	}
}

// ReadHeadL1Origin retrieves the last L1Origin from database.
func ReadHeadL1Origin(db ethdb.KeyValueReader) (*big.Int, error) {
	data, _ := db.Get(headL1OriginKey)
	if len(data) == 0 {
		return nil, nil
	}

	blockID := new(math.HexOrDecimal256)
	if err := blockID.UnmarshalText(data); err != nil {
		log.Error("Unmarshal L1Origin unmarshal error", "error", err)
		return nil, fmt.Errorf("invalid L1Origin unmarshal: %w", err)
	}

	return (*big.Int)(blockID), nil
}

// WriteBatchToLastBlockID stores the mapping from batch ID to the last block ID in this batch.
func WriteBatchToLastBlockID(db ethdb.KeyValueWriter, batch *big.Int, blockID *big.Int) {
	data, _ := (*math.HexOrDecimal256)(blockID).MarshalText()
	if err := db.Put(batchToLastBlockKey(batch), data); err != nil {
		log.Crit("Failed to store batch to block mapping", "error", err)
	}
}

// ReadBatchToLastBlockID retrieves the block ID corresponding to the last block ID in this batch.
func ReadBatchToLastBlockID(db ethdb.KeyValueReader, batch *big.Int) (*big.Int, error) {
	data, _ := db.Get(batchToLastBlockKey(batch))
	if len(data) == 0 {
		return nil, nil
	}

	blockID := new(math.HexOrDecimal256)
	if err := blockID.UnmarshalText(data); err != nil {
		log.Error("Unmarshal batch to block unmarshal error", "error", err)
		return nil, fmt.Errorf("invalid batch to block unmarshal: %w", err)
	}

	return (*big.Int)(blockID), nil
}
