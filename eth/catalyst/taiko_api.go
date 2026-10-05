package catalyst

import (
	"slices"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/log"
)

// taikoEngineCapabilities is the engine_exchangeCapabilities response on Taiko
// chains. engine_exchangeCapabilities itself is served but never listed.
var taikoEngineCapabilities = []string{"engine_forkchoiceUpdatedV3", "engine_getPayloadV5", "engine_newPayloadV4"}

// TaikoEngineAPI is the only engine-namespace service mounted on Taiko chains.
// It serves engine_exchangeCapabilities, engine_forkchoiceUpdatedV3,
// engine_getPayloadV5 and engine_newPayloadV4; every other engine_* method is
// unregistered and answers -32601.
//
// The ConsensusAPI is an unexported, non-embedded field, so none of its
// methods are served.
type TaikoEngineAPI struct {
	api *ConsensusAPI
}

// NewTaikoEngineAPI creates the Taiko Engine API service for the given backend.
func NewTaikoEngineAPI(eth *eth.Ethereum) *TaikoEngineAPI {
	return &TaikoEngineAPI{api: NewConsensusAPI(eth)}
}

// ExchangeCapabilities returns the Engine API methods served on Taiko chains.
func (t *TaikoEngineAPI) ExchangeCapabilities([]string) []string {
	return slices.Clone(taikoEngineCapabilities)
}

// GetPayloadV5 returns a payload built by engine_forkchoiceUpdatedV3.
//
//  1. An unknown or evicted ID returns -38001. The ID's version byte is not
//     checked.
//  2. The job's own block timestamp must be in Osaka (Unzen on Taiko networks)
//     and not in Amsterdam, else -38005.
//  3. A job without a built block returns -32603. A failed build stores no job,
//     so this only guards the queue.
func (t *TaikoEngineAPI) GetPayloadV5(payloadID engine.PayloadID) (*engine.TaikoExecutionPayloadEnvelopeV5, error) {
	log.Trace("Engine API request received", "method", "GetPayloadV5", "id", payloadID)
	envelope := t.api.localBlocks.get(payloadID, false)
	if envelope == nil {
		return nil, engine.UnknownPayload
	}
	data := envelope.ExecutionPayload
	if data == nil {
		return nil, engine.InternalError
	}
	config := t.api.config()
	if !config.IsOsaka(config.LondonBlock, data.Timestamp) || config.IsAmsterdam(config.LondonBlock, data.Timestamp) {
		return nil, engine.UnsupportedFork
	}
	return engine.NewTaikoExecutionPayloadEnvelopeV5(data), nil
}
