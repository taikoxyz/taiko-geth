// CHANGE(taiko): debug_executionWitnessForTxList replays an explicit
// transaction list on a block's parent state and returns a cross-client
// execution witness (headers RLP-encoded, key preimages populated).
package eth

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

// Block-level transaction-list size limits. The raw guard bounds the work of
// the compression check; the authoritative limit is the compressed size that
// must fit one blob of data availability.
const (
	maxTxListRawBytes        = 16 * 1024 * 1024
	maxTxListCompressedBytes = params.BlobTxFieldElementsPerBlob * params.BlobTxBytesPerFieldElement
)

// txListWitnessOptions holds the optional flags for debug_executionWitnessForTxList.
type txListWitnessOptions struct {
	// SkipZkGasDifficultyCheck skips matching the recomputed block zk gas against
	// the block header difficulty. Only useful for debug experiments that replay
	// non-canonical transaction lists on top of a canonical parent state.
	SkipZkGasDifficultyCheck bool `json:"skipZkGasDifficultyCheck"`
}

// txListExecutionWitness is the cross-client execution-witness wire format.
type txListExecutionWitness struct {
	State   []hexutil.Bytes `json:"state"`
	Codes   []hexutil.Bytes `json:"codes"`
	Keys    []hexutil.Bytes `json:"keys"`
	Headers []hexutil.Bytes `json:"headers"`
}

// newTxListExecutionWitness converts the internal witness to the cross-client
// wire format, RLP-encoding each ancestor header.
func newTxListExecutionWitness(w *stateless.Witness) (*txListExecutionWitness, error) {
	out := &txListExecutionWitness{
		State:   make([]hexutil.Bytes, 0, len(w.State)),
		Codes:   make([]hexutil.Bytes, 0, len(w.Codes)),
		Keys:    make([]hexutil.Bytes, 0, len(w.Keys)),
		Headers: make([]hexutil.Bytes, 0, len(w.Headers)),
	}
	for node := range w.State {
		out.State = append(out.State, []byte(node))
	}
	for code := range w.Codes {
		out.Codes = append(out.Codes, []byte(code))
	}
	for key := range w.Keys {
		out.Keys = append(out.Keys, []byte(key))
	}
	for _, header := range w.Headers {
		enc, err := rlp.EncodeToBytes(header)
		if err != nil {
			return nil, fmt.Errorf("failed to rlp-encode witness header %v: %w", header.Number, err)
		}
		out.Headers = append(out.Headers, enc)
	}
	return out, nil
}

// decodeTxListWitnessTxs decodes an RLP list of transactions. It errors only on
// a malformed top-level list; unrecoverable-signer transactions are filtered
// later during replay, matching block-building behavior.
func decodeTxListWitnessTxs(txListRLP []byte) (types.Transactions, error) {
	var txs types.Transactions
	if err := rlp.DecodeBytes(txListRLP, &txs); err != nil {
		return nil, fmt.Errorf("failed to decode tx list: %w", err)
	}
	return txs, nil
}

// checkTxListSize rejects a transaction list too large to be a valid block list.
func checkTxListSize(raw []byte) error {
	if len(raw) > maxTxListRawBytes {
		return fmt.Errorf("tx list size %d exceeds raw limit %d", len(raw), maxTxListRawBytes)
	}
	compressed, err := zlibCompressedLen(raw)
	if err != nil {
		return err
	}
	if compressed > maxTxListCompressedBytes {
		return fmt.Errorf("tx list compressed size %d exceeds block data limit %d", compressed, maxTxListCompressedBytes)
	}
	return nil
}

func zlibCompressedLen(raw []byte) (int, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return 0, err
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	return buf.Len(), nil
}

// zkGasDifficultyMismatch reports whether the recomputed block zk gas differs
// from the block header difficulty.
func zkGasDifficultyMismatch(headerDifficulty *big.Int, recomputed uint64) bool {
	return headerDifficulty == nil || headerDifficulty.Cmp(new(big.Int).SetUint64(recomputed)) != 0
}

