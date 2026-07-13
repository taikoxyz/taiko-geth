package rawdb

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// randomBigInt generates a random big integer.
func randomBigInt() *big.Int {
	randomBigInt, err := rand.Int(rand.Reader, common.Big256)
	if err != nil {
		log.Crit(err.Error())
	}

	return randomBigInt
}

// randomHash generates a random blob of data and returns it as a hash.
func randomHash() common.Hash {
	var hash common.Hash
	if n, err := rand.Read(hash[:]); n != common.HashLength || err != nil {
		panic(err)
	}
	return hash
}

func TestL1Origin(t *testing.T) {
	db := NewMemoryDatabase()
	testL1Origin := &L1Origin{
		BlockID:     randomBigInt(),
		L2BlockHash: randomHash(),
		// L1BlockHeight is intentionally set to nil to represent a value of zero for legacy behavior.
		L1BlockHeight:      nil,
		L1BlockHash:        randomHash(),
		BuildPayloadArgsID: [8]byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
	}
	WriteL1Origin(db, testL1Origin.BlockID, testL1Origin)
	l1Origin, err := ReadL1Origin(db, testL1Origin.BlockID)
	require.Nil(t, err)
	require.NotNil(t, l1Origin)
	assert.Equal(t, testL1Origin.BlockID, l1Origin.BlockID)
	assert.Equal(t, testL1Origin.L2BlockHash, l1Origin.L2BlockHash)
	assert.True(t, l1Origin.L1BlockHeight.Cmp(common.Big0) == 0)
	assert.Equal(t, testL1Origin.L1BlockHash, l1Origin.L1BlockHash)
	assert.Equal(t, testL1Origin.BuildPayloadArgsID, l1Origin.BuildPayloadArgsID)
}

func TestHeadL1Origin(t *testing.T) {
	db := NewMemoryDatabase()
	testBlockID := randomBigInt()
	WriteHeadL1Origin(db, testBlockID)
	blockID, err := ReadHeadL1Origin(db)
	require.Nil(t, err)
	require.NotNil(t, blockID)
	assert.Equal(t, testBlockID, blockID)
}

func TestL1OriginDeletion(t *testing.T) {
	db := NewMemoryDatabase()
	blockID := big.NewInt(8)
	origin := &L1Origin{
		BlockID:       blockID,
		L2BlockHash:   common.HexToHash("0x08"),
		L1BlockHeight: common.Big1,
	}
	WriteL1Origin(db, blockID, origin)
	WriteHeadL1Origin(db, blockID)
	WriteBatchToLastBlockID(db, big.NewInt(101), blockID)

	batch := db.NewBatch()
	DeleteL1Origin(batch, blockID)
	DeleteHeadL1Origin(batch)
	DeleteBatchToLastBlockID(batch, big.NewInt(101))
	require.NoError(t, batch.Write())

	gotOrigin, err := ReadL1Origin(db, blockID)
	require.NoError(t, err)
	assert.Nil(t, gotOrigin)
	gotHead, err := ReadHeadL1Origin(db)
	require.NoError(t, err)
	assert.Nil(t, gotHead)
	gotBlockID, err := ReadBatchToLastBlockID(db, big.NewInt(101))
	require.NoError(t, err)
	assert.Nil(t, gotBlockID)
}

func TestBatchIDsByLastBlockRange(t *testing.T) {
	db := NewMemoryDatabase()
	WriteBatchToLastBlockID(db, big.NewInt(100), big.NewInt(7))
	WriteBatchToLastBlockID(db, big.NewInt(101), big.NewInt(8))
	WriteBatchToLastBlockID(db, big.NewInt(102), big.NewInt(9))

	batches, err := BatchIDsByLastBlockRange(db, big.NewInt(7), big.NewInt(8))
	require.NoError(t, err)
	assert.ElementsMatch(t, []*big.Int{big.NewInt(100), big.NewInt(101)}, batches)

	batch := db.NewBatch()
	for _, batchID := range batches {
		DeleteBatchToLastBlockID(batch, batchID)
	}
	require.NoError(t, batch.Write())

	for _, batchID := range []*big.Int{big.NewInt(100), big.NewInt(101)} {
		blockID, err := ReadBatchToLastBlockID(db, batchID)
		require.NoError(t, err)
		assert.Nil(t, blockID)
	}
	blockID, err := ReadBatchToLastBlockID(db, big.NewInt(102))
	require.NoError(t, err)
	require.NotNil(t, blockID)
	assert.Equal(t, int64(9), (*big.Int)(blockID).Int64())
}

