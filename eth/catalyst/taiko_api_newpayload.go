package catalyst

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/params/forks"
)

// Errors of engine_newPayloadV4: the Etna and pre-Etna parameter errors
// (-32602), and the conversion errors answered with an uncached INVALID.
var (
	errEtnaZeroBeaconRoot   = errors.New("invalid Etna payload: zero parent beacon block root")
	errEtnaWithdrawals      = errors.New("invalid Etna payload: withdrawals must be empty")
	errEtnaBlobGas          = errors.New("invalid Etna payload: blob gas must be zero")
	errEtnaVersionedHashes  = errors.New("invalid Etna payload: blob versioned hashes must be empty")
	errEtnaRequests         = errors.New("invalid Etna payload: execution requests must be empty")
	errPreEtnaBeaconRoot    = errors.New("non-zero parent beacon block roots are unsupported before Etna")
	errPreEtnaOsakaFields   = errors.New("nonempty or nonzero Osaka fields are unsupported before Etna")
	errEtnaBlobTransactions = errors.New("blob transactions are unsupported")
)

// NewPayloadV4 validates an Osaka payload and imports it without moving the
// head. Checks run in this order, and the first failure decides the answer:
//
//  1. missing arguments: -32602 (strict payload decoding happens earlier, in
//     the JSON-RPC layer);
//  2. a timestamp outside Prague..BPO5, i.e. before Unzen or from Amsterdam on:
//     -38005;
//  3. Etna: zero root (except block 0), withdrawals, blob gas, versioned
//     hashes, execution requests: -32602;
//  4. before Etna: a nonzero root: -32602;
//  5. conversion (INVALID, never cached as invalid): Unzen side data, a base
//     fee above 64 bits, an Etna blob transaction, malformed transactions or
//     extra data, a block hash mismatch;
//  6. import, see importPayload.
func (t *TaikoEngineAPI) NewPayloadV4(ctx context.Context, payload engine.TaikoExecutionPayloadV3, versionedHashes []common.Hash, beaconRoot *common.Hash, executionRequests []hexutil.Bytes) (engine.PayloadStatusV1, error) {
	switch {
	case versionedHashes == nil:
		return invalidStatus, paramsErr("nil versionedHashes post-cancun")
	case beaconRoot == nil:
		return invalidStatus, paramsErr("nil beaconRoot post-cancun")
	case executionRequests == nil:
		return invalidStatus, paramsErr("nil executionRequests post-prague")
	case !t.api.checkFork(payload.Timestamp, forks.Prague, forks.Osaka, forks.BPO1, forks.BPO2, forks.BPO3, forks.BPO4, forks.BPO5):
		return invalidStatus, unsupportedForkErr("newPayloadV4 must only be called for unzen payloads")
	}
	config := t.api.config()
	etna := config.IsEtna(payload.Timestamp)
	if etna {
		var err error
		switch {
		case payload.Number != 0 && *beaconRoot == (common.Hash{}):
			err = errEtnaZeroBeaconRoot
		case len(payload.Withdrawals) != 0:
			err = errEtnaWithdrawals
		case payload.BlobGasUsed != 0 || payload.ExcessBlobGas != 0:
			err = errEtnaBlobGas
		case len(versionedHashes) != 0:
			err = errEtnaVersionedHashes
		case len(executionRequests) != 0:
			err = errEtnaRequests
		}
		if err != nil {
			return invalidStatus, engine.InvalidParams.With(err)
		}
	} else if *beaconRoot != (common.Hash{}) {
		return invalidStatus, engine.InvalidParams.With(errPreEtnaBeaconRoot)
	}
	// As upstream, conversion and import run under newPayloadLock, so the
	// latestValidHash of a conversion failure sees every earlier import.
	t.api.newPayloadLock.Lock()
	defer t.api.newPayloadLock.Unlock()

	// Unzen headers commit to none of the Osaka side data, so a payload that
	// carries any is rejected before conversion instead of executing data
	// absent from the block hash.
	if !etna && (len(payload.Withdrawals) != 0 || payload.BlobGasUsed != 0 || payload.ExcessBlobGas != 0 ||
		len(versionedHashes) != 0 || len(executionRequests) != 0) {
		return t.conversionInvalid(&payload, errPreEtnaOsakaFields), nil
	}
	block, err := taikoPayloadToBlock(config, &payload, *beaconRoot)
	if err != nil {
		return t.conversionInvalid(&payload, err), nil
	}
	return t.importPayload(ctx, block)
}

// taikoPayloadToBlock rebuilds the block of a newPayloadV4 payload: the header
// difficulty is the payload's headerDifficulty, the root is the given parent
// beacon block root, the requests hash is the empty requests hash, and the
// transactions and (always empty) withdrawals roots are derived from the body.
// Callers have already rejected any nonempty side data.
func taikoPayloadToBlock(config *params.ChainConfig, payload *engine.TaikoExecutionPayloadV3, beaconRoot common.Hash) (*types.Block, error) {
	if payload.BaseFeePerGas == nil || !payload.BaseFeePerGas.IsUint64() {
		return nil, fmt.Errorf("invalid baseFeePerGas: %v", payload.BaseFeePerGas)
	}
	data := payload.ToExecutableData()
	data.Withdrawals = make([]*types.Withdrawal, 0)

	// The payload carries no versioned hashes. Etna rejects any blob
	// transaction here. Before Etna a blob transaction is a block validation
	// failure found at import (and cached as invalid there), so its own blob
	// hashes are passed to keep the conversion from rejecting it.
	versionedHashes := make([]common.Hash, 0)
	for i, enc := range data.Transactions {
		if len(enc) == 0 || enc[0] != types.BlobTxType {
			continue
		}
		if config.IsEtna(payload.Timestamp) {
			return nil, fmt.Errorf("%w: transaction %d", errEtnaBlobTransactions, i)
		}
		var tx types.Transaction
		if err := tx.UnmarshalBinary(enc); err != nil {
			return nil, fmt.Errorf("invalid transaction %d: %v", i, err)
		}
		versionedHashes = append(versionedHashes, tx.BlobHashes()...)
	}
	return engine.ExecutableDataToBlock(data, versionedHashes, &beaconRoot, [][]byte{})
}

