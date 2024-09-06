package miner

import (
	"errors"
	"fmt"
	ckzg4844 "github.com/ethereum/c-kzg-4844/bindings/go"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/google/uuid"
	"github.com/holiman/uint256"
	"math/big"
	"os"
	"sync"
	"time"
)

var (
	SimCh             = make(chan SimulateTxRequest, 1000)
	CommitCh          = make(chan ReqCommitState, 1000)
	SealBlock         = make(chan SealBlockRequest, 1)
	taikoMinTip       = big.NewInt(0)
	maxBytesPerTxList = ckzg4844.BytesPerBlob
)

var (
	singletonGattaca *GattacaWorker = nil
	singletonLock                   = &sync.Mutex{}
)

type SimulateTxRequest struct {
	RawTx   []byte                  `json:"tx"`
	StateId uint32                  `json:"stateId"`
	Tx      *types.Transaction      `json:"-"`
	SimRes  chan SimulationResponse `json:"-"`
}

type SealBlockResponse struct {
	block                    *types.Block
	cumulativeBuilderPayment string
	err                      error
}

func (s SealBlockResponse) Block() *types.Block {
	return s.block
}

func (s SealBlockResponse) CumulativeBuilderPayment() string {
	return s.cumulativeBuilderPayment
}

func (s SealBlockResponse) Err() error {
	return s.err
}

type SealBlockRequest struct {
	StateId  uint32 `json:"stateId"`
	Response chan SealBlockResponse
}

type ReqCommitState struct {
	StateId uint32                   `json:"stateId"`
	SimRes  chan CommitStateResponse `json:"-"`
}

type InnerCommitState struct {
	StateId   uint32
	commitRes chan SimulationResponse
}

type SimulationResponse struct {
	stateId        uint32
	error          error
	gasUsed        uint64
	builderPayment string
}

type CommitStateResponse struct {
	cumulativeGasUsed        uint64
	cumulativeBuilderPayment uint256.Int
	error                    error
}

func (c CommitStateResponse) CumulativeGasUsed() uint64 {
	return c.cumulativeGasUsed
}

func (c CommitStateResponse) CumulativeBuilderPayment() string {
	return c.cumulativeBuilderPayment.Hex()
}

func (c CommitStateResponse) Error() error {
	return c.error
}

func (s SimulationResponse) Error() error {
	return s.error
}

func (s SimulationResponse) StateId() uint32 {
	return s.stateId
}

func (s SimulationResponse) GasUsed() uint64 {
	return s.gasUsed
}

func (s SimulationResponse) BuilderPayment() string {
	return s.builderPayment
}

type GattacaWorker struct {
	chainConfig      *params.ChainConfig
	chain            *core.BlockChain
	config           *Config
	engine           consensus.Engine
	extra            []byte
	lock             sync.RWMutex
	commitMutex      sync.Mutex
	envMap           map[uint32]*environment
	envBuilder       map[uint32][]uint32
	preconfHead      *environment
	halt             bool
	haltReason       string
	builtBlocks      []types.Block
	startBlockNumber uint64
}

func NewGattacaWorker(chainConfig *params.ChainConfig, chain *core.BlockChain, config *Config, engine consensus.Engine) (*GattacaWorker, error) {
	singletonLock.Lock()
	defer singletonLock.Unlock()
	if singletonGattaca == nil {

		singletonGattaca = &GattacaWorker{
			chainConfig: chainConfig,
			chain:       chain,
			config:      config,
			engine:      engine,
			extra:       config.ExtraData,
			envMap:      make(map[uint32]*environment),
			envBuilder:  make(map[uint32][]uint32),
			preconfHead: nil,
			halt:        false,
			haltReason:  "",
			commitMutex: sync.Mutex{},
			builtBlocks: make([]types.Block, 0),
		}
		env, err := singletonGattaca.retrieveEnv(1)
		if err != nil {
			log.Error("Failed to retrieve environment", "err", err)
			return nil, err
		}
		singletonGattaca.preconfHead = env
		singletonGattaca.startBlockNumber = singletonGattaca.chain.CurrentBlock().Number.Uint64()
		go singletonGattaca.runLoop()
		go singletonGattaca.newHeadEventSubscriber()
	}
	return singletonGattaca, nil
}

