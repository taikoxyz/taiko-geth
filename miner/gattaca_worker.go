package miner

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"sync"
	"time"

	ckzg4844 "github.com/ethereum/c-kzg-4844/bindings/go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"golang.org/x/crypto/sha3"
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
	StateId uint64                  `json:"stateId"`
	Tx      *types.Transaction      `json:"-"`
	SimRes  chan SimulationResponse `json:"-"`
}

type SimulateAnchorTx struct {
	Tx       *types.Transaction            `json:"-"`
	BlockEnv common.BlockEnv               `json:"-"`
	SimRes   chan SimulateAnchorTxResponse `json:"-"`
}

type SimulateAnchorTxResponse struct {
	StateId uint64 `json:"stateId"`
	Err     error  `json:"err"`
	GasUsed uint64 `json:"gasUsed"`
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
	StateId uint64                   `json:"stateId"`
	SimRes  chan CommitStateResponse `json:"-"`
}

type InnerCommitState struct {
	StateId   uint32
	commitRes chan SimulationResponse
}

type SimulationResponse struct {
	stateId        uint64
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

func (s SimulationResponse) StateId() uint64 {
	return s.stateId
}

func (s SimulationResponse) GasUsed() uint64 {
	return s.gasUsed
}

func (s SimulationResponse) BuilderPayment() string {
	return s.builderPayment
}

type GattacaWorker struct {
	chainConfig    *params.ChainConfig
	chain          *core.BlockChain
	config         *Config
	engine         consensus.Engine
	extra          []byte
	lock           sync.RWMutex
	halt           bool
	haltReason     string
	mapBlockNumber map[int64]inMemoryStore
	mapBlockHash   map[string]inMemoryStore

	mixHash common.Hash

	envMap map[uint64]*environment

	preconfHead *environment
	builtBlocks []inMemoryStore

	preconfState *PreconfState
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
			envMap:         make(map[uint64]*environment),
			preconfHead:    nil,
			halt:           false,
			haltReason:     "",
			builtBlocks:    make([]inMemoryStore, 0),
			mapBlockNumber: make(map[int64]inMemoryStore),
			mapBlockHash:   make(map[string]inMemoryStore),
			preconfState:   NewPreconfState(chain),
		}
		env, err := singletonGattaca.retrieveEnv(1)
		if err != nil {
			log.Error("Failed to retrieve environment", "err", err)
			return nil, err
		}
		singletonGattaca.preconfHead = env
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
			log.Info("run seal block ", "stateId", req.StateId)
			go g.sealBlock(req)
		case req := <-SimAnchorTx:
			go g.simulateAnchorTx(req.Tx, req.BlockEnv, req.SimRes)
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
			g.lock.Lock()
			block := ev.Block
			idx := -1
			if len(g.builtBlocks) > 0 && block != nil {
				for i, entry := range g.builtBlocks {
					if block.NumberU64() == entry.block.NumberU64() {
						idx = i
						if block.Hash().Hex() == entry.block.Hash().Hex() {
						} else {
							log.Warn("block hash mismatch, most likely the preconfHead is different from chain head, resetting it.")
							env, _ := g.retrieveEnv(1)
							g.preconfHead = env
						}
						break
					}
				}
				if idx != -1 {
					g.builtBlocks = append(g.builtBlocks[:idx], g.builtBlocks[idx+1:]...)
				}
				// we need to check if the block number received is equal or greater than the current preconfHead.
				if block.NumberU64() >= g.preconfHead.header.Number.Uint64() {
					log.Warn("preconfhead number is equal or lower to the received block, resetting it.")
					g.preconfHead, _ = g.retrieveEnv(1)
				}
			}
			g.lock.Unlock()
		}
	}
}

// TODO: we need to make sure we set all the correct preconfState.pendingPreconfBlock environment fields correctly. A lot of stuff was set in sealBlock and has been removed.
func (g *GattacaWorker) simulateAnchorTx(tx *types.Transaction, env common.BlockEnv, res chan SimulateAnchorTxResponse) {
	blockEnv, err := g.retrieveEnv(1)
	if err != nil {
		panic(err)
	}
	blockNumber := big.NewInt(0)
	// as per alloy spec
	// The number of ancestor blocks of this block (block height).
	// So we need to increment by 1 to get the current block.
	blockEnv.header.Number = blockNumber.Add(env.Number.ToInt(), big.NewInt(1))
	blockEnv.header.Coinbase = env.Coinbase
	blockEnv.header.MixDigest = *env.PrevRandao
	blockEnv.header.GasLimit = env.GasLimit.ToInt().Uint64()
	blockEnv.header.BaseFee = env.BaseFee.ToInt()
	blockEnv.header.Time = env.Timestamp.ToInt().Uint64()
	// simulate tx
	signer := types.MakeSigner(g.chainConfig, blockEnv.header.Number, blockEnv.header.Time)
	from, err := types.Sender(signer, tx)
	if err != nil {
		log.Error("error retrieving sender address from transaction", "err", err.Error())
		res <- SimulateAnchorTxResponse{
			Err:     errors.New("failed to retrieve sender address from transaction"),
			StateId: 0,
		}
		return
	}
	if from.Hex() != "0x0000777735367b36bC9B61C50022d9D0700dB4Ec" {
		log.Error("first transaction must come from GoldenTouchAccount", "from", from.Hex())
		res <- SimulateAnchorTxResponse{
			Err: errors.New("first transaction must come from GoldenTouchAccount"),
		}
		return
	}

	receipt, _, _, err := g.commitTx(blockEnv, tx)
	log.Debug("Anchor tx receipt", "receipt", receipt)

	if err != nil {
		var gasUsed uint64
		var commitError CommitError
		if errors.As(err, &commitError) {
			gasUsed = blockEnv.gasPool.Gas()
		}
		log.Error("Failed to simulate transaction", "err", err)
		res <- SimulateAnchorTxResponse{
			Err:     NewCommitError(err),
			GasUsed: gasUsed,
		}
		return
	} else if receipt.Status == 0 {
		log.Error("transaction reverted", "receipt", receipt)
		err := errors.New("transaction reverted")
		res <- SimulateAnchorTxResponse{
			Err: NewCommitError(err),
		}
		return
	}

	blockEnv.hashReceipts[tx.Hash().Hex()] = receipt
	blockEnv.receipts = append(blockEnv.receipts, receipt)
	// set env
	newStateId := rand.Uint64()
	g.envMap[newStateId] = blockEnv.copy()
	g.preconfState.setPendingPreconfBlock(blockEnv.copy())
	res <- SimulateAnchorTxResponse{
		Err:     nil,
		StateId: newStateId,
	}
}

