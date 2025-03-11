package miner

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

var (
	SimCh       = make(chan SimulateTxRequest, 1000)
	SimAnchorTx = make(chan SimulateAnchorTx, 1000)
	SealBlock   = make(chan SealBlockRequest, 1)
	taikoMinTip = big.NewInt(0)

	GoldenTouchAddress = common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec")
)

var (
	singletonGattaca *GattacaWorker = nil
	singletonLock                   = &sync.Mutex{}
)

type GattacaWorker struct {
	chainConfig *params.ChainConfig
	chain       *core.BlockChain
	config      *Config
	engine      consensus.Engine
	lock        sync.RWMutex
	stateIdLock sync.Mutex // Lock for stateId operations
	halt        bool
	haltReason  string

	preconfState *PreconfState
}

func NewGattacaWorker(
	chainConfig *params.ChainConfig,
	chain *core.BlockChain,
	config *Config,
	engine consensus.Engine,
	preconfState *PreconfState,
) (*GattacaWorker, error) {

	singletonLock.Lock()
	defer singletonLock.Unlock()
	if singletonGattaca == nil {

		singletonGattaca = &GattacaWorker{
			chainConfig:  chainConfig,
			chain:        chain,
			config:       config,
			engine:       engine,
			halt:         false,
			haltReason:   "",
			preconfState: preconfState,
		}

		go singletonGattaca.runLoop()
		go singletonGattaca.newHeadEventSubscriber()
	}
	return singletonGattaca, nil
}

func (g *GattacaWorker) runLoop() {
	for {
		select {
		case req := <-SimCh:
			go g.simulateTx(req.StateId, req.Tx, req.SimRes)
			break
		case req := <-SealBlock:
			go g.sealBlock(req)
		case req := <-SimAnchorTx:
			go g.simulateAnchorTx(req.Tx, req.BlockEnv, req.SimRes, req.ExtraData)
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
			g.preconfState.onNewChainHeadEvent(&ev)
		}
	}
}

// simulateAnchorTx simulates the execution of an anchor transaction in a new environment
// based on the latest sealed state. It commits the transaction to the state, checks for errors,
// and returns the simulation result via the provided channel.
func (g *GattacaWorker) simulateAnchorTx(tx *types.Transaction, newEnvParams common.BlockEnv, res chan SimulationResponse, extraData string) {
	// Log the input parameters for the simulation.
	log.Info(
		"GTC-WORKER: simulateAnchorTx",
		"newEnvParams", newEnvParams,
		"txHash", tx.Hash(),
	)

	// Fetch the latest sealed env.
	env, err := g.retrieveEnv(uint64(LatestSealedId))
	if err != nil {
		res <- SimulationResponse{
			error: fmt.Errorf("failed to retrieve environment. err: %s", err.Error()),
		}
		return
	}
	bbExtraData, err := hexutil.Decode(extraData)
	if err != nil {
		log.Error("Failed to decode extra data", "extraData", extraData, "err", err)
		res <- SimulationResponse{
			error: fmt.Errorf("failed to decode extra data extraData %v, err %s", extraData, err.Error()),
		}
		return
	}
	env.header.Extra = bbExtraData

	// Copy the environment from the latest sealed state and set the params for the new block.
	simEnv := env.copyAtNewEnvironment(newEnvParams)

	// Set the new tx signer in the env.
	simEnv.signer = types.MakeSigner(g.chainConfig, simEnv.header.Number, simEnv.header.Time)
	from, err := types.Sender(simEnv.signer, tx)
	if err != nil {
		log.Error("error retrieving sender address from anchor transaction", "err", err.Error())
		res <- SimulationResponse{
			error: errors.New("failed to retrieve sender address from transaction"),
		}
		return
	}

	// Anchor txs must be signed by GoldenTouchAddress
	if from != GoldenTouchAddress {
		log.Error("first transaction must come from GoldenTouchAccount")
		res <- SimulationResponse{
			error: errors.New("first transaction must come from GoldenTouchAccount"),
		}
		return
	}

	// Ensure anchor tx nonce matches parent block number
	if err := g.validateAnchorNonce(tx, simEnv); err != nil {
		res <- SimulationResponse{
			error: fmt.Errorf("invalid anchor nonce: %w", err),
		}
		return
	}

	// Commit the anchor to the state
	receipt, _, _, err := g.commitTx(simEnv, tx)

	// Verify the tx didn't fail. e.g., nonce issues.
	if err != nil {
		log.Error("GTC-WORKER: Transaction commit failed", "error", err)
		res <- SimulationResponse{
			error: NewCommitError(err),
		}
		return
	}

	// Verify the tx didn't revert.
	if receipt.Status == types.ReceiptStatusFailed {
		log.Error("GTC-WORKER: anchor transaction reverted", "receipt", receipt)
		err := errors.New("transaction reverted")
		res <- SimulationResponse{
			error:   NewCommitError(err),
			gasUsed: receipt.GasUsed,
		}
		return
	}
	log.Info("GTC-WORKER: anchor transaction executed successfully", "gasUsed", receipt.GasUsed)

	// Finalise the simulation environment and add it to the stateIdMap.
	simEnv.hashReceipts[tx.Hash().Hex()] = receipt
	simEnv.receipts = append(simEnv.receipts, receipt)

	newStateId := g.preconfState.getNextStateId()
	g.preconfState.stateIdMutex.Lock()
	g.preconfState.stateIdMap[newStateId] = simEnv
	g.preconfState.stateIdMutex.Unlock()

	res <- SimulationResponse{
		gasUsed:        receipt.GasUsed,
		stateId:        newStateId,
		builderPayment: &hexutil.U256{0, 0, 0, 0},
	}
}

