package catalyst

import (
	"context"
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
)

// ForkchoiceUpdatedV3 applies the forkchoice state and, given attributes,
// seals the requested L2 block, caches it under its payload ID and records its
// L1 origin.
//
// Invalid attributes do not stop the forkchoice update: the state is applied
// without them, an INVALID or SYNCING result is returned as a plain status,
// and otherwise the attribute error is returned.
func (t *TaikoEngineAPI) ForkchoiceUpdatedV3(ctx context.Context, update engine.ForkchoiceStateV1, attrs *engine.TaikoPayloadAttributesV3) (engine.ForkChoiceResponse, error) {
	api := t.api
	api.forkchoiceLock.Lock()
	defer api.forkchoiceLock.Unlock()

	log.Trace("Engine API request received", "method", "ForkchoiceUpdatedV3", "head", update.HeadBlockHash, "finalized", update.FinalizedBlockHash, "safe", update.SafeBlockHash)
	if attrs != nil {
		// The wire decoder requires these fields; Go callers get the same answer.
		if attrs.BlockMetadata == nil || attrs.L1Origin == nil || attrs.L1Origin.BlockID == nil || attrs.BaseFeePerGas == nil {
			return engine.STATUS_INVALID, paramsErr("missing blockMetadata, l1Origin or baseFeePerGas")
		}
		if attrErr := validateTaikoForkchoiceAttributes(api.config(), attrs); attrErr != nil {
			res, _, err := t.applyForkchoice(update)
			if err != nil {
				return res, err
			}
			if res.PayloadStatus.Status != engine.VALID {
				return res, nil
			}
			return engine.STATUS_INVALID, attrErr
		}
	}
	res, head, err := t.applyForkchoice(update)
	if err != nil || res.PayloadStatus.Status != engine.VALID || attrs == nil {
		return res, err
	}
	// Taiko allows several L2 blocks per L1 block, so the target may share the
	// head's timestamp; only an earlier one is rejected.
	if attrs.Timestamp < head.Time() {
		return engine.STATUS_INVALID, attributesErr("payload attributes timestamp is before the head block")
	}
	id, err := t.buildTaikoPayload(ctx, head, &attrs.PayloadAttributes)
	if err != nil {
		return engine.STATUS_INVALID, err
	}
	res.PayloadID = &id
	return res, nil
}

// validateTaikoForkchoiceAttributes runs the engine_forkchoiceUpdatedV3
// attribute checks in order and returns the first failure.
func validateTaikoForkchoiceAttributes(config *params.ChainConfig, attrs *engine.TaikoPayloadAttributesV3) error {
	switch {
	case attrs.SlotNumber != nil:
		return attributesErr("slot number is unsupported")
	case attrs.Withdrawals == nil:
		return attributesErr("missing withdrawals")
	case attrs.BeaconRoot == nil:
		return attributesErr("missing beacon root")
	case !config.IsUnzen(attrs.Timestamp) || config.IsAmsterdam(config.LondonBlock, attrs.Timestamp):
		return unsupportedForkErr("forkchoiceUpdatedV3 must only be called for Unzen payloads")
	case attrs.TargetGasLimitSet:
		return paramsErr("target gas limit is unsupported")
	}
	if config.IsEtna(attrs.Timestamp) {
		switch {
		case attrs.BlockMetadata.Timestamp != attrs.Timestamp:
			return paramsErr("block metadata timestamp must match the payload attributes timestamp")
		case *attrs.BeaconRoot == (common.Hash{}):
			return paramsErr("a non-zero parent beacon block root is required from Etna")
		case attrs.AnchorTransactionSet:
			return paramsErr("an anchor transaction is unsupported from Etna")
		case len(attrs.Withdrawals) != 0:
			return paramsErr("withdrawals must be empty from Etna")
		case len(attrs.BlockMetadata.ExtraData) != params.EtnaExtraDataLen:
			return paramsErr("extra data must be exactly 13 bytes from Etna")
		case !attrs.BaseFeePerGas.IsUint64():
			return paramsErr("base fee per gas does not fit in 64 bits")
		}
		return nil
	}
	if *attrs.BeaconRoot != (common.Hash{}) {
		return paramsErr("non-zero parent beacon block root is unsupported before Etna")
	}
	return nil
}

