package catalyst

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/rawdb"
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
