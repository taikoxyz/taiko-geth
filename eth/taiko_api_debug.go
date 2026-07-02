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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
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

// executionWitness is the alethia-reth-compatible debug execution-witness wire
// format. All fields are byte arrays in JSON; headers are RLP-encoded.
type executionWitness struct {
	State   []hexutil.Bytes `json:"state"`
	Codes   []hexutil.Bytes `json:"codes"`
	Keys    []hexutil.Bytes `json:"keys"`
	Headers []hexutil.Bytes `json:"headers"`
}

// newExecutionWitness converts the internal witness to the cross-client debug
// RPC wire format, RLP-encoding each ancestor header.
func newExecutionWitness(w *stateless.Witness) (*executionWitness, error) {
	out := &executionWitness{
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

// CHANGE(taiko): recoverableNonAnchorTxErrors is the transaction-error set the
// tx-list replay tolerates by skipping a non-anchor transaction — the same
// recoverable class canonical block building skips: zk-gas truncation, a gas
// limit exceeding the block's remaining gas, and the message pre-check
// (invalid-transaction) failures from core/error.go. Any error outside this set
// is fatal, so the RPC never emits a witness for a tx list canonical execution
// would reject.
var recoverableNonAnchorTxErrors = []error{
	vm.ErrZkGasLimitExceeded,
	core.ErrGasLimitReached,
	core.ErrGasLimitOverflow,
	core.ErrNonceTooLow,
	core.ErrNonceTooHigh,
	core.ErrNonceMax,
	core.ErrInsufficientFunds,
	core.ErrInsufficientFundsForTransfer,
	core.ErrInsufficientBalanceWitness,
	core.ErrGasUintOverflow,
	core.ErrIntrinsicGas,
	core.ErrFloorDataGas,
	core.ErrTxTypeNotSupported,
	core.ErrTipAboveFeeCap,
	core.ErrTipVeryHigh,
	core.ErrFeeCapVeryHigh,
	core.ErrFeeCapTooLow,
	core.ErrSenderNoEOA,
	core.ErrBlobFeeCapTooLow,
	core.ErrMissingBlobHashes,
	core.ErrTooManyBlobs,
	core.ErrBlobTxCreate,
	core.ErrEmptyAuthList,
	core.ErrSetCodeTxCreate,
	core.ErrGasLimitTooHigh,
}

func isRecoverableNonAnchorTxError(err error) bool {
	for _, target := range recoverableNonAnchorTxErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
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

	// CHANGE(taiko): record the block numbers whose hashes the transactions
	// resolve via the BLOCKHASH opcode. go-geth serves these from the header
	// chain and witnesses them as headers, but an EIP-2935 stateless executor
	// resolves them from the HistoryStorage contract's storage. We collect the
	// numbers here and pull the backing storage slots into the witness below.
	var blockHashNums []uint64
	seenBlockHash := make(map[uint64]struct{})
	evm := vm.NewEVM(core.NewEVMBlockContext(header, bc, &header.Coinbase), statedb, config, vm.Config{
		Tracer: &tracing.Hooks{
			OnBlockHashRead: func(number uint64, _ common.Hash) {
				if _, ok := seenBlockHash[number]; ok {
					return
				}
				seenBlockHash[number] = struct{}{}
				blockHashNums = append(blockHashNums, number)
			},
		},
	})
	var zkGasMeter *vm.ZkGasMeter
	if config.IsUnzen(header.Time) {
		zkGasMeter = vm.NewZkGasMeter(&vm.UnzenZkGasSchedule)
		evm.SetZkGasMeter(zkGasMeter)
	}

	// CHANGE(taiko): apply the same pre-execution system calls as canonical block
	// processing (EIP-4788 beacon-block-root, EIP-2935 parent-block-hash) before
	// replaying transactions, so the witness captures their state accesses and the
	// replay reproduces the canonical post-state.
	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		core.ProcessBeaconBlockRoot(*beaconRoot, evm)
	}
	if config.IsPrague(block.Number(), block.Time()) || config.IsVerkle(block.Number(), block.Time()) {
		core.ProcessParentBlockHash(block.ParentHash(), evm)
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
			if isAnchor {
				return nil, nil, fmt.Errorf("anchor transaction failed: %w", err)
			}
			// CHANGE(taiko): tolerate only the recoverable error set the block
			// builder skips for non-anchor txs; any other error is fatal.
			if !isRecoverableNonAnchorTxError(err) {
				return nil, nil, fmt.Errorf("non-anchor transaction at index %d failed: %w", i, err)
			}
			// A non-anchor zk-gas-limit error truncates the block.
			if zkGasMeter != nil && errors.Is(err, vm.ErrZkGasLimitExceeded) {
				zkGasMeter.ResetTransaction()
				evm.ResetZkGasErr()
				break
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

	// CHANGE(taiko): mirror canonical Prague post-execution system calls (EIP-7002
	// withdrawal queue, EIP-7251 consolidation queue) so the witness captures their
	// state accesses before finalization. EIP-6110 deposit-log parsing reads logs
	// only and touches no state, so it is not needed for the witness.
	if config.IsPrague(block.Number(), block.Time()) {
		var requests [][]byte
		if err := core.ProcessWithdrawalQueue(&requests, evm); err != nil {
			return nil, nil, fmt.Errorf("post-execution withdrawal queue system call failed: %w", err)
		}
		if err := core.ProcessConsolidationQueue(&requests, evm); err != nil {
			return nil, nil, fmt.Errorf("post-execution consolidation queue system call failed: %w", err)
		}
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

	// CHANGE(taiko): witness the system-call caller account (params.SystemAddress,
	// 0xff..fe). EVM.Call skips the value transfer for system calls (see
	// core/vm/evm.go), so the caller account is never loaded during the EIP-4788 /
	// EIP-2935 / EIP-7002 / EIP-7251 system calls and its account-trie path never
	// enters the witness. This node's own stateless re-execution skips the caller
	// too, so the gap is invisible to a state-root self-consistency check — but a
	// cross-client stateless executor that loads the system caller during those
	// system calls cannot resolve the account without its account-trie proof (and
	// key preimage). A zero-value balance add loads the account (recording the key)
	// and, via empty-account clearing in IntermediateRoot, walks its account-trie
	// path (recording the proof nodes) without changing the post-state root. Only
	// done when a system call actually ran, so the witness matches cross-client
	// contents exactly.
	if block.BeaconRoot() != nil ||
		config.IsPrague(block.Number(), block.Time()) ||
		config.IsVerkle(block.Number(), block.Time()) {
		statedb.AddBalance(params.SystemAddress, uint256.NewInt(0), tracing.BalanceChangeUnspecified)
	}

	// CHANGE(taiko): witness the EIP-2935 HistoryStorage slots backing the block
	// hashes the transactions resolved via BLOCKHASH. go-geth reads those hashes
	// from the header chain, so their storage-trie proofs never enter the witness
	// otherwise; a cross-client stateless executor that resolves BLOCKHASH from
	// the HistoryStorage contract cannot resolve the account without them. The
	// slot value equals the block hash go-geth already returned, so this pure
	// read records the key preimage and loads the storage-trie path without
	// changing the post-state. EIP-2935 (Prague) stores hash(n) at slot
	// n % HistoryServeWindow; the served window is a subset of BLOCKHASH's.
	if config.IsPrague(block.Number(), block.Time()) || config.IsVerkle(block.Number(), block.Time()) {
		for _, num := range blockHashNums {
			slot := common.BigToHash(new(big.Int).SetUint64(num % params.HistoryServeWindow))
			statedb.GetState(params.HistoryStorageAddress, slot)
		}
	}
	statedb.IntermediateRoot(true)

	return statedb.Witness(), committed, nil
}

// CHANGE(taiko): ExecutionWitnessForTxList replays the given RLP transaction
// list on top of the parent state of the requested block and returns the
// execution witness.
//
// Params: (blockNrOrHash, txListRLP, mode?, options?). Only the legacy witness
// mode is supported.
func (api *DebugAPI) ExecutionWitnessForTxList(bn rpc.BlockNumberOrHash, txList hexutil.Bytes, mode *string, opts *txListWitnessOptions) (*executionWitness, error) {
	return executionWitnessForTxList(api.eth.blockchain, bn, txList, mode, opts)
}

// CHANGE(taiko): executionWitnessForTxList validates the request, resolves the
// target block, decodes the transaction list, and returns the cross-client
// execution witness produced by replaying it on the parent state.
func executionWitnessForTxList(bc *core.BlockChain, bn rpc.BlockNumberOrHash, txList hexutil.Bytes, mode *string, opts *txListWitnessOptions) (*executionWitness, error) {
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
	return newExecutionWitness(witness)
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