// CHANGE(taiko): buildTxListWitness replays txs on top of the parent state of
// `block` and returns the collected execution witness plus the committed
// (post-filter) transactions. It mirrors the block-builder's filtering: the
// anchor (index 0) must succeed; non-anchor failures are skipped; a non-anchor
// zk-gas-limit error truncates the block.
func buildTxListWitness(bc *core.BlockChain, block *types.Block, txs types.Transactions, opts txListWitnessOptions) (*stateless.Witness, types.Transactions, error) {
	config := bc.Config()
	header := block.Header() // copy; safe to mutate during finalize
	parent := bc.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return nil, nil, fmt.Errorf("parent of block %d not found", header.Number)
	}
	statedb, err := bc.StateAt(parent.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open state at parent root %s: %w", parent.Root, err)
	}
	witness, err := stateless.NewWitness(header, bc, false)
	if err != nil {
		return nil, nil, err
	}
	statedb.StartPrefetcher("txlist-witness", witness)
	defer statedb.StopPrefetcher()

	evm := vm.NewEVM(core.NewEVMBlockContext(header, bc, &header.Coinbase), statedb, config, vm.Config{})
	var zkGasMeter *vm.ZkGasMeter
	if config.IsUnzen(header.Time) {
		zkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
		evm.SetZkGasMeter(zkGasMeter)
	}

	gasPool := core.NewGasPool(header.GasLimit)
	rules := config.Rules(header.Number, true, header.Time)
	signer := types.LatestSignerForChainID(config.ChainID)

	committed := make(types.Transactions, 0, len(txs))
	for i, tx := range txs {
		isAnchor := i == 0 && config.Taiko
		if isAnchor {
			if err := tx.MarkAsAnchor(); err != nil {
				return nil, nil, fmt.Errorf("anchor transaction invalid: %w", err)
			}
		}
		if tx.Type() == types.BlobTxType {
			if isAnchor {
				return nil, nil, errors.New("anchor transaction must not be a blob transaction")
			}
			continue
		}
		sender, err := signer.Sender(tx)
		if err != nil {
			if isAnchor {
				return nil, nil, fmt.Errorf("anchor transaction sender unrecoverable: %w", err)
			}
			continue
		}

		statedb.Prepare(rules, sender, header.Coinbase, tx.To(), vm.ActivePrecompiles(rules), tx.AccessList())
		statedb.SetTxContext(tx.Hash(), len(committed))
		if zkGasMeter != nil {
			zkGasMeter.ResetTransaction()
			evm.ResetZkGasErr()
		}

		snap := statedb.Snapshot()
		gpSnap := gasPool.Snapshot()
		if _, err := core.ApplyTransaction(evm, gasPool, statedb, header, tx); err != nil {
			statedb.RevertToSnapshot(snap)
			gasPool.Set(gpSnap)
			// A non-anchor zk-gas-limit error truncates the block.
			if zkGasMeter != nil && errors.Is(err, vm.ErrZkGasLimitExceeded) && i > 0 {
				zkGasMeter.ResetTransaction()
				evm.ResetZkGasErr()
				break
			}
			if isAnchor {
				return nil, nil, fmt.Errorf("anchor transaction failed: %w", err)
			}
			continue
		}
		if zkGasMeter != nil {
			if commitErr := zkGasMeter.CommitTransaction(); commitErr != nil && i > 0 {
				zkGasMeter.ResetTransaction()
				break
			}
		}
		committed = append(committed, tx)
	}

	if zkGasMeter != nil && !opts.SkipZkGasDifficultyCheck {
		recomputed := zkGasMeter.BlockZkGasUsed()
		if zkGasDifficultyMismatch(header.Difficulty, recomputed) {
			return nil, nil, fmt.Errorf("zk gas difficulty mismatch: header has %v, recomputed %d", header.Difficulty, recomputed)
		}
	}

	// Apply post-execution changes (withdrawals / header finalization), then
	// flush the trie so the witness state-node set is complete.
	bc.Engine().Finalize(bc, header, statedb, &types.Body{Withdrawals: block.Withdrawals()})
	statedb.IntermediateRoot(true)

	return statedb.Witness(), committed, nil
}

// CHANGE(taiko): ExecutionWitnessForTxList replays the given RLP transaction
// list on top of the parent state of the requested block and returns the
// execution witness.
//
// Params: (blockNrOrHash, txListRLP, mode?, options?). Only the legacy witness
// mode is supported.
func (api *DebugAPI) ExecutionWitnessForTxList(bn rpc.BlockNumberOrHash, txList hexutil.Bytes, mode *string, opts *txListWitnessOptions) (*txListExecutionWitness, error) {
	return executionWitnessForTxList(api.eth.blockchain, bn, txList, mode, opts)
}

// CHANGE(taiko): executionWitnessForTxList validates the request, resolves the
// target block, decodes the transaction list, and returns the cross-client
// execution witness produced by replaying it on the parent state.
func executionWitnessForTxList(bc *core.BlockChain, bn rpc.BlockNumberOrHash, txList hexutil.Bytes, mode *string, opts *txListWitnessOptions) (*txListExecutionWitness, error) {
	if mode != nil && *mode != "" && *mode != "legacy" {
		return nil, fmt.Errorf("unsupported witness mode %q", *mode)
	}
	var options txListWitnessOptions
	if opts != nil {
		options = *opts
	}
	if err := checkTxListSize(txList); err != nil {
		return nil, err
	}
	block, err := resolveWitnessBlock(bc, bn)
	if err != nil {
		return nil, err
	}
	txs, err := decodeTxListWitnessTxs(txList)
	if err != nil {
		return nil, err
	}
	witness, _, err := buildTxListWitness(bc, block, txs, options)
	if err != nil {
		return nil, err
	}
	return newTxListExecutionWitness(witness)
}

// CHANGE(taiko): resolveWitnessBlock resolves a block number or hash to a block
// for witness generation. Negative sentinels (latest/pending/finalized/safe)
// resolve to the current chain head.
func resolveWitnessBlock(bc *core.BlockChain, bn rpc.BlockNumberOrHash) (*types.Block, error) {
	if hash, ok := bn.Hash(); ok {
		block := bc.GetBlockByHash(hash)
		if block == nil {
			return nil, fmt.Errorf("block %s not found", hash)
		}
		return block, nil
	}
	number, _ := bn.Number()
	if number < 0 { // latest / pending / finalized / safe
		current := bc.CurrentBlock()
		if current == nil {
			return nil, errors.New("current block not available")
		}
		block := bc.GetBlockByNumber(current.Number.Uint64())
		if block == nil {
			return nil, errors.New("current block not found")
		}
		return block, nil
	}
	block := bc.GetBlockByNumber(uint64(number))
	if block == nil {
		return nil, fmt.Errorf("block %d not found", number)
	}
	return block, nil
}