// applyForkchoice makes the requested head canonical and checks the finalized
// and safe blocks. It returns the head block when the result is VALID.
//
// Unlike the upstream method, a head that is already a canonical ancestor
// rewinds the chain to it, since L2 drivers reorg their own chain.
func (t *TaikoEngineAPI) applyForkchoice(update engine.ForkchoiceStateV1) (engine.ForkChoiceResponse, *types.Block, error) {
	api := t.api
	if update.HeadBlockHash == (common.Hash{}) {
		return engine.STATUS_INVALID, nil, engine.InvalidForkChoiceState.With(errors.New("zero head block hash"))
	}
	// Stash away the last update to warn the user if the driver goes offline.
	api.lastForkchoiceUpdate.Store(time.Now().Unix())

	chain := api.eth.BlockChain()
	block := chain.GetBlockByHash(update.HeadBlockHash)
	if block == nil {
		// If this block was previously invalidated, keep rejecting it here too.
		if res := api.checkInvalidAncestor(update.HeadBlockHash, update.HeadBlockHash); res != nil {
			return engine.ForkChoiceResponse{PayloadStatus: *res}, nil, nil
		}
		header := api.remoteBlocks.get(update.HeadBlockHash)
		if header == nil {
			log.Warn("Fetching the unknown forkchoice head from network", "hash", update.HeadBlockHash)
			retrieved, err := api.eth.Downloader().GetHeader(update.HeadBlockHash)
			if err != nil {
				log.Warn("Could not retrieve unknown head from peers", "err", err)
				return engine.STATUS_SYNCING, nil, nil
			}
			api.remoteBlocks.put(retrieved.Hash(), retrieved)
			header = retrieved
		}
		finalized := api.remoteBlocks.get(update.FinalizedBlockHash)
		if finalized == nil {
			finalized = chain.GetHeaderByHash(update.FinalizedBlockHash)
		}
		log.Info("Forkchoice requested sync to new head", "number", header.Number, "hash", header.Hash())
		if err := api.eth.Downloader().BeaconSync(header, finalized); err != nil {
			log.Warn("Failed to start beacon sync", "err", err)
		}
		return engine.STATUS_SYNCING, nil, nil
	}
	// Unzen repurposes the header difficulty for zk gas, so a positive
	// difficulty marks a terminal proof-of-work block only before Unzen.
	if block.Difficulty().BitLen() > 0 && block.NumberU64() > 0 {
		ph := chain.GetHeader(block.ParentHash(), block.NumberU64()-1)
		if ph == nil {
			return engine.STATUS_INVALID, nil, engine.InternalError.With(errors.New("parent unavailable for difficulty check"))
		}
		if ph.Difficulty.Sign() == 0 && block.Difficulty().Sign() > 0 && !api.config().IsUnzen(block.Time()) {
			log.Error("Parent block is already post-ttd", "number", block.NumberU64(), "hash", update.HeadBlockHash, "diff", block.Difficulty())
			return engine.ForkChoiceResponse{PayloadStatus: engine.INVALID_TERMINAL_BLOCK}, nil, nil
		}
	}
	if chain.CurrentBlock().Hash() != update.HeadBlockHash {
		if latestValid, err := chain.SetCanonical(block); err != nil {
			log.Warn("Failed to set the forkchoice head", "number", block.NumberU64(), "hash", update.HeadBlockHash, "err", err)
			return engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.INVALID, LatestValidHash: &latestValid}}, nil, nil
		}
	}
	api.eth.SetSynced()

	if update.FinalizedBlockHash != (common.Hash{}) {
		finalBlock := chain.GetBlockByHash(update.FinalizedBlockHash)
		if finalBlock == nil {
			log.Warn("Final block not available in database", "hash", update.FinalizedBlockHash)
			return engine.STATUS_INVALID, nil, engine.InvalidForkChoiceState.With(errors.New("final block not available in database"))
		}
		if rawdb.ReadCanonicalHash(api.eth.ChainDb(), finalBlock.NumberU64()) != update.FinalizedBlockHash {
			log.Warn("Final block not in canonical chain", "number", finalBlock.NumberU64(), "hash", update.FinalizedBlockHash)
			return engine.STATUS_INVALID, nil, engine.InvalidForkChoiceState.With(errors.New("final block not in canonical chain"))
		}
		chain.SetFinalized(finalBlock.Header())
	}
	if update.SafeBlockHash != (common.Hash{}) {
		safeBlock := chain.GetBlockByHash(update.SafeBlockHash)
		if safeBlock == nil {
			log.Warn("Safe block not available in database", "hash", update.SafeBlockHash)
			return engine.STATUS_INVALID, nil, engine.InvalidForkChoiceState.With(errors.New("safe block not available in database"))
		}
		if rawdb.ReadCanonicalHash(api.eth.ChainDb(), safeBlock.NumberU64()) != update.SafeBlockHash {
			log.Warn("Safe block not in canonical chain", "number", safeBlock.NumberU64(), "hash", update.SafeBlockHash)
			return engine.STATUS_INVALID, nil, engine.InvalidForkChoiceState.With(errors.New("safe block not in canonical chain"))
		}
		chain.SetSafe(safeBlock.Header())
	}
	return engine.ForkChoiceResponse{
		PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID, LatestValidHash: &update.HeadBlockHash},
	}, block, nil
}

