package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// newTaikoUnzenTestConfig returns a Taiko chain config with every fork up to
// Unzen active from genesis, as the live Taiko chains run after Unzen. Fork
// times are pinned explicitly so upstream additions to the shared test
// configs cannot change what these tests exercise.
func newTaikoUnzenTestConfig() *params.ChainConfig {
	zero := uint64(0)
	config := *params.TaikoChainConfig
	config.ChainID = big.NewInt(167000)
	config.OntakeBlock, config.PacayaBlock, config.ShastaTime = common.Big0, common.Big0, &zero
	config.CancunTime, config.PragueTime, config.OsakaTime, config.UnzenTime = &zero, &zero, &zero, &zero
	return &config
}

// TestTaikoCreateRejectsAddressWithStorage pins the EIP-7610 collision rule on
// a Taiko Unzen chain: CREATE onto an address that has no code and nonce 0 but
// non-empty storage fails with ErrContractAddressCollision. The same CREATE
// onto the address without storage succeeds, so storage is the only reason.
func TestTaikoCreateRejectsAddressWithStorage(t *testing.T) {
	config := newTaikoUnzenTestConfig()
	caller := common.HexToAddress("0x000000000000000000000000000000000000ca11")
	target := crypto.CreateAddress(caller, 0)

	for _, withStorage := range []bool{true, false} {
		statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		statedb.SetBalance(caller, uint256.NewInt(params.Ether), 0)
		if withStorage {
			statedb.SetState(target, common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(1)))
		}
		// Hash the pending storage into the account's storage root, keeping
		// the empty (nonce 0, no code, no balance) target account: EIP-158
		// empty-account deletion is deliberately off here.
		statedb.IntermediateRoot(params.Rules{IsEIP158: false})
		if statedb.GetNonce(caller) != 0 || statedb.GetNonce(target) != 0 || len(statedb.GetCode(target)) != 0 {
			t.Fatal("caller and target must start with nonce 0 and the target without code")
		}

		evm := NewEVM(BlockContext{
			CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
			Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
			BlockNumber: big.NewInt(1),
			Time:        1,
			Random:      &common.Hash{},
		}, statedb, config, Config{})

		_, addr, _, err := evm.Create(caller, []byte{byte(STOP)}, NewGasBudget(100_000, 0), new(uint256.Int))
		if withStorage {
			if !errors.Is(err, ErrContractAddressCollision) {
				t.Fatalf("Create onto an address with storage: err = %v, want %v", err, ErrContractAddressCollision)
			}
			continue
		}
		if err != nil || addr != target {
			t.Fatalf("Create onto an empty address = (%v, %v), want (%v, nil)", addr, err, target)
		}
	}
}