func GetWorker(maxRetry uint) *GattacaWorker {
	if os.Getenv("GATTACA_OVERRIDE") == "" {
		return nil
	}
	for i := uint(0); i < maxRetry; i++ {
		singletonLock.Lock()
		if singletonGattaca != nil {
			singletonLock.Unlock()
			return singletonGattaca
		}
		singletonLock.Unlock()
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func (g *GattacaWorker) runLoop() {
	for {
		select {
		case req := <-SimCh:
			log.Debug("run simulation for tx hash ", req.Tx.Hash().String())
			go g.simulateTx(req.StateId, req.Tx, req.SimRes)
			break
		case req := <-CommitCh:
			log.Debug("run commit state ", req.StateId)
			go g.commitEnvToPreconf(req.StateId, req.SimRes)
		case req := <-SealBlock:
			log.Info("run seal block ", req.StateId)
			go g.sealBlock(req)
		}
	}
}

func (g *GattacaWorker) newHeadEventSubscriber() {
	newBlockCh := make(chan core.ChainHeadEvent, 10)
	sub := g.chain.SubscribeChainHeadEvent(newBlockCh)
	defer sub.Unsubscribe()
	for {
		select {
		case ev := <-newBlockCh:
			block := ev.Block
			log.Info("new head block ", "blockNumber", block.NumberU64())
		case err := <-sub.Err():
			if err != nil {
				log.Error("Error in block subscription", "err", err)
			}
			break
		}
	}
}

func (g *GattacaWorker) simulateTx(stateId uint32, tx *types.Transaction, res chan SimulationResponse) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	if g.halt {
		res <- SimulationResponse{
			error: NewHaltError(errors.New(g.haltReason)),
		}
		return
	}
	env, err := g.retrieveEnv(stateId)
	if err != nil {
		res <- SimulationResponse{
			error:   NewRetrieveEnError(err),
			gasUsed: 0,
		}
		return
	}
	simEnv := env.copy()
	receipt, _, _, err := g.commitTx(simEnv, tx)
	if err != nil {
		var gasUsed uint64
		var commitError CommitError
		if errors.As(err, &commitError) {
			gasUsed = simEnv.gasPool.Gas()
		}
		log.Error("Failed to simulate transaction", "err", err)
		res <- SimulationResponse{
			error:   NewCommitError(err),
			gasUsed: gasUsed,
		}
		return
	}
	var newStateId uint32
	newStateId = uuid.New().ID()
	simEnv.hashReceipts[tx.Hash().Hex()] = receipt
	simEnv.cumulativeBuilderPayment.Sub(simEnv.state.GetBalance(env.coinbase), &simEnv.startBalance)
	g.envMap[newStateId] = simEnv
	simEnv.receipts = append(simEnv.receipts, receipt)
	if stateId < 100 {
		g.envBuilder[newStateId] = make([]uint32, 0)
	} else {
		prevBuild := g.envBuilder[stateId]
		g.envBuilder[newStateId] = append(prevBuild, newStateId)
	}
	res <- SimulationResponse{
		error:          nil,
		gasUsed:        receipt.GasUsed,
		stateId:        newStateId,
		builderPayment: simEnv.cumulativeBuilderPayment.Hex(),
	}
}

func (g *GattacaWorker) commitEnvToPreconf(stateId uint32, simRes chan CommitStateResponse) {
	g.commitMutex.Lock()
	defer g.commitMutex.Unlock()
	env, exists := g.envMap[stateId]
	if !exists {
		simRes <- CommitStateResponse{
			error: errors.New(fmt.Sprintf("cache for id %d does not exists", stateId)),
		}
		return
	}
	if g.preconfHead == nil {
		var err error
		env, err = g.envFromHead()
		if err != nil {
			simRes <- CommitStateResponse{
				error: err,
			}
			return
		}
		g.preconfHead = env.copy()
	}
	var cumulativeGasUsed uint64
	for _, tx := range env.txs {
		receipt, _, _, err := g.commitTx(g.preconfHead, tx)
		if err != nil {
			log.Error("error committing transaction to head ", "hash", tx.Hash().Hex(), "error", err)
			simRes <- CommitStateResponse{
				error: err,
			}
			return
		}
		g.preconfHead.hashReceipts[tx.Hash().Hex()] = receipt
		cumulativeGasUsed += receipt.GasUsed
	}

	var cumulativeBuilderPayment uint256.Int
	cumulativeBuilderPayment.Sub(g.preconfHead.state.GetBalance(env.coinbase), &env.startBalance)
	simRes <- CommitStateResponse{
		cumulativeGasUsed:        cumulativeGasUsed,
		cumulativeBuilderPayment: cumulativeBuilderPayment,
	}
}

func (g *GattacaWorker) sealBlock(req SealBlockRequest) {
	g.lock.Lock()
	defer g.lock.Unlock()
	cumulativeBuilderPayment := g.preconfHead.cumulativeBuilderPayment.Clone()
	blkNumber := uint64(1) + g.startBlockNumber + uint64(len(g.builtBlocks))
	g.preconfHead.header.Number.Set(big.NewInt(int64(blkNumber)))
	// Create a new block using the current preconfHead values.
	block := types.NewBlock(g.preconfHead.header, g.preconfHead.txs, nil, g.preconfHead.receipts, trie.NewStackTrie(nil))
	g.builtBlocks = append(g.builtBlocks, *block)
	g.preconfHead.reset()
	block.Hash()
	// Send the response back indicating success.
	req.Response <- SealBlockResponse{
		block:                    block,
		cumulativeBuilderPayment: cumulativeBuilderPayment.Hex(),
		err:                      nil,
	}
}

func (g *GattacaWorker) commitTx(env *environment, tx *types.Transaction) (*types.Receipt, *uint256.Int, uint64, error) {

	if env.gasPool.Gas() < params.TxGas {
		log.Info("PRECONF: Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
		return nil, nil, 0, errors.New("not enough gas for further transactions")
	}

	// Optional min tip.
	if taikoMinTip != nil {
		if tx.GasTipCapIntCmp(taikoMinTip) < 0 {
			log.Info("PRECONF: Ignoring transaction with low tip", "hash", tx.Hash(), "tip", tx.GasTipCap(), "minTip", taikoMinTip)
			return nil, nil, 0, errors.New("ignoring transaction with low tip")
		}
	}

	// Check whether the tx is replay protected. If we're not in the EIP155 hf
	// phase, start ignoring the sender until we do.
	if tx.Protected() && !g.chainConfig.IsEIP155(env.header.Number) {
		log.Info("PRECONF: Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", g.chainConfig.EIP155Block)
		return nil, nil, 0, errors.New("ignoring reply protected transaction")
	}

	// Encode and compress the txList, if the byte length is > maxBytesPerTxList, we cannot include it.
	compressedBytes, err := encodeAndCompressTxList(append(env.txs, tx))
	if err != nil {
		return nil, nil, 0, err
	}
	compressedBytesLen := len(compressedBytes)
	if compressedBytesLen > int(maxBytesPerTxList) {
		return nil, nil, 0, errors.New("reached maxBytesPerTxList")
	}

	// Execute the transaction and return the result.
	env.state.SetTxContext(tx.Hash(), env.tcount)
	receipt, err := g.commitPreconfTransaction(env, tx)
	if err != nil {
		return nil, nil, 0, err
	}

	log.Info("PRECONF: simulated tx.", "was success", receipt != nil)

	// Fetch nonce and balance
	signer := types.MakeSigner(g.chainConfig, env.header.Number, env.header.Time)
	sender, err := signer.Sender(tx)
	if err != nil {
		log.Error("PRECONF: failed to recover sender", "err", err)
		return nil, nil, 0, err
	}

	nonce := env.state.GetNonce(sender)
	balance := env.state.GetBalance(sender)

	return receipt, balance, nonce, err
}

func (g *GattacaWorker) commitPreconfTransaction(env *environment, tx *types.Transaction) (*types.Receipt, error) {

	if tx.Type() == types.BlobTxType {
		return g.commitPreconfBlobTransaction(env, tx)
	}
	receipt, err := g.applyTransaction(env, tx)
	if err != nil {
		return nil, err
	}
	env.txs = append(env.txs, tx)
	return receipt, nil
}

func (g *GattacaWorker) commitPreconfBlobTransaction(env *environment, tx *types.Transaction) (*types.Receipt, error) {
	sc := tx.BlobTxSidecar()
	if sc == nil {
		return nil, errors.New("blob transaction without blobs in miner")
	}
	// Checking against blob gas limit: It's kind of ugly to perform this check here, but there
	// isn't really a better place right now. The blob gas limit is checked at block validation time
	// and not during execution. This means core.ApplyTransaction will not return an error if the
	// tx has too many blobs. So we have to explicitly check it here.
	if (env.blobs+len(sc.Blobs))*params.BlobTxBlobGasPerBlob > params.MaxBlobGasPerBlock {
		return nil, errors.New("max data blobs reached")
	}
	receipt, err := g.applyTransaction(env, tx)
	if err != nil {
		return nil, err
	}
	env.txs = append(env.txs, tx.WithoutBlobTxSidecar())
	env.sidecars = append(env.sidecars, sc)
	env.blobs += len(sc.Blobs)
	*env.header.BlobGasUsed += receipt.BlobGasUsed
	return receipt, nil
}

func (g *GattacaWorker) applyTransaction(env *environment, tx *types.Transaction) (*types.Receipt, error) {
	var (
		snap = env.state.Snapshot()
		gp   = env.gasPool.Gas()
	)
	receipt, err := core.ApplyTransaction(g.chainConfig, g.chain, &env.coinbase, env.gasPool, env.state, env.header, tx, &env.header.GasUsed, *g.chain.GetVMConfig())
	if err != nil {
		env.state.RevertToSnapshot(snap)
		env.gasPool.SetGas(gp)
		err = NewRevertCommitError(err)
	}
	return receipt, err
}

func (g *GattacaWorker) GetStateAndHeader() (*state.StateDB, *types.Header) {
	return g.preconfHead.state.Copy(), g.preconfHead.header
}
