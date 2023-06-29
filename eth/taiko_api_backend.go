package eth

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

// TaikoAPIBackend handles l2 node related RPC calls.
type TaikoAPIBackend struct {
	eth *Ethereum
}

// NewTaikoAPIBackend creates a new TaikoAPIBackend instance.
func NewTaikoAPIBackend(eth *Ethereum) *TaikoAPIBackend {
	return &TaikoAPIBackend{
		eth: eth,
	}
}

// HeadL1Origin returns the latest L2 block's corresponding L1 origin.
func (s *TaikoAPIBackend) HeadL1Origin() (*rawdb.L1Origin, error) {
	blockID, err := rawdb.ReadHeadL1Origin(s.eth.ChainDb())
	if err != nil {
		return nil, err
	}

	if blockID == nil {
		return nil, ethereum.NotFound
	}

	l1Origin, err := rawdb.ReadL1Origin(s.eth.ChainDb(), blockID)
	if err != nil {
		return nil, err
	}

	if l1Origin == nil {
		return nil, ethereum.NotFound
	}

	return l1Origin, nil
}

// L1OriginByID returns the L2 block's corresponding L1 origin.
func (s *TaikoAPIBackend) L1OriginByID(blockID *math.HexOrDecimal256) (*rawdb.L1Origin, error) {
	l1Origin, err := rawdb.ReadL1Origin(s.eth.ChainDb(), (*big.Int)(blockID))
	if err != nil {
		return nil, err
	}

	if l1Origin == nil {
		return nil, ethereum.NotFound
	}

	return l1Origin, nil
}

// TxPoolContent retrieves the transaction pool content with the given upper limits.
func (s *TaikoAPIBackend) TxPoolContent(
	maxTransactionsPerBlock uint64,
	blockMaxGasLimit uint64,
	maxBytesPerTxList uint64,
	minTxGasLimit uint64,
	locals []string,
) ([]types.Transactions, error) {
	pending := s.eth.TxPool().Pending(false)

	log.Debug(
		"Fetching L2 pending transactions finished",
		"length", core.PoolContent(pending).Len(),
		"maxTransactionsPerBlock", maxTransactionsPerBlock,
		"blockMaxGasLimit", blockMaxGasLimit,
		"maxBytesPerTxList", maxBytesPerTxList,
		"minTxGasLimit", minTxGasLimit,
		"locals", locals,
	)

	filteredPendings, err := filterInexecutableTxs(context.Background(), s.eth.APIBackend, pending)
	if err != nil {
		return nil, err
	}

	contentSplitter, err := core.NewPoolContentSplitter(
		s.eth.BlockChain().Config().ChainID,
		maxTransactionsPerBlock,
		blockMaxGasLimit,
		maxBytesPerTxList,
		minTxGasLimit,
		locals,
	)
	if err != nil {
		return nil, err
	}

	var (
		txsCount = 0
		txLists  []types.Transactions
	)
	for _, splittedTxs := range contentSplitter.Split(filteredPendings) {
		if txsCount+splittedTxs.Len() < int(maxTransactionsPerBlock) {
			txLists = append(txLists, splittedTxs)
			txsCount += splittedTxs.Len()
			continue
		}

		txLists = append(txLists, splittedTxs[0:(int(maxTransactionsPerBlock)-txsCount)])
		break
	}

	return txLists, nil
}

func filterInexecutableTxs(
	ctx context.Context,
	backend ethapi.Backend,
	pendings map[common.Address]types.Transactions,
) (map[common.Address]types.Transactions, error) {
	executableTxs := make(map[common.Address]types.Transactions)
	currentHead := rpc.BlockNumber(backend.CurrentHeader().Number.Int64())
	// Resolve block number and use its state to ask for the nonce
	state, _, err := backend.StateAndHeaderByNumberOrHash(ctx, rpc.BlockNumberOrHash{
		BlockNumber: &currentHead,
	})
	if state == nil || err != nil {
		return nil, err
	}

	for addr, txs := range pendings {
		pendingTxs := make(types.Transactions, 0)
		for i, tx := range txs {
			// Check that account's nonce at first
			if i == 0 {
				nonce := state.GetNonce(addr)
				if tx.Nonce() != nonce {
					log.Debug(
						"Skip a transaction with an invalid nonce",
						"address", addr,
						"nonceInTx", tx.Nonce(),
						"nonceInState", nonce,
					)
					break
				}
			}
			// Check baseFee, should not be zero
			if tx.GasFeeCap().Uint64() == 0 {
				break
			}

			pendingTxs = append(pendingTxs, tx)
		}

		if len(pendingTxs) > 0 {
			executableTxs[addr] = pendingTxs
		}
	}

	return executableTxs, nil
}