// conversionInvalid answers a payload that cannot be turned into a block. Its
// block hash proves nothing, so nothing is cached as invalid.
func (t *TaikoEngineAPI) conversionInvalid(payload *engine.TaikoExecutionPayloadV3, err error) engine.PayloadStatusV1 {
	log.Warn("Invalid newPayloadV4 payload", "number", payload.Number, "hash", payload.BlockHash, "parent", payload.ParentHash, "error", err)
	msg := err.Error()
	return engine.PayloadStatusV1{
		Status:          engine.INVALID,
		LatestValidHash: t.latestValidHashForInvalidPayload(payload.ParentHash),
		ValidationError: &msg,
	}
}

// latestValidHashForInvalidPayload returns the parent hash when the parent is
// known. Otherwise it follows the invalid-ancestor cache from the parent back
// to the first known block that is not itself cached as invalid, and returns
// nil if there is none.
func (t *TaikoEngineAPI) latestValidHashForInvalidPayload(parentHash common.Hash) *common.Hash {
	chain := t.api.eth.BlockChain()
	if chain.GetHeaderByHash(parentHash) != nil {
		return &parentHash
	}
	t.api.invalidLock.Lock()
	defer t.api.invalidLock.Unlock()

	current := parentHash
	bad, ok := t.api.invalidTipsets[current]
	for ok {
		current = bad.ParentHash
		bad, ok = t.api.invalidTipsets[current]
		if !ok && chain.GetHeaderByHash(current) != nil {
			return &current
		}
	}
	return nil
}

// importPayload is the import step of the upstream newPayload (everything
// after the block conversion), kept here so that the upstream method stays
// unchanged. The caller holds newPayloadLock. It differs in three ways:
//   - before Etna, a zk-gas exhaustion or a zk-gas/difficulty mismatch is an
//     internal error (-32603) and is not cached as invalid;
//   - there is no separate parent-timestamp check: header verification rejects
//     the timestamp, and that failure is cached like any other;
//   - a failed import answers INVALID with the parent hash as the latest valid
//     hash, whatever the parent's difficulty.
func (t *TaikoEngineAPI) importPayload(ctx context.Context, block *types.Block) (engine.PayloadStatusV1, error) {
	api := t.api

	// Stash away the last update to warn the user if the driver goes offline.
	api.lastNewPayloadUpdate.Store(time.Now().Unix())

	chain := api.eth.BlockChain()
	hash := block.Hash()
	if chain.GetBlockByHash(hash) != nil {
		log.Debug("Ignoring already known payload", "number", block.NumberU64(), "hash", hash)
		return engine.PayloadStatusV1{Status: engine.VALID, LatestValidHash: &hash}, nil
	}
	if res := api.checkInvalidAncestor(hash, hash); res != nil {
		return *res, nil
	}
	parent := chain.GetBlock(block.ParentHash(), block.NumberU64()-1)
	if parent == nil {
		return api.delayPayloadImport(block), nil
	}
	if api.eth.Downloader().ConfigSyncMode() == ethconfig.SnapSync {
		return api.delayPayloadImport(block), nil
	}
	if !chain.HasBlockAndState(block.ParentHash(), block.NumberU64()-1) {
		api.remoteBlocks.put(hash, block.Header())
		log.Warn("State not available, ignoring new payload")
		return engine.PayloadStatusV1{Status: engine.ACCEPTED}, nil
	}
	start := time.Now()
	if _, err := chain.InsertBlockWithoutSetHead(ctx, block, false); err != nil {
		if !api.config().IsEtna(block.Time()) && core.IsZkGasValidationError(err) {
			log.Warn("NewPayloadV4: zk gas failure before Etna", "number", block.NumberU64(), "hash", hash, "error", err)
			return engine.PayloadStatusV1{}, engine.InternalError.With(err)
		}
		log.Warn("NewPayloadV4: inserting block failed", "number", block.NumberU64(), "hash", hash, "error", err)

		api.invalidLock.Lock()
		api.invalidBlocksHits[hash] = 1
		api.invalidTipsets[hash] = block.Header()
		api.invalidLock.Unlock()

		// Unlike the upstream invalid response, the latest valid hash is the
		// parent hash even when the parent's difficulty is nonzero: from Unzen
		// on the difficulty carries zk gas and never marks a proof-of-work
		// parent.
		parentHash, msg := parent.Hash(), err.Error()
		return engine.PayloadStatusV1{Status: engine.INVALID, LatestValidHash: &parentHash, ValidationError: &msg}, nil
	}
	chain.SendNewPayloadEvent(core.NewPayloadEvent{
		Hash:           hash,
		Number:         block.NumberU64(),
		ProcessingTime: time.Since(start),
	})
	return engine.PayloadStatusV1{Status: engine.VALID, LatestValidHash: &hash}, nil
}
