package catalyst

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// samplePendingL1Origin builds a deterministic pending entry for buffer tests.
func samplePendingL1Origin(blockID int64, batchID *big.Int) pendingL1Origin {
	return pendingL1Origin{
		l1Origin: &rawdb.L1Origin{
			BlockID:       big.NewInt(blockID),
			L2BlockHash:   common.HexToHash("0xaa"),
			L1BlockHeight: big.NewInt(100),
			L1BlockHash:   common.HexToHash("0xbb"),
		},
		batchID: batchID,
	}
}

func TestPendingL1OriginsGetRetainsEntry(t *testing.T) {
	var pending pendingL1Origins
	hash := common.HexToHash("0x01")
	pending.stash(hash, samplePendingL1Origin(7, big.NewInt(3)))

	first, ok := pending.get(hash)
	require.True(t, ok, "entry must be present")
	assert.Equal(t, big.NewInt(7), first.l1Origin.BlockID)
	assert.Equal(t, big.NewInt(3), first.batchID)

	second, ok := pending.get(hash)
	require.True(t, ok, "lookup must retain the entry")
	assert.Same(t, first.l1Origin, second.l1Origin)
}

func TestPendingL1OriginsGetUnknownHashIsMiss(t *testing.T) {
	var pending pendingL1Origins
	_, ok := pending.get(common.HexToHash("0x01"))
	assert.False(t, ok)
}

func TestPendingL1OriginsValidateTimestamp(t *testing.T) {
	var pending pendingL1Origins
	confirmedHash := common.HexToHash("0x01")
	pending.stash(confirmedHash, samplePendingL1Origin(7, nil))
	assert.ErrorIs(t, pending.validateTimestamp(confirmedHash, 101, 100), consensus.ErrFutureBlock)

	preconfHash := common.HexToHash("0x02")
	preconf := samplePendingL1Origin(8, nil)
	preconf.l1Origin.L1BlockHeight = common.Big0
	pending.stash(preconfHash, preconf)
	assert.NoError(t, pending.validateTimestamp(preconfHash, 101, 100))

	assert.NoError(t, pending.validateTimestamp(common.HexToHash("0x03"), 101, 100))
}

func TestPendingL1OriginsStashReplacesEntryForSameHash(t *testing.T) {
	var pending pendingL1Origins
	hash := common.HexToHash("0x01")
	pending.stash(hash, samplePendingL1Origin(7, big.NewInt(1)))
	pending.stash(hash, samplePendingL1Origin(7, big.NewInt(2)))

	taken, ok := pending.get(hash)
	require.True(t, ok, "entry must be present")
	assert.Equal(t, big.NewInt(2), taken.batchID)
}

func TestPendingL1OriginsStashEvictsOldestBeyondCapacity(t *testing.T) {
	var pending pendingL1Origins
	for i := 0; i <= pendingL1OriginsCapacity; i++ {
		hash := common.BigToHash(big.NewInt(int64(i + 1)))
		pending.stash(hash, samplePendingL1Origin(int64(i), nil))
	}

	_, ok := pending.get(common.BigToHash(big.NewInt(1)))
	assert.False(t, ok, "oldest entry must be evicted")

	_, ok = pending.get(common.BigToHash(big.NewInt(2)))
	assert.True(t, ok, "newer entries must survive")
}

func stashPendingOrigin(pending *pendingL1Origins, blockID int64, blockHash common.Hash, batchID int64) {
	entry := samplePendingL1Origin(blockID, big.NewInt(batchID))
	entry.l1Origin.L2BlockHash = blockHash
	pending.stash(blockHash, entry)
}

func TestReconcileL1OriginTablesSiblingReorgAndBack(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	var pending pendingL1Origins
	hashA := common.HexToHash("0xa7")
	hashB := common.HexToHash("0xb7")
	stashPendingOrigin(&pending, 7, hashA, 100)
	stashPendingOrigin(&pending, 7, hashB, 101)

	assertActive := func(hash common.Hash, activeBatch, displacedBatch int64) {
		t.Helper()
		origin, err := rawdb.ReadL1Origin(db, big.NewInt(7))
		require.NoError(t, err)
		require.NotNil(t, origin)
		assert.Equal(t, hash, origin.L2BlockHash)
		head, err := rawdb.ReadHeadL1Origin(db)
		require.NoError(t, err)
		assert.Equal(t, int64(7), head.Int64())
		mapped, err := rawdb.ReadBatchToLastBlockID(db, big.NewInt(activeBatch))
		require.NoError(t, err)
		require.NotNil(t, mapped)
		assert.Equal(t, int64(7), (*big.Int)(mapped).Int64())
		mapped, err = rawdb.ReadBatchToLastBlockID(db, big.NewInt(displacedBatch))
		require.NoError(t, err)
		assert.Nil(t, mapped)
	}

	rawdb.WriteCanonicalHash(db, hashA, 7)
	require.NoError(t, reconcileL1OriginTables(db, &pending, 7, 7, []canonicalL1OriginBlock{{number: 7, hash: hashA}}))
	assertActive(hashA, 100, 101)

	rawdb.WriteCanonicalHash(db, hashB, 7)
	require.NoError(t, reconcileL1OriginTables(db, &pending, 7, 7, []canonicalL1OriginBlock{{number: 7, hash: hashB}}))
	assertActive(hashB, 101, 100)

	rawdb.WriteCanonicalHash(db, hashA, 7)
	require.NoError(t, reconcileL1OriginTables(db, &pending, 7, 7, []canonicalL1OriginBlock{{number: 7, hash: hashA}}))
	assertActive(hashA, 100, 101)
}