// simulateTx fetches the environment at stateId. It then clones this environment and simulates/commits the tx request
// to this cloned environment. A new stateId is then generated and the cloned environment is saved.
func (g *GattacaWorker) simulateTx(stateId uint64, tx *types.Transaction, res chan SimulationResponse) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	log.Info("GTC-WORKER: simulateTx", "stateId", stateId, "tx", tx.Hash().Hex())

	// Check for halt message
	if g.halt {
		res <- SimulationResponse{
			error: NewHaltError(errors.New(g.haltReason)),
		}
		return
	}

	// Fetch state ID
	env, err := g.retrieveEnv(stateId)
	if err != nil {
		res <- SimulationResponse{error: NewRetrieveEnError(err)}
		return
	}

	// Anchor tx must always be applied first
	if len(env.txs) == 0 {
		res <- SimulationResponse{
			error: fmt.Errorf("first transaction needs to executed by simulateAnchorAtState. StateId %d", stateId),
		}
		return
	}

	// Copy environment and simulate tx.
	simEnv := env.copy()

	startBalance := simEnv.state.GetBalance(env.coinbase)
	receipt, _, _, err := g.commitTx(simEnv, tx)
	if err != nil {
		log.Error("GTC-WORKER: simulateTx, failed to simulate transaction", "err", err)
		res <- SimulationResponse{
			error: NewCommitError(err),
		}
		return
	}
	endBalance := simEnv.state.GetBalance(env.coinbase)

	var builderPayment *uint256.Int
	if endBalance.Cmp(startBalance) <= 0 {
		builderPayment = uint256.NewInt(0)
	} else {
		builderPayment = new(uint256.Int).Sub(endBalance, startBalance)
	}

	// Tx simulation worked so save result to new env.
	simEnv.hashReceipts[tx.Hash().Hex()] = receipt
	simEnv.cumulativeBuilderPayment = new(uint256.Int).Add(simEnv.cumulativeBuilderPayment, builderPayment)
	simEnv.receipts = append(simEnv.receipts, receipt)

	// Add env to state id map
	newStateId := g.preconfState.getNextStateId()
	g.preconfState.stateIdMutex.Lock()
	g.preconfState.stateIdMap[newStateId] = simEnv
	g.preconfState.stateIdMutex.Unlock()

	log.Info("GTC-WORKER: successfully simulated tx", "tx", tx.Hash().Hex(), "receipt", receipt)

	res <- SimulationResponse{
		error:          nil,
		gasUsed:        receipt.GasUsed,
		stateId:        newStateId,
		builderPayment: (*hexutil.U256)(builderPayment),
	}
}

