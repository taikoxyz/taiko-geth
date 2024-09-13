package miner

import (
	"encoding/binary"
	"errors"
	"fmt"
	ckzg4844 "github.com/ethereum/c-kzg-4844/bindings/go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/google/uuid"
	"github.com/holiman/uint256"
	"golang.org/x/crypto/sha3"
	"math/big"
	"os"
	"sync"
	"time"
)

var (
	SimCh             = make(chan SimulateTxRequest, 1000)
	SimAnchorTx       = make(chan SimulateAnchorTx, 1000)
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

type SimulateAnchorTx struct {
	Tx        *types.Transaction `json:"-"`
	Timestamp uint64
	BaseFee   uint64
	MixHash   common.Hash
	SimRes    chan SimulationResponse `json:"-"`
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
	cumulativeBuilderPayment string
	error                    error
}

type inMemoryStore struct {
	block *types.Block
	env   *environment
}

func (c CommitStateResponse) CumulativeGasUsed() uint64 {
	return c.cumulativeGasUsed
}

func (c CommitStateResponse) CumulativeBuilderPayment() string {
	return c.cumulativeBuilderPayment
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
	builtBlocks      []inMemoryStore
	startBlockNumber uint64
	sequencing       int32
	mapBlockNumber   map[int64]inMemoryStore
	mapBlockHash     map[string]inMemoryStore

	mixHash common.Hash
}

func NewGattacaWorker(chainConfig *params.ChainConfig, chain *core.BlockChain, config *Config, engine consensus.Engine) (*GattacaWorker, error) {
	singletonLock.Lock()
	defer singletonLock.Unlock()
	if singletonGattaca == nil {

		singletonGattaca = &GattacaWorker{
			chainConfig:    chainConfig,
			chain:          chain,
			config:         config,
			engine:         engine,
			extra:          config.ExtraData,
			envMap:         make(map[uint32]*environment),
			envBuilder:     make(map[uint32][]uint32),
			preconfHead:    nil,
			halt:           false,
			haltReason:     "",
			commitMutex:    sync.Mutex{},
			builtBlocks:    make([]inMemoryStore, 0),
			mapBlockNumber: make(map[int64]inMemoryStore),
			mapBlockHash:   make(map[string]inMemoryStore),
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
		case req := <-SimAnchorTx:
			go g.simulateAnchorTx(req.Tx, req.Timestamp, req.BaseFee, req.MixHash, req.SimRes)
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
			var localBlock inMemoryStore
			found := false
			for _, localBlock = range g.builtBlocks {
				if localBlock.block.NumberU64() == block.NumberU64() {
					found = true
					break
				}
			}
			if found {
				if len(localBlock.block.Transactions()) != len(block.Transactions()) {
					log.Crit("blocks txs length differ between in memory block and block received")
				}
				localTxs := localBlock.block.Transactions()
				receivedTxs := block.Transactions()
				for i := 1; i < len(localBlock.block.Transactions()); i++ {
					if localTxs[i].Hash().Hex() != receivedTxs[i].Hash().Hex() {
						log.Crit("transaction hash differs.", "local hash", localTxs[i].Hash().Hex(), "received hash", receivedTxs[i].Hash().Hex())
					}
				}
				delete(g.mapBlockNumber, int64(block.NumberU64()))
				delete(g.mapBlockHash, block.Hash().Hex())
			}
		case err := <-sub.Err():
			if err != nil {
				log.Error("Error in block subscription", "err", err)
			}
			break
		}
	}
}

func (g *GattacaWorker) simulateAnchorTx(tx *types.Transaction, timestamp uint64, baseFee uint64, mixHash common.Hash, res chan SimulationResponse) {
	env, err := g.retrieveEnv(2)
	if err != nil {
		res <- SimulationResponse{
			stateId:        0,
			error:          errors.New(fmt.Sprintf("failed to retrieve environment. err: %s", err.Error())),
			gasUsed:        0,
			builderPayment: "0x0",
		}
		return
	}
	simEnv := env.copy()
	env.header.Time = timestamp
	env.header.BaseFee = big.NewInt(int64(baseFee))

	signer := types.MakeSigner(g.chainConfig, env.header.Number, env.header.Time)
	from, err := types.Sender(signer, tx)
	if err != nil {
		log.Error("error retrieving sender address from transaction", "err", err.Error())
		res <- SimulationResponse{
			stateId:        0,
			error:          errors.New("failed to retrieve sender address from transaction"),
			gasUsed:        0,
			builderPayment: "0x0",
		}
		return
	}
	if len(env.txs) > 0 {
		log.Error("anchor tx needs to be the first committed transaction")
		res <- SimulationResponse{
			stateId:        0,
			error:          errors.New("anchor tx needs to be the first committed transaction"),
			gasUsed:        0,
			builderPayment: "0x0",
		}
		return
	}
	if from.Hex() != "0x0000777735367b36bC9B61C50022d9D0700dB4Ec" {
		log.Error("first transaction must come from GoldenTouchAccount")
		res <- SimulationResponse{
			stateId:        0,
			error:          errors.New("first transaction must come from GoldenTouchAccount"),
			gasUsed:        0,
			builderPayment: "0x0",
		}
		return
	}

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
	simEnv.receipts = append(simEnv.receipts, receipt)
	g.envMap[newStateId] = simEnv
	g.envBuilder[newStateId] = make([]uint32, 0)
	g.mixHash = mixHash

	res <- SimulationResponse{
		error:          nil,
		gasUsed:        receipt.GasUsed,
		stateId:        newStateId,
		builderPayment: "0x0",
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
	if len(simEnv.txs) == 0 {
		res <- SimulationResponse{
			stateId:        0,
			error:          errors.New(fmt.Sprintf("first transaction needs to executed by simulateAnchorAtState. StateId %d", stateId)),
			gasUsed:        0,
			builderPayment: "0x0",
		}
		return
	}
	startBalance := simEnv.state.GetBalance(env.coinbase).Uint64()
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
	endBalance := simEnv.state.GetBalance(env.coinbase).Uint64()
	var newStateId uint32
	newStateId = uuid.New().ID()
	simEnv.hashReceipts[tx.Hash().Hex()] = receipt
	builderPayment := endBalance - startBalance
	simEnv.cumulativeBuilderPayment += builderPayment
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
		builderPayment: fmt.Sprintf("0x%x", builderPayment),
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
		headEnv, err := g.envFromHead()
		if err != nil {
			simRes <- CommitStateResponse{
				error: err,
			}
			return
		}
		g.preconfHead = headEnv.copy()
	}
	var cumulativeGasUsed uint64
	for _, tx := range env.txs {
		if _, in := g.preconfHead.txHashSet[tx.Hash().Hex()]; !in {
			receipt, _, _, err := g.commitTx(g.preconfHead, tx)
			if err != nil {
				log.Error("error committing transaction to head ", "hash", tx.Hash().Hex(), "error", err)
				simRes <- CommitStateResponse{
					error: err,
				}
				return
			}
			g.preconfHead.receipts = append(g.preconfHead.receipts, receipt)
			g.preconfHead.hashReceipts[tx.Hash().Hex()] = receipt
			g.preconfHead.txHashSet[tx.Hash().Hex()] = struct{}{}
			cumulativeGasUsed += receipt.GasUsed
		}
	}

	simRes <- CommitStateResponse{
		cumulativeGasUsed:        cumulativeGasUsed,
		cumulativeBuilderPayment: fmt.Sprintf("0x%x", g.preconfHead.cumulativeBuilderPayment),
	}
}

func (g *GattacaWorker) sealBlock(req SealBlockRequest) {
	g.lock.Lock()
	defer g.lock.Unlock()

	chainHead, err := g.retrieveEnv(1)
	if err != nil {
		log.Crit("error getting chain head")
		req.Response <- SealBlockResponse{
			block:                    nil,
			cumulativeBuilderPayment: "",
			err:                      err,
		}
		return
	}
	var empty common.Hash
	log.Info("env parent Hash", "hash", g.preconfHead.parentHash.Hex())
	if g.preconfHead.parentHash.Hex() != empty.Hex() {
		g.preconfHead.header.ParentHash = g.preconfHead.parentHash
	}
	chainNo := chainHead.header.Number.Uint64()
	headerNo := g.preconfHead.header.Number.Uint64()

	if len(g.builtBlocks) > 0 {
		lastBlockNumber := g.builtBlocks[len(g.builtBlocks)-1].block.NumberU64() + 1
		if lastBlockNumber > chainNo {
			g.preconfHead.header.Number = big.NewInt(int64(lastBlockNumber))
		} else {
			g.preconfHead.header.Number = big.NewInt(int64(chainNo))
		}
	} else {
		if chainNo > headerNo {
			g.preconfHead.header.Number = big.NewInt(int64(chainNo))
		}
	}

	prevDigest := g.preconfHead.header.MixDigest
	g.preconfHead.header.MixDigest = g.mixHash
	g.preconfHead.header.Extra = make([]byte, 32)
	log.Info("Header extra data is", "extra-data", len(g.preconfHead.header.Extra), "content", g.preconfHead.header.Extra)
	block, err := g.engine.FinalizeAndAssemble(g.chain, g.preconfHead.header, g.preconfHead.state, g.preconfHead.txs, nil, g.preconfHead.receipts, make([]*types.Withdrawal, 0))
	if err != nil {

		req.Response <- SealBlockResponse{
			block:                    nil,
			cumulativeBuilderPayment: "",
			err:                      err,
		}
		return
	}

	results := make(chan *types.Block, 1)
	if err := g.engine.Seal(g.chain, block, results, nil); err != nil {
		req.Response <- SealBlockResponse{
			block:                    nil,
			cumulativeBuilderPayment: "",
			err:                      err,
		}
		return
	}
	block = <-results
	g.preconfHead.header.MixDigest = prevDigest

	entry := inMemoryStore{
		block: block,
		env:   g.preconfHead.copy(),
	}

	g.builtBlocks = append(g.builtBlocks, entry)
	g.mapBlockNumber[int64(block.NumberU64())] = entry
	g.mapBlockHash[block.Hash().Hex()] = entry
	cumulativeBuilderPayment := g.preconfHead.cumulativeBuilderPayment

	g.preconfHead.parentHash = block.Hash()
	g.preconfHead.reset()

	req.Response <- SealBlockResponse{
		block:                    block,
		cumulativeBuilderPayment: fmt.Sprintf("0x%x", cumulativeBuilderPayment),
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

	signer := types.MakeSigner(g.chainConfig, env.header.Number, env.header.Time)
	from, err := types.Sender(signer, tx)
	if err != nil {
		log.Error("error retrieving sender address from transaction", "err", err.Error())
		return nil, nil, 0, err
	}
	if len(env.txs) == 0 {
		if from.Hex() != "0x0000777735367b36bC9B61C50022d9D0700dB4Ec" {
			log.Error("first transaction must come from GoldenTouchAccount")
			//return nil, nil, 0, errors.New("first transaction must come from GoldenTouchAccount")
		} else {
			err = tx.MarkAsAnchor()
			if err != nil {
				log.Error("error marking transaction as anchor", "err", err.Error())
			}
		}
	}

	receipt, err := g.commitPreconfTransaction(env, tx)
	if err != nil {
		return nil, nil, 0, err
	}

	log.Info("PRECONF: simulated tx.", "was success", receipt != nil)

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

func genMixHash(blockNumber uint64) common.Hash {
	taikoDifficulty := []byte("TAIKO_DIFFICULTY")

	// ABI encoding equivalent: combine "TAIKO_DIFFICULTY" with numBlocks
	encoded := append(taikoDifficulty, uint64ToBytes(blockNumber)...)

	// Perform keccak256 hashing (Keccak-256 is sha3.NewLegacyKeccak256)
	hash := sha3.NewLegacyKeccak256()
	hash.Write(encoded)
	result := hash.Sum(nil)
	return common.HexToHash(fmt.Sprintf("0x%x", result))
}

func uint64ToBytes(num uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, num)
	return buf
}
