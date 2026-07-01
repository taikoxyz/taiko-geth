package state

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// CHANGE(taiko): verifies tx-list execution witnesses capture unhashed
// account and storage-slot key preimages.
func TestWitnessCapturesUnhashedKeys(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	slot := common.HexToHash("0x000000000000000000000000000000000000000000000000000000000000beef")

	// Seed a committed account with one storage slot.
	db := NewDatabaseForTesting()
	seed, _ := New(types.EmptyRootHash, db)
	seed.SetBalance(addr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
	seed.SetState(addr, slot, common.HexToHash("0x1"))
	seed.SetCode(addr, []byte{0x00}, tracing.CodeChangeUnspecified)
	root, _ := seed.Commit(1, true, false)

	// Re-open at that root with a witness attached and read the account + slot.
	sdb, _ := New(root, db)
	w := &stateless.Witness{Headers: []*types.Header{{Root: root}}}
	sdb.StartPrefetcher("test", w)
	defer sdb.StopPrefetcher()

	_ = sdb.GetBalance(addr)     // account read -> address key
	_ = sdb.GetState(addr, slot) // storage read -> slot key

	if _, ok := w.Keys[string(addr.Bytes())]; !ok {
		t.Fatalf("account address preimage not captured")
	}
	if _, ok := w.Keys[string(slot.Bytes())]; !ok {
		t.Fatalf("storage slot preimage not captured")
	}
}
