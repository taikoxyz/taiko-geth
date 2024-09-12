package miner

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
	"math/big"
	"time"
)

func (g *GattacaWorker) prepareWork(genParams *generateParams) (*environment, error) {

	// Find the parent block for sealing task
	parent := g.chain.CurrentBlock()
	if genParams.parentHash != (common.Hash{}) {
		block := g.chain.GetBlockByHash(genParams.parentHash)
		if block == nil {
			return nil, fmt.Errorf("missing parent")
		}
		parent = block.Header()
	}
	// Sanity check the timestamp correctness, recap the timestamp
	// to parent+1 if the mutation is allowed.
	timestamp := genParams.timestamp
	if parent.Time >= timestamp {
		// CHANGE(taiko): block.timestamp == parent.timestamp is allowed in Taiko protocol.
		if !g.chainConfig.Taiko {
			if genParams.forceTime {
				return nil, fmt.Errorf("invalid timestamp, parent %d given %d", parent.Time, timestamp)
			}
			timestamp = parent.Time + 1
		} else {
			if parent.Time > timestamp {
				return nil, fmt.Errorf("invalid timestamp, parent %d given %d", parent.Time, timestamp)
			}
		}
	}
	// Construct the sealing block header.
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit, g.config.GasCeil),
		Time:       timestamp,
		Coinbase:   genParams.coinbase,

		// gtc
		Root: parent.Root,
	}
	// Set the extra field.
	if len(g.extra) != 0 {
		header.Extra = g.extra
	}
	// Set the randomness field from the beacon chain if it's available.
	if genParams.random != (common.Hash{}) {
		header.MixDigest = genParams.random
	}
	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	if g.chainConfig.IsLondon(header.Number) {
		if g.chainConfig.Taiko && genParams.baseFeePerGas != nil {
			header.BaseFee = genParams.baseFeePerGas
		} else {
			header.BaseFee = eip1559.CalcBaseFee(g.chainConfig, parent)
			if !g.chainConfig.IsLondon(parent.Number) {
				parentGasLimit := parent.GasLimit * g.chainConfig.ElasticityMultiplier()
				header.GasLimit = core.CalcGasLimit(parentGasLimit, g.config.GasCeil)
			}
		}
	}
	// Apply EIP-4844, EIP-4788.
	if g.chainConfig.IsCancun(header.Number, header.Time) {
		var excessBlobGas uint64
		if g.chainConfig.IsCancun(parent.Number, parent.Time) {
			excessBlobGas = eip4844.CalcExcessBlobGas(*parent.ExcessBlobGas, *parent.BlobGasUsed)
		} else {
			// For the first post-fork block, both parent.data_gas_used and parent.excess_data_gas are evaluated as 0
			excessBlobGas = eip4844.CalcExcessBlobGas(0, 0)
		}
		header.BlobGasUsed = new(uint64)
		header.ExcessBlobGas = &excessBlobGas
		header.ParentBeaconRoot = genParams.beaconRoot
	}
	// Run the consensus preparation with the default or customized consensus engine.
	if err := g.engine.Prepare(g.chain, header); err != nil {
		log.Error("Failed to prepare header for sealing", "err", err)
		return nil, err
	}
	// Could potentially happen if starting to mine in an odd state.
	// Note genParams.coinbase can be different with header.Coinbase
	// since clique algorithm can modify the coinbase field in header.
	env, err := g.makeEnv(parent, header, genParams.coinbase)
	if err != nil {
		log.Error("Failed to create sealing context", "err", err)
		return nil, err
	}
	if header.ParentBeaconRoot != nil {
		context := core.NewEVMBlockContext(header, g.chain, nil)
		vmenv := vm.NewEVM(context, vm.TxContext{}, env.state, g.chainConfig, vm.Config{})
		core.ProcessBeaconBlockRoot(*header.ParentBeaconRoot, vmenv, env.state)
	}
	return env, nil
}

