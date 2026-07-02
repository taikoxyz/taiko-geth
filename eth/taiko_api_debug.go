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
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/triedb/database"
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

// executionWitness is the cross-client debug execution-witness wire
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
	//
	// CHANGE(taiko): also record which storage slots the replay writes, per
	// account. The wire-format alignment pass below needs the written slots to
	// find inserts that split an extension node in the parent trie. State-level
	// hooks only fire through a hooked state wrapper, so the EVM runs on the
	// wrapper while the replay keeps operating on the underlying statedb.
	var blockHashNums []uint64
	seenBlockHash := make(map[uint64]struct{})
	storageWrites := make(map[common.Address]map[common.Hash]struct{})
	hooks := &tracing.Hooks{
		OnBlockHashRead: func(number uint64, _ common.Hash) {
			if _, ok := seenBlockHash[number]; ok {
				return
			}
			seenBlockHash[number] = struct{}{}
			blockHashNums = append(blockHashNums, number)
		},
		OnStorageChange: func(addr common.Address, slot common.Hash, _, _ common.Hash) {
			slots, ok := storageWrites[addr]
			if !ok {
				slots = make(map[common.Hash]struct{})
				storageWrites[addr] = slots
			}
			slots[slot] = struct{}{}
		},
	}
	evm := vm.NewEVM(core.NewEVMBlockContext(header, bc, &header.Coinbase), state.NewHookedState(statedb, hooks), config, vm.Config{
		Tracer: hooks,
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

	// CHANGE(taiko): the EIP-7002 withdrawal-queue and EIP-7251 consolidation-queue
	// post-execution system calls are deliberately NOT replayed: the cross-client
	// reference block executor never performs them (its requests are
	// unconditionally empty), so running them here would witness the queue
	// contracts' account state that the reference witness does not carry.
	// EIP-6110 deposit-log parsing reads logs only and touches no state.

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
	// 0xff..fe). The reference EVM touches the caller during the EIP-4788/2935
	// system calls, so the reference witness carries the account's account-trie
	// exclusion proof — including its terminal divergence node, which no other
	// touched account shares (verified against a live reference node). go-geth's
	// system calls skip the caller entirely, so an explicit read loads the
	// account and prefetches the exclusion proof. The caller never exists, so
	// the existence rule keeps its key preimage out of the response, matching
	// the reference exactly. Only done when a system call actually ran.
	if block.BeaconRoot() != nil ||
		config.IsPrague(block.Number(), block.Time()) ||
		config.IsVerkle(block.Number(), block.Time()) {
		statedb.GetBalance(params.SystemAddress)
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

	// CHANGE(taiko): post-process the collected witness into the cross-client
	// legacy witness format (storage-trie root nodes, extension-split reveals,
	// created bytecode, and existence-filtered keys).
	if err := alignWitnessForWireFormat(bc, parent.Root, statedb, witness, storageWrites); err != nil {
		return nil, nil, err
	}

	return statedb.Witness(), committed, nil
}

// CHANGE(taiko): alignWitnessForWireFormat post-processes the collected witness
// so the RPC response matches the cross-client legacy execution-witness format.
// For every account loaded during the replay (the 20-byte witness keys):
//
//   - state additionally carries the account's parent-state storage-trie root
//     node even when the replay never walked its storage. The root node of an
//     empty (or absent) storage trie is the RLP empty string, 0x80. Accounts
//     whose storage the replay did walk already carry their root node, so the
//     set-insert is a no-op for them.
//   - codes additionally carries bytecode deployed during the replay: code is
//     otherwise only witnessed when read, so a contract created but never
//     called afterwards would be missing.
//   - keys keeps only accounts that still exist when the replay finishes.
//     Accounts that were merely probed (absent system contracts, the system
//     caller) or cleared during finalization carry account-trie exclusion
//     proofs in state, but no key preimage.
//   - state additionally carries the child node of every extension node that
//     an insert splits. The insert's exclusion proof ends at the extension, but
//     the reference sparse-trie mutation cannot restructure the extension
//     without its child revealed. Deletions need no equivalent: collapsing a
//     branch resolves the surviving sibling through the witness-tracking trie
//     reader already.
func alignWitnessForWireFormat(bc *core.BlockChain, parentRoot common.Hash, statedb *state.StateDB, witness *stateless.Witness, storageWrites map[common.Address]map[common.Hash]struct{}) error {
	preState, err := bc.StateAt(parentRoot)
	if err != nil {
		return fmt.Errorf("failed to open pre-state at %s: %w", parentRoot, err)
	}
	reader, err := bc.TrieDB().NodeReader(parentRoot)
	if err != nil {
		return fmt.Errorf("failed to open trie reader at %s: %w", parentRoot, err)
	}
	// Snapshot the loaded account addresses first: the statedb reads below may
	// re-record keys of absent accounts, which are pruned at the end.
	addrs := make([]common.Address, 0, len(witness.Keys))
	for key := range witness.Keys {
		if len(key) == common.AddressLength {
			addrs = append(addrs, common.BytesToAddress([]byte(key)))
		}
	}
	for _, addr := range addrs {
		// Parent-state storage-trie root node (0x80 for empty or absent tries).
		storageRoot := preState.GetStorageRoot(addr)
		if storageRoot == (common.Hash{}) || storageRoot == types.EmptyRootHash {
			witness.AddState(map[string][]byte{"": {0x80}}, types.EmptyRootHash)
		} else {
			owner := crypto.Keccak256Hash(addr.Bytes())
			blob, err := reader.Node(owner, nil, storageRoot)
			if err != nil || len(blob) == 0 {
				return fmt.Errorf("storage-trie root node %s of account %s unavailable: %w", storageRoot, addr, err)
			}
			witness.AddState(map[string][]byte{"": blob}, owner)
		}
		// Bytecode deployed during the replay. The GetCode read records the blob
		// in the witness.
		postCodeHash := statedb.GetCodeHash(addr)
		if postCodeHash != (common.Hash{}) && postCodeHash != types.EmptyCodeHash && postCodeHash != preState.GetCodeHash(addr) {
			statedb.GetCode(addr)
		}
		// Extension-split reveal for accounts created by the replay.
		if !preState.Exist(addr) && statedb.Exist(addr) {
			if err := revealExtensionChild(reader, witness, common.Hash{}, parentRoot, crypto.Keccak256(addr.Bytes())); err != nil {
				return err
			}
		}
		// Keys carry only accounts existing post-execution.
		if !statedb.Exist(addr) {
			witness.DeleteKey(addr.Bytes())
		}
	}
	// Extension-split reveal for storage slots inserted by the replay. Written
	// slots are cached in the statedb, so the post-value reads stay off the trie
	// and record nothing new.
	for addr, slots := range storageWrites {
		storageRoot := preState.GetStorageRoot(addr)
		if storageRoot == (common.Hash{}) || storageRoot == types.EmptyRootHash {
			continue // no parent storage trie, nothing to split
		}
		owner := crypto.Keccak256Hash(addr.Bytes())
		for slot := range slots {
			if preState.GetState(addr, slot) != (common.Hash{}) {
				continue // update or delete, not an insert
			}
			if statedb.GetState(addr, slot) == (common.Hash{}) {
				continue // written back to zero, no leaf inserted
			}
			if err := revealExtensionChild(reader, witness, owner, storageRoot, crypto.Keccak256(slot.Bytes())); err != nil {
				return err
			}
		}
	}
	return nil
}

// CHANGE(taiko): revealExtensionChild walks the trie rooted at root along the
// hashed key of an inserted leaf. If the walk diverges inside an extension
// node's key — the insert splits the extension — the extension's child node is
// added to the witness state. All other divergence shapes (vacant branch slot,
// mismatching leaf) need no extra node: the insert's exclusion proof already
// carries everything the reference sparse-trie mutation resolves.
func revealExtensionChild(reader database.NodeReader, witness *stateless.Witness, owner common.Hash, root common.Hash, hashedKey []byte) error {
	keyNibbles := make([]byte, 0, 2*len(hashedKey))
	for _, b := range hashedKey {
		keyNibbles = append(keyNibbles, b>>4, b&0x0f)
	}
	path := make([]byte, 0, len(keyNibbles))
	blob, err := reader.Node(owner, path, root)
	if err != nil || len(blob) == 0 {
		return fmt.Errorf("trie node %s at root of %s unavailable: %w", root, owner, err)
	}
	for {
		elems, _, err := rlp.SplitList(blob)
		if err != nil {
			return fmt.Errorf("malformed trie node at path %x of %s: %w", path, owner, err)
		}
		count, err := rlp.CountValues(elems)
		if err != nil {
			return fmt.Errorf("malformed trie node at path %x of %s: %w", path, owner, err)
		}
		switch count {
		case 17: // branch node: step into the child at the next key nibble
			if len(path) >= len(keyNibbles) {
				return nil
			}
			rest := elems
			for i := 0; i < int(keyNibbles[len(path)]); i++ {
				if _, _, rest, err = rlp.Split(rest); err != nil {
					return fmt.Errorf("malformed branch node at path %x of %s: %w", path, owner, err)
				}
			}
			kind, child, _, err := rlp.Split(rest)
			if err != nil {
				return fmt.Errorf("malformed branch node at path %x of %s: %w", path, owner, err)
			}
			if kind == rlp.List { // embedded child: its bytes are already witnessed with the branch
				return nil
			}
			if len(child) == 0 { // vacant slot: the insert lands here
				return nil
			}
			path = append(path, keyNibbles[len(path)])
			if blob, err = reader.Node(owner, path, common.BytesToHash(child)); err != nil || len(blob) == 0 {
				return fmt.Errorf("trie node at path %x of %s unavailable: %w", path, owner, err)
			}
		case 2: // short node: leaf or extension
			compact, rest, err := rlp.SplitString(elems)
			if err != nil {
				return fmt.Errorf("malformed short node at path %x of %s: %w", path, owner, err)
			}
			nibbles, isLeaf := compactToNibbles(compact)
			if isLeaf {
				return nil // divergent or matching leaf: revealed by the exclusion proof
			}
			if remaining := keyNibbles[len(path):]; len(remaining) >= len(nibbles) && bytes.Equal(remaining[:len(nibbles)], nibbles) {
				// Key continues through the extension: follow its child.
				kind, child, _, err := rlp.Split(rest)
				if err != nil {
					return fmt.Errorf("malformed extension node at path %x of %s: %w", path, owner, err)
				}
				if kind == rlp.List {
					return nil // embedded child, witnessed with the extension
				}
				path = append(path, nibbles...)
				if blob, err = reader.Node(owner, path, common.BytesToHash(child)); err != nil || len(blob) == 0 {
					return fmt.Errorf("trie node at path %x of %s unavailable: %w", path, owner, err)
				}
				continue
			}
			// The insert splits this extension: reveal its child node.
			kind, child, _, err := rlp.Split(rest)
			if err != nil {
				return fmt.Errorf("malformed extension node at path %x of %s: %w", path, owner, err)
			}
			if kind == rlp.List {
				return nil // embedded child, witnessed with the extension
			}
			childPath := append(append([]byte{}, path...), nibbles...)
			blob, err := reader.Node(owner, childPath, common.BytesToHash(child))
			if err != nil || len(blob) == 0 {
				return fmt.Errorf("extension child node at path %x of %s unavailable: %w", childPath, owner, err)
			}
			witness.AddState(map[string][]byte{"": blob}, owner)
			return nil
		default:
			return fmt.Errorf("unexpected trie node with %d items at path %x of %s", count, path, owner)
		}
	}
}

// compactToNibbles decodes a hex-prefix (compact) encoded trie path into its
// nibbles and reports whether the node is a leaf.
func compactToNibbles(compact []byte) ([]byte, bool) {
	if len(compact) == 0 {
		return nil, false
	}
	flag := compact[0] >> 4
	isLeaf := flag >= 2
	nibbles := make([]byte, 0, 2*len(compact))
	if flag&1 == 1 { // odd length: first nibble lives in the flag byte
		nibbles = append(nibbles, compact[0]&0x0f)
	}
	for _, b := range compact[1:] {
		nibbles = append(nibbles, b>>4, b&0x0f)
	}
	return nibbles, isLeaf
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