func (g *GattacaWorker) simulateTx(stateId uint64, tx *types.Transaction, res chan SimulationResponse) {
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
	newStateId := rand.Uint64()
	simEnv.hashReceipts[tx.Hash().Hex()] = receipt
	builderPayment := endBalance - startBalance
	simEnv.cumulativeBuilderPayment += builderPayment
	g.envMap[newStateId] = simEnv
	simEnv.receipts = append(simEnv.receipts, receipt)
	res <- SimulationResponse{
		error:          nil,
		gasUsed:        receipt.GasUsed,
		stateId:        newStateId,
		builderPayment: fmt.Sprintf("0x%x", builderPayment),
	}
}

func (g *GattacaWorker) commitEnvToPreconf(stateId uint64, simRes chan CommitStateResponse) {
	cumGasUsed, builderPayment, err := g.preconfState.commitStateIDToPendingBlock(stateId)
	simRes <- CommitStateResponse{
		error:                    err,
		cumulativeGasUsed:        cumGasUsed,
		cumulativeBuilderPayment: builderPayment,
	}
}

// sealBlock seals the current pending pre-confirmed block.
// It finalizes, assembles, and seals the block, then updates the preconf state.
func (g *GattacaWorker) sealBlock(req SealBlockRequest) {
	g.lock.Lock()
	defer g.lock.Unlock()

	// Retrieve the pending pre-confirmed block from the preconf state.
	pendingPreconfBlock := g.preconfState.pendingPreconfBlock
	if pendingPreconfBlock == nil {
		req.Response <- SealBlockResponse{err: errors.New("no pending preconf block to seal")}
		return
	}

	log.Info("Starting block sealing process",
		"pendingPreconfBlockNumber", pendingPreconfBlock.header.Number.Uint64(),
	)

	// Calculate total gas used by transactions in the pending block.
	var transactionGas uint64
	for _, tx := range pendingPreconfBlock.txs {
		receipt, exists := pendingPreconfBlock.hashReceipts[tx.Hash().Hex()]
		if !exists {
			req.Response <- SealBlockResponse{
				err: fmt.Errorf("missing receipt for transaction %s", tx.Hash().Hex()),
			}
			return
		}
		transactionGas += receipt.GasUsed
	}

	log.Info("Transactions in block",
		"blockNumber", pendingPreconfBlock.header.Number.Uint64(),
		"transactionCount", len(pendingPreconfBlock.txs),
		"totalGasUsed", transactionGas,
	)

	block, err := g.engine.FinalizeAndAssemble(
		g.chain,
		pendingPreconfBlock.header,
		pendingPreconfBlock.state,
		pendingPreconfBlock.txs,
		nil, // Uncles (always nil for Taiko)
		pendingPreconfBlock.receipts,
		nil, // Withdrawals (always nil for Taiko)
	)
	if err != nil {
		// Error finalizing and assembling block; send error response.
		req.Response <- SealBlockResponse{err: err}
		return
	}

	results := make(chan *types.Block, 1)
	if err := g.engine.Seal(g.chain, block, results, nil); err != nil {
		req.Response <- SealBlockResponse{err: err}
		return
	}
	sealedBlock := <-results
	log.Info("Block sealed", "sealedBlockHash", sealedBlock.Hash().Hex())

	//before sealing it, set the block to the env
	pendingPreconfBlock.sealedBlock = sealedBlock
	err = g.preconfState.sealPendingPreconfBlock()
	if err != nil {
		req.Response <- SealBlockResponse{err: err}
	}

	// Send the successful seal block response.
	log.Info("Sending seal block response",
		"sealedBlockNumber", sealedBlock.Number().Uint64(),
		"sealedBlockHash", sealedBlock.Hash().Hex(),
	)
	cumulativeBuilderPaymentHex := fmt.Sprintf("0x%x", pendingPreconfBlock.cumulativeBuilderPayment)
	req.Response <- SealBlockResponse{
		block:                    sealedBlock,
		cumulativeBuilderPayment: cumulativeBuilderPaymentHex,
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

func (g *GattacaWorker) PreconfState() *PreconfState {
	return g.preconfState
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