func TestL1OriginReconciliationJournal(t *testing.T) {
	db := NewMemoryDatabase()
	journal := &L1OriginReconciliationJournal{
		First:         7,
		Last:          9,
		OldHeadHash:   common.HexToHash("0xaa"),
		NewHeadHash:   common.HexToHash("0xbb"),
		NewHeadNumber: 9,
	}
	WriteL1OriginReconciliationJournal(db, journal)
	stored, err := ReadL1OriginReconciliationJournal(db)
	require.NoError(t, err)
	assert.Equal(t, journal, stored)

	batch := db.NewBatch()
	DeleteL1OriginReconciliationJournal(batch)
	require.NoError(t, batch.Write())
	stored, err = ReadL1OriginReconciliationJournal(db)
	require.NoError(t, err)
	assert.Nil(t, stored)
}

func TestL1Origin_OptionalFields(t *testing.T) {
	db := NewMemoryDatabase()

	// helper to generate a random 65-byte signature
	randSig := func() [65]byte {
		var sig [65]byte
		if _, err := rand.Read(sig[:]); err != nil {
			t.Fatalf("rand.Read failed: %v", err)
		}
		return sig
	}

	tests := []struct {
		name              string
		origin            *L1Origin
		expectHeightZero  bool
		expectBuildIDZero bool
		expectForced      bool
		expectSignature   [65]byte
	}{
		{
			name: "signature only",
			origin: &L1Origin{
				BlockID:     randomBigInt(),
				L2BlockHash: randomHash(),
				// leave L1BlockHeight nil → treated as zero
				L1BlockHash:        common.Hash{}, // zero
				BuildPayloadArgsID: [8]byte{},     // zero
				// new fields:
				IsForcedInclusion: false,
				Signature:         randSig(),
			},
			expectHeightZero:  true,
			expectBuildIDZero: true,
			expectForced:      false,
			// will compare against origin.Signature
		},
		{
			name: "forced only",
			origin: &L1Origin{
				BlockID:            randomBigInt(),
				L2BlockHash:        randomHash(),
				L1BlockHeight:      nil,
				L1BlockHash:        common.Hash{}, // zero
				BuildPayloadArgsID: [8]byte{},     // zero
				IsForcedInclusion:  true,
				Signature:          [65]byte{}, // zero
			},
			expectHeightZero:  true,
			expectBuildIDZero: true,
			expectForced:      true,
			expectSignature:   [65]byte{},
		},
		{
			name: "all fields",
			origin: &L1Origin{
				BlockID:            randomBigInt(),
				L2BlockHash:        randomHash(),
				L1BlockHeight:      big.NewInt(42),
				L1BlockHash:        randomHash(),
				BuildPayloadArgsID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
				IsForcedInclusion:  true,
				Signature:          randSig(),
			},
			expectHeightZero:  false,
			expectBuildIDZero: false,
			expectForced:      true,
			// will compare against origin.Signature
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// write & read
			WriteL1Origin(db, tt.origin.BlockID, tt.origin)
			got, err := ReadL1Origin(db, tt.origin.BlockID)
			require.NoError(t, err)
			require.NotNil(t, got)

			// always-check the core fields
			assert.Equal(t, tt.origin.BlockID, got.BlockID, "BlockID")
			assert.Equal(t, tt.origin.L2BlockHash, got.L2BlockHash, "L2BlockHash")

			// L1BlockHeight
			if tt.expectHeightZero {
				// nil or zero should both become zero
				assert.NotNil(t, got.L1BlockHeight, "L1BlockHeight should be non-nil")
				assert.Zero(t, got.L1BlockHeight.Cmp(common.Big0), "L1BlockHeight==0")
			} else {
				assert.Equal(t, tt.origin.L1BlockHeight, got.L1BlockHeight, "L1BlockHeight")
			}

			// L1BlockHash
			if tt.origin.L1BlockHash == (common.Hash{}) {
				assert.Equal(t, common.Hash{}, got.L1BlockHash, "L1BlockHash zero")
			} else {
				assert.Equal(t, tt.origin.L1BlockHash, got.L1BlockHash, "L1BlockHash")
			}

			// BuildPayloadArgsID
			if tt.expectBuildIDZero {
				assert.Equal(t, [8]byte{}, got.BuildPayloadArgsID, "BuildPayloadArgsID zero")
			} else {
				assert.Equal(t, tt.origin.BuildPayloadArgsID, got.BuildPayloadArgsID, "BuildPayloadArgsID")
			}

			// NEW fields
			assert.Equal(t, tt.expectForced, got.IsForcedInclusion, "IsForcedInclusion")
			assert.Equal(t, tt.origin.Signature, got.Signature, "Signature")
		})
	}
}