// makeEnv creates a new Environment for the sealing block.
func (g *GattacaWorker) makeEnv(parent *types.Header, header *types.Header, coinbase common.Address) (*environment, error) {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit.
	state, err := g.chain.StateAt(parent.Root)
	if err != nil {
		return nil, err
	}
	state.StartPrefetcher("miner")
	// Note the passed coinbase may be different with header.Coinbase.
	env := &environment{
		signer:                   types.MakeSigner(g.chainConfig, header.Number, header.Time),
		state:                    state,
		coinbase:                 coinbase,
		header:                   header,
		startBalance:             *state.GetBalance(coinbase),
		hashReceipts:             make(map[string]*types.Receipt),
		cumulativeBuilderPayment: 0,
		txHashSet:                make(map[string]struct{}),
		txs:                      make([]*types.Transaction, 0),
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0
	return env, nil
}

func (g *GattacaWorker) envFromHead() (*environment, error) {
	currentHead := g.chain.CurrentBlock()
	envParams := &generateParams{
		timestamp:     uint64(time.Now().Unix()),
		forceTime:     true,
		parentHash:    currentHead.Hash(),
		coinbase:      common.HexToAddress("0xB851706411c088A2c43e6AfCb2794be5dd2f2A51"),
		random:        currentHead.MixDigest,
		noTxs:         false,
		baseFeePerGas: big.NewInt(1),
	}

	env, err := g.prepareWork(envParams)

	if err != nil {
		return nil, err
	}
	env.gasPool = new(core.GasPool).AddGas(30_000_000)
	env.header.GasLimit = 240_250_000
	env.startBalance.Set(env.state.GetBalance(env.coinbase))
	var empty common.Hash
	env.parentHash = empty
	return env, nil
}

func (g *GattacaWorker) retrieveEnv(stateId uint32) (*environment, error) {
	var env *environment
	var err error
	if stateId == 1 {
		env, err = g.envFromHead()
		if err != nil {
			log.Error("Failed  envFromHead", "err", err)
			return nil, err
		}
	} else if stateId == 2 {
		if g.preconfHead != nil {
			env = g.preconfHead
		} else {
			log.Warn("nothing committed yet, retrieving env from chain head")
			env, err = g.envFromHead()
			if err != nil {
				log.Error("Failed  envFromHead", "err", err)
				return nil, err
			}
		}
	} else {
		var exists bool
		env, exists = g.envMap[stateId]
		if !exists {
			return nil, errors.New(fmt.Sprintf("state not found for id %d", stateId))
		}
	}
	return env, err
}

type OverrideAccount struct {
	Nonce     *hexutil.Uint64              `json:"nonce"`
	Code      *hexutil.Bytes               `json:"code"`
	Balance   **hexutil.Big                `json:"balance"`
	State     *map[common.Hash]common.Hash `json:"state"`
	StateDiff *map[common.Hash]common.Hash `json:"stateDiff"`
}

// StateOverride is the collection of overridden accounts.
type StateOverride map[common.Address]OverrideAccount

// Apply overrides the fields of specified accounts into the given state.
func (diff *StateOverride) Apply(state *state.StateDB) error {
	if diff == nil {
		return nil
	}
	for addr, account := range *diff {
		// Override account nonce.
		if account.Nonce != nil {
			state.SetNonce(addr, uint64(*account.Nonce))
		}
		// Override account(contract) code.
		if account.Code != nil {
			state.SetCode(addr, *account.Code)
		}
		// Override account balance.
		if account.Balance != nil {
			u256Balance, _ := uint256.FromBig((*big.Int)(*account.Balance))
			state.SetBalance(addr, u256Balance)
		}
		if account.State != nil && account.StateDiff != nil {
			return fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
		}
		// Replace entire state if caller requires.
		if account.State != nil {
			state.SetStorage(addr, *account.State)
		}
		// Apply state diff into specified accounts.
		if account.StateDiff != nil {
			for key, value := range *account.StateDiff {
				state.SetState(addr, key, value)
			}
		}
	}
	// Now finalize the changes. Finalize is normally performed between transactions.
	// By using finalize, the overrides are semantically behaving as
	// if they were created in a transaction just before the tracing occur.
	state.Finalise(false)
	return nil
}