func TestReconcileL1OriginTablesRewind(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	var pending pendingL1Origins
	canonical := make([]canonicalL1OriginBlock, 0, 3)
	for number := int64(7); number <= 9; number++ {
		hash := common.BigToHash(big.NewInt(number))
		stashPendingOrigin(&pending, number, hash, 100+number)
		rawdb.WriteCanonicalHash(db, hash, uint64(number))
		canonical = append(canonical, canonicalL1OriginBlock{number: uint64(number), hash: hash})
	}
	require.NoError(t, reconcileL1OriginTables(db, &pending, 7, 9, canonical))

	rawdb.DeleteCanonicalHash(db, 8)
	rawdb.DeleteCanonicalHash(db, 9)
	require.NoError(t, reconcileL1OriginTables(db, &pending, 8, 9, nil))

	for number := int64(8); number <= 9; number++ {
		origin, err := rawdb.ReadL1Origin(db, big.NewInt(number))
		require.NoError(t, err)
		assert.Nil(t, origin)
		mapped, err := rawdb.ReadBatchToLastBlockID(db, big.NewInt(100+number))
		require.NoError(t, err)
		assert.Nil(t, mapped)
	}
	head, err := rawdb.ReadHeadL1Origin(db)
	require.NoError(t, err)
	require.NotNil(t, head)
	assert.Equal(t, int64(7), head.Int64())
}

func TestReconcileL1OriginTablesMissingMetadataClearsStaleRows(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	var pending pendingL1Origins
	hashA := common.HexToHash("0xa7")
	hashB := common.HexToHash("0xb7")
	stashPendingOrigin(&pending, 7, hashA, 100)
	rawdb.WriteCanonicalHash(db, hashA, 7)
	require.NoError(t, reconcileL1OriginTables(db, &pending, 7, 7, []canonicalL1OriginBlock{{number: 7, hash: hashA}}))

	rawdb.WriteCanonicalHash(db, hashB, 7)
	require.NoError(t, reconcileL1OriginTables(db, &pending, 7, 7, []canonicalL1OriginBlock{{number: 7, hash: hashB}}))

	origin, err := rawdb.ReadL1Origin(db, big.NewInt(7))
	require.NoError(t, err)
	assert.Nil(t, origin)
	mapped, err := rawdb.ReadBatchToLastBlockID(db, big.NewInt(100))
	require.NoError(t, err)
	assert.Nil(t, mapped)
	head, err := rawdb.ReadHeadL1Origin(db)
	require.NoError(t, err)
	assert.Nil(t, head)
}

func TestCanonicalL1OriginSegment(t *testing.T) {
	genesis := &types.Header{Number: big.NewInt(0), Extra: []byte("genesis")}
	a1 := &types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1), Extra: []byte("a1")}
	a2 := &types.Header{ParentHash: a1.Hash(), Number: big.NewInt(2), Extra: []byte("a2")}
	b1 := &types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1), Extra: []byte("b1")}
	b2 := &types.Header{ParentHash: b1.Hash(), Number: big.NewInt(2), Extra: []byte("b2")}
	headers := map[common.Hash]*types.Header{
		genesis.Hash(): genesis,
		a1.Hash():      a1,
		a2.Hash():      a2,
		b1.Hash():      b1,
		b2.Hash():      b2,
	}
	lookup := func(hash common.Hash) *types.Header { return headers[hash] }

	tests := []struct {
		name      string
		oldHead   *types.Header
		newHead   *types.Header
		first     uint64
		last      uint64
		canonical []canonicalL1OriginBlock
	}{
		{"forward", a1, a2, 2, 2, []canonicalL1OriginBlock{{number: 2, hash: a2.Hash()}}},
		{"rewind", a2, genesis, 1, 2, nil},
		{"sibling", a1, b1, 1, 1, []canonicalL1OriginBlock{{number: 1, hash: b1.Hash()}}},
		{"multi block", a2, b2, 1, 2, []canonicalL1OriginBlock{{number: 1, hash: b1.Hash()}, {number: 2, hash: b2.Hash()}}},
		{"idempotent", a2, a2, 2, 2, []canonicalL1OriginBlock{{number: 2, hash: a2.Hash()}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, last, canonical, err := canonicalL1OriginSegment(tt.oldHead, tt.newHead, lookup)
			require.NoError(t, err)
			assert.Equal(t, tt.first, first)
			assert.Equal(t, tt.last, last)
			assert.Equal(t, tt.canonical, canonical)
		})
	}
}