// buildTaikoPayload seals the block described by attrs on top of head, caches
// it under its payload ID and writes its L1 origin. A cached ID skips sealing
// and only rewrites the L1 origin for the block cached under it.
func (t *TaikoEngineAPI) buildTaikoPayload(ctx context.Context, head *types.Block, attrs *engine.PayloadAttributes) (engine.PayloadID, error) {
	api := t.api
	args := taikoBuildPayloadArgs(head.Hash(), attrs)
	id := args.Id()
	if cached := api.localBlocks.get(id, false); cached != nil {
		return id, t.writeL1Origin(attrs, cached.ExecutionPayload.BlockHash)
	}
	var parentBlockTime uint64
	if head.NumberU64() != 0 {
		if ancestor := api.eth.BlockChain().GetHeaderByHash(head.ParentHash()); ancestor != nil {
			parentBlockTime = head.Time() - ancestor.Time
		}
	}
	block, err := api.eth.Miner().SealBlockWith(head.Header(), parentBlockTime, attrs)
	if err != nil {
		log.Error("Failed to seal the Taiko block", "id", id, "err", err)
		return id, engine.InternalError.With(err)
	}
	// BuildPayload creates the payload entry around an empty block built from
	// the same arguments; the sealed block then replaces it.
	payload, err := api.eth.Miner().BuildPayload(ctx, args, false)
	if err != nil {
		log.Error("Failed to build the Taiko payload", "id", id, "err", err)
		return id, engine.InternalError.With(err)
	}
	payload.SetFullBlock(block, common.Big0)
	api.localBlocks.put(id, payload)
	return id, t.writeL1Origin(attrs, block.Hash())
}

// writeL1Origin atomically records the L1 origin of the block built from
// attrs. Unless the block is a preconfirmation, it also moves the head L1
// origin and, from Shasta on, maps the extraData proposal ID to the block.
func (t *TaikoEngineAPI) writeL1Origin(attrs *engine.PayloadAttributes, blockHash common.Hash) error {
	origin := *attrs.L1Origin
	origin.L2BlockHash = blockHash

	batch := t.api.eth.ChainDb().NewBatch()
	rawdb.WriteL1Origin(batch, origin.BlockID, &origin)
	if !origin.IsPreconfBlock() {
		rawdb.WriteHeadL1Origin(batch, origin.BlockID)
		if t.api.config().IsShasta(attrs.Timestamp) {
			if proposalID, err := core.DecodeShastaProposalID(attrs.BlockMetadata.ExtraData); err == nil {
				rawdb.WriteBatchToLastBlockID(batch, proposalID, origin.BlockID)
			}
		}
	}
	if err := batch.Write(); err != nil {
		return engine.InternalError.With(err)
	}
	return nil
}

// taikoBuildPayloadArgs fills the payload-building arguments from the
// attributes alone, never from the sealed block, so the payload ID is known
// before sealing. A zero parent beacon root builds the same pre-Etna block as
// an absent one, so only a non-zero root is set.
func taikoBuildPayloadArgs(head common.Hash, attrs *engine.PayloadAttributes) *miner.BuildPayloadArgs {
	txListHash := crypto.Keccak256Hash(attrs.BlockMetadata.TxList)
	args := &miner.BuildPayloadArgs{
		Parent:       head,
		Timestamp:    attrs.Timestamp,
		FeeRecipient: attrs.SuggestedFeeRecipient,
		Random:       attrs.Random,
		Withdrawals:  attrs.Withdrawals,
		Version:      engine.PayloadV2,
		TxListHash:   &txListHash,
		Extra:        attrs.BlockMetadata.ExtraData,
	}
	if attrs.BeaconRoot != nil && *attrs.BeaconRoot != (common.Hash{}) {
		root := *attrs.BeaconRoot
		args.BeaconRoot = &root
	}
	return args
}

// taikoPayloadID returns the payload ID of the block that attrs build on top
// of head. Pre-Etna IDs equal the IDs of the earlier V2 wire path.
func taikoPayloadID(head common.Hash, attrs *engine.PayloadAttributes) engine.PayloadID {
	return taikoBuildPayloadArgs(head, attrs).Id()
}
