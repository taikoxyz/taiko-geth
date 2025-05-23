package miner

import (
	"fmt"
	"math"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

type StateId uint64

const (
	LatestSealedId     StateId = 0
	StateIdToStartFrom         = uint64(1) // 0 is reserved for latest sealed state
)

// PreconfState holds all information about pending changes that are going to be pre-confirmed.
type PreconfState struct {
	// stateIdMap maps all state IDs to their corresponding environments.
	stateIdMap map[uint64]*environment
	// chain represents the current canonical blockchain.
	chain        *core.BlockChain
	stateIdMutex sync.RWMutex // Mutex for stateIdMap
	// receiptsCache stores the derived receipts in order to not compute them twice
	receiptsCache *lru.Cache[common.Hash, []*types.Receipt]
	// currentStateId is the next stateId to be used
	currentStateId uint64
}

// NewPreconfState initializes a new PreconfState with empty sealed and pending preconf blocks.
// It requires a reference to the canonical blockchain.
func NewPreconfState(chain *core.BlockChain) *PreconfState {
	return &PreconfState{
		chain:      chain,
		stateIdMap: make(map[uint64]*environment),
		// create an lru cache limited to 32 elements
		receiptsCache:  lru.NewCache[common.Hash, []*types.Receipt](32),
		currentStateId: StateIdToStartFrom,
	}
}

func (state *PreconfState) clearStateIdMap() {
	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()
	log.Info("Simulator-WORKER: Clearing state Id map")
	state.stateIdMap = make(map[uint64]*environment)
}

// onNewChainHeadEvent just logs the new chain head event for now
func (state *PreconfState) onNewChainHeadEvent(event *core.ChainHeadEvent) {
	log.Info("Simulator-WORKER: New chain head event",
		"blockHash", event.Header.Hash().String(),
		"txs-hash", len(event.Header.TxHash))
}

// calculateStateMetrics returns the cumulative gas used and builder payment for a given state ID.
func (state *PreconfState) calculateStateMetrics(stateId uint64) (uint64, *uint256.Int, error) {
	state.stateIdMutex.Lock()
	defer state.stateIdMutex.Unlock()

	env, exists := state.stateIdMap[stateId]
	if !exists {
		return 0, nil, fmt.Errorf("state for id %d does not exist", stateId)
	}

	log.Info("Simulator-WORKER: Calculating metrics for state", "stateId", stateId, "blockNumber", env.header.Number.Uint64(), "num_receipts", len(env.receipts))

	totalGas := uint64(0)
	for _, receipt := range env.receipts {
		totalGas += receipt.GasUsed
	}

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

// addEnvironment adds a new environment to the stateIdMap in a thread-safe manner and returns the assigned stateId
func (p *PreconfState) addEnvironment(env *environment) uint64 {
	if env == nil {
		return 0
	}

	p.stateIdMutex.Lock()
	defer p.stateIdMutex.Unlock()

	if p.stateIdMap == nil {
		p.stateIdMap = make(map[uint64]*environment)
	}

	// Check for overflow - if we're at max uint64, reset to starting point
	if p.currentStateId == math.MaxUint64 {
		p.currentStateId = StateIdToStartFrom
	}

	stateId := p.currentStateId
	p.currentStateId++
	p.stateIdMap[stateId] = env
	return stateId
}