// sealBlock seals the block at the given stateId
func (g *GattacaWorker) sealBlock(req SealBlockRequest) {
	g.lock.Lock()
	defer g.lock.Unlock()

	log.Info("GTC-WORKER: sealBlock", "stateId", req.StateId)

	// First commit the state
	cumGasUsed, builderPayment, err := g.preconfState.calculateStateMetrics(req.StateId)
	if err != nil {
		req.Response <- SealBlockResponse{err: err}
		return
	}

	// Get the environment to seal
	env, exists := g.preconfState.stateIdMap[req.StateId]
	if !exists {
		req.Response <- SealBlockResponse{err: errors.New("state id not found")}
		return
	}

	log.Info("Starting block sealing process",
		"blockNumber", env.header.Number.Uint64(),
	)

	block, err := g.engine.FinalizeAndAssemble(
		g.chain,
		env.header,
		env.state,
		&types.Body{Transactions: env.txs, Withdrawals: make([]*types.Withdrawal, 0)},
		env.receipts,
	)
	if err != nil {
		// Error finalizing and assembling block; send error response.
		log.Error("GTC-WORKER: sealBlock, failed to finalize and assemble block", "err", err)
		req.Response <- SealBlockResponse{err: err}
		return
	}

	results := make(chan *types.Block, 1)
	if err := g.engine.Seal(g.chain, block, results, nil); err != nil {
		req.Response <- SealBlockResponse{err: err}
		return
	}
	sealedBlock := <-results

	//before sealing it, set the block to the env
	env.sealedBlock = sealedBlock
	err = g.preconfState.sealPreconfBlock(req.StateId, sealedBlock.Hash())
	if err != nil {
		log.Error("GTC-WORKER: sealBlock, failed to seal block", "err", err)
		req.Response <- SealBlockResponse{err: err}
		return
	}

	// Set preconf tag in block
	sealedBlock.PreconfBlock = true

	log.Info("GTC-WORKER: inserting block into chain")

	// Note: might change the actual chain. Will this have side effects?
	_, err = g.chain.InsertChain(types.Blocks{sealedBlock})
	if err != nil {
		log.Error("GTC-WORKER: sealBlock, failed to insert chain", "err", err)
		req.Response <- SealBlockResponse{err: err}
		return
	}

	// Send the successful seal block response.
	cumulativeBuilderPaymentHex := fmt.Sprintf("0x%x", builderPayment)
	req.Response <- SealBlockResponse{
		block:                    sealedBlock,
		cumulativeBuilderPayment: cumulativeBuilderPaymentHex,
		cumulativeGasUsed:        cumGasUsed,
		err:                      nil,
	}
}

func (g *GattacaWorker) commitTx(env *environment, tx *types.Transaction) (*types.Receipt, *uint256.Int, uint64, error) {

	log.Info("GTC-WORKER: commitTx", "tx", tx.Hash().Hex())

	if env.gasPool.Gas() < params.TxGas {
		log.Info("GTC-WORKER: Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
		return nil, nil, 0, errors.New("not enough gas for further transactions")
	}

	// Optional min tip.
	if taikoMinTip != nil {
		if tx.GasTipCapIntCmp(taikoMinTip) < 0 {
			log.Info("GTC-WORKER: Ignoring transaction with low tip", "hash", tx.Hash(), "tip", tx.GasTipCap(), "minTip", taikoMinTip)
			return nil, nil, 0, errors.New("ignoring transaction with low tip")
		}
	}

	// Check whether the tx is replay protected. If we're not in the EIP155 hf
	// phase, start ignoring the sender until we do.
	if tx.Protected() && !g.chainConfig.IsEIP155(env.header.Number) {
		log.Info("GTC-WORKER: Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", g.chainConfig.EIP155Block)
		return nil, nil, 0, errors.New("ignoring reply protected transaction")
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
		if from != GoldenTouchAddress {
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

	sender, err := signer.Sender(tx)
	if err != nil {
		log.Error("GTC-WORKER: failed to recover sender", "err", err)
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

	log.Info("GTC-WORKER: applyTransaction", "tx", tx.Hash().Hex())

	receipt, err := core.ApplyTransaction(g.chainConfig, g.chain, &env.coinbase, env.gasPool, env.state, env.header, tx, &env.header.GasUsed, *g.chain.GetVMConfig())
	if err != nil {
		env.state.RevertToSnapshot(snap)
		env.gasPool.SetGas(gp)
		err = NewRevertCommitError(err)
	}
	return receipt, err
}

func (g *GattacaWorker) PreconfState() *PreconfState {
	return g.preconfState
}

func (g *GattacaWorker) validateAnchorNonce(tx *types.Transaction, env *environment) error {
	// Ensure anchor tx nonce matches parent block number
	parentNumber := env.header.Number.Uint64() - 1
	if tx.Nonce() != parentNumber {
		return fmt.Errorf("anchor nonce %d does not match parent block number %d",
			tx.Nonce(), parentNumber)
	}
	return nil
}
