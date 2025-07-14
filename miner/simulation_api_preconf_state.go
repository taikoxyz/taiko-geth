package miner

import (
	"fmt"
	"math"
	"sync"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

type StateId uint64

const (
	LatestSealedId     StateId = 0
	StateIdToStartFrom         = uint64(1) // 0 is reserved for latest state
)

// PreconfState holds all information about pending simulation states.
type PreconfState struct {
	// stateIdMap maps all state IDs to their corresponding environments.
	stateIdMap map[uint64]*environment
	// chain represents the current canonical blockchain.
	chain        *core.BlockChain
	stateIdMutex sync.RWMutex // Mutex for stateIdMap
	// currentStateId is the next stateId to be used
	currentStateId uint64
}

// NewPreconfState initializes a new PreconfState.
// It requires a reference to the canonical blockchain.
func NewPreconfState(chain *core.BlockChain) *PreconfState {
	return &PreconfState{
		chain:          chain,
		stateIdMap:     make(map[uint64]*environment),
		currentStateId: StateIdToStartFrom,
	}
}

func (state *PreconfState) clearStateIdMap() {
	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()
	log.Info("Simulator-WORKER: Clearing state Id map")
	state.stateIdMap = make(map[uint64]*environment)
}

// onNewChainHeadEvent logs when a new canonical chain head is seen
func (state *PreconfState) onNewChainHeadEvent(event *core.ChainHeadEvent) {
	log.Info("Simulator-WORKER: New chain head event", "blockHash", event.Header.Hash())
}

// calculateStateMetrics returns the cumulative gas used and builder payment for a given state ID.
func (state *PreconfState) calculateStateMetrics(stateId uint64) (uint64, *uint256.Int, error) {
	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()

	env, exists := state.stateIdMap[stateId]
	if !exists {
		return 0, nil, fmt.Errorf("state for id %d does not exist", stateId)
	}

	log.Info("Simulator-WORKER: Calculating metrics for state", "stateId", stateId, "blockNumber", env.header.Number.Uint64())

	// Calculate total gas used from header
	totalGas := env.header.GasUsed

	// Calculate builder payment by comparing current balance with initial balance
	builderPayment := new(uint256.Int)
	endBalance := env.state.GetBalance(env.coinbase)
	if endBalance.Cmp(env.initialCoinbaseBalance) > 0 {
		builderPayment.Sub(endBalance, env.initialCoinbaseBalance)
	}

	return totalGas, builderPayment, nil
}

func (state *PreconfState) envAtId(stateId uint64) *environment {
	state.stateIdMutex.RLock()
	defer state.stateIdMutex.RUnlock()

	if env, exists := state.stateIdMap[stateId]; exists {
		return env
	}
	return nil
}

// addEnvironment adds a new environment to the stateIdMap and returns the assigned stateId
func (state *PreconfState) addEnvironment(env *environment) uint64 {
	if env == nil {
		return 0
	}

	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()

	if state.stateIdMap == nil {
		state.stateIdMap = make(map[uint64]*environment)
	}

	// Check for overflow - if we're at max uint64, reset to starting point
	if state.currentStateId == math.MaxUint64 {
		state.currentStateId = StateIdToStartFrom
	}

	stateId := state.currentStateId
	state.currentStateId++
	state.stateIdMap[stateId] = env
	return stateId
}
