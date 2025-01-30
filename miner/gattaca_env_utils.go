package miner

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
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
	env, err := g.makeEnv(parent, header, genParams.coinbase, false)
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

// makeEnv creates a new environment for the sealing block.
func (g *GattacaWorker) makeEnv(parent *types.Header, header *types.Header, coinbase common.Address, witness bool) (*environment, error) {
	// Retrieve the parent state to execute on top.
	state, err := g.chain.StateAt(parent.Root)
	if err != nil {
		return nil, err
	}
	if witness {
		bundle, err := stateless.NewWitness(header, g.chain)
		if err != nil {
			return nil, err
		}
		state.StartPrefetcher("miner", bundle)
	}
	// Note the passed coinbase may be different with header.Coinbase.
	return &environment{
		signer:   types.MakeSigner(g.chainConfig, header.Number, header.Time),
		state:    state,
		coinbase: coinbase,
		header:   header,
		witness:  state.Witness(),

		//gattaca
		hashReceipts:             make(map[string]*types.Receipt),
		cumulativeBuilderPayment: new(uint256.Int).SetUint64(0),
		txs:                      make([]*types.Transaction, 0),
	}, nil
}

func (g *GattacaWorker) envFromHead() (*environment, error) {
	currentHead := g.chain.CurrentBlock()
	sealedBlock := g.chain.GetBlockByNumber(currentHead.Number.Uint64())
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
	env.sealedBlock = sealedBlock

	return env, nil
}

// retrieveEnv retrieves the environment associated with the given stateId.
// If stateId equals LatestSealedId, it attempts to fetch the latest sealed environment.
//   - If the latest sealed environment exists and its block number is higher than
//     the current head environment, it returns the sealed environment.
//   - Otherwise, it returns the head environment.
//
// For any other stateId, it looks up the corresponding environment in the stateIdMap.
// If the stateId is not found in the map, it returns an error indicating that the
// stateId is not present.
func (g *GattacaWorker) retrieveEnv(stateId uint64) (*environment, error) {
	if stateId == uint64(LatestSealedId) {
		// stateId 1 fetches the latest sealed env, if present, or the latest chain head env
		env, err := g.envFromHead()
		if err != nil {
			return nil, err
		}
		latestSealedEnv := g.preconfState.latestSealedPreconfEnv()
		if latestSealedEnv != nil && latestSealedEnv.header.Number.Uint64() > env.header.Number.Uint64() {
			return latestSealedEnv, nil
		}

		return env, nil
	} else {
		env, exists := g.preconfState.stateIdMap[stateId]
		if !exists {
			return nil, errors.New(fmt.Sprintf("state not found for id %d", stateId))
		}
		return env, nil
	}
}
