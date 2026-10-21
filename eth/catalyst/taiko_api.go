package catalyst

import (
	"slices"
	"sync"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
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

	// lastPayload is the payload the last successful build stored under
	// lastPayloadID, and the only one engine_getPayloadV5 serves. The
	// upstream payload queue is not used: the reference client's payload
	// service drops each job once resolved and keeps only the last result.
	lastPayloadLock sync.RWMutex
	lastPayloadID   engine.PayloadID
	lastPayload     *miner.Payload // nil until a build succeeds, and after a drop
}

// NewTaikoEngineAPI creates the Taiko Engine API service for the given backend.
func NewTaikoEngineAPI(eth *eth.Ethereum) *TaikoEngineAPI {
	return &TaikoEngineAPI{api: NewConsensusAPI(eth)}
}

// setLastPayload makes payload, built under id, the last built payload and
// drops the previous one.
func (t *TaikoEngineAPI) setLastPayload(id engine.PayloadID, payload *miner.Payload) {
	t.lastPayloadLock.Lock()
	previous := t.lastPayload
	t.lastPayloadID, t.lastPayload = id, payload
	t.lastPayloadLock.Unlock()

	stopTaikoPayload(previous)
}

// dropLastPayload drops the last built payload if it was built under id.
func (t *TaikoEngineAPI) dropLastPayload(id engine.PayloadID) {
	t.lastPayloadLock.Lock()
	var dropped *miner.Payload
	if t.lastPayload != nil && t.lastPayloadID == id {
		dropped = t.lastPayload
		t.lastPayloadID, t.lastPayload = engine.PayloadID{}, nil
	}
	t.lastPayloadLock.Unlock()

	stopTaikoPayload(dropped)
}

// stopTaikoPayload stops the background builder of a payload that is no
// longer served. Resolve terminates it and is safe to call more than once,
// so a payload already served by engine_getPayloadV5 is unaffected; the
// envelope it returns is discarded.
func stopTaikoPayload(payload *miner.Payload) {
	if payload != nil {
		payload.Resolve()
	}
}

// resolveLastPayload returns the last built payload if it was built under id,
// and nil for any other ID.
func (t *TaikoEngineAPI) resolveLastPayload(id engine.PayloadID) *engine.ExecutionPayloadEnvelope {
	t.lastPayloadLock.RLock()
	defer t.lastPayloadLock.RUnlock()

	if t.lastPayload == nil || t.lastPayloadID != id {
		return nil
	}
	return t.lastPayload.Resolve()
}

// ExchangeCapabilities returns the Engine API methods served on Taiko chains.
func (t *TaikoEngineAPI) ExchangeCapabilities([]string) []string {
	return slices.Clone(taikoEngineCapabilities)
}

// GetPayloadV5 returns the last payload built by engine_forkchoiceUpdatedV3.
//
//  1. Any ID other than the last built payload's returns -38001, so a build
//     makes every earlier ID unknown. The last payload is served any number of
//     times. The ID's version byte is not checked.
//  2. The job's own block timestamp must be in Osaka (Unzen on Taiko networks)
//     and not in Amsterdam, else -38005.
//  3. A job without a built block returns -32603. A failed build stores no job,
//     so this only guards the store.
func (t *TaikoEngineAPI) GetPayloadV5(payloadID engine.PayloadID) (*engine.TaikoExecutionPayloadEnvelopeV5, error) {
	log.Trace("Engine API request received", "method", "GetPayloadV5", "id", payloadID)
	envelope := t.resolveLastPayload(payloadID)
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
