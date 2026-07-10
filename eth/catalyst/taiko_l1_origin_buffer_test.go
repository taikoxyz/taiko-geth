package catalyst

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

func TestPendingL1OriginsTakeReturnsStashedEntryOnce(t *testing.T) {
	var pending pendingL1Origins
	hash := common.HexToHash("0x01")
	pending.stash(hash, samplePendingL1Origin(7, big.NewInt(3)))

	taken, ok := pending.take(hash)
	require.True(t, ok, "entry must be present")
	assert.Equal(t, big.NewInt(7), taken.l1Origin.BlockID)
	assert.Equal(t, big.NewInt(3), taken.batchID)

	_, ok = pending.take(hash)
	assert.False(t, ok, "entry must be removed after take")
}

func TestPendingL1OriginsTakeUnknownHashIsMiss(t *testing.T) {
	var pending pendingL1Origins
	_, ok := pending.take(common.HexToHash("0x01"))
	assert.False(t, ok)
}

func TestPendingL1OriginsStashReplacesEntryForSameHash(t *testing.T) {
	var pending pendingL1Origins
	hash := common.HexToHash("0x01")
	pending.stash(hash, samplePendingL1Origin(7, big.NewInt(1)))
	pending.stash(hash, samplePendingL1Origin(7, big.NewInt(2)))

	taken, ok := pending.take(hash)
	require.True(t, ok, "entry must be present")
	assert.Equal(t, big.NewInt(2), taken.batchID)

	_, ok = pending.take(hash)
	assert.False(t, ok, "replacement must not leave a duplicate")
}

func TestPendingL1OriginsStashEvictsOldestBeyondCapacity(t *testing.T) {
	var pending pendingL1Origins
	for i := 0; i <= pendingL1OriginsCapacity; i++ {
		hash := common.BigToHash(big.NewInt(int64(i + 1)))
		pending.stash(hash, samplePendingL1Origin(int64(i), nil))
	}

	_, ok := pending.take(common.BigToHash(big.NewInt(1)))
	assert.False(t, ok, "oldest entry must be evicted")

	_, ok = pending.take(common.BigToHash(big.NewInt(2)))
	assert.True(t, ok, "newer entries must survive")
}
