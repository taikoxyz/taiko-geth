package simulator

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/status-im/keycard-go/hexutils"
)

type SimulateTxResponse struct {
	StateId uint32  `json:"stateId"`
	Success GasUsed `json:"success,omitempty"`
	Revert  GasUsed `json:"revert,omitempty"`
	Halt    Reason  `json:"halt,omitempty"`
	Invalid Reason  `json:"invalid,omitempty"`
}

type GasUsed struct {
	GasUsed uint64 `json:"gasUsed"`
}

type Reason struct {
	Reason string `json:"reason"`
}

// SimulatorAPI exposes methods for simulating transactions and sealing blocks.
type SimulatorAPI struct {
	b ethapi.Backend
}

// NewSimulatorAPI creates a new RPC service with methods for simulating transactions and sealing blocks.
func NewSimulatorAPI(b ethapi.Backend) *SimulatorAPI {
	return &SimulatorAPI{b}
}

func (s *SimulatorAPI) SimulateAnchorTx(ctx context.Context, input hexutil.Bytes, env common.BlockEnv, extraData string) (map[string]interface{}, error) {
	log.Info("Simulator-API: SimulateAnchorTx")

	tx := new(types.Transaction)
	if err := rlp.DecodeBytes(input, &tx); err != nil {
		err = tx.UnmarshalBinary(input)
		if err != nil {
			log.Error("Simulator-API: SimulateAnchorTx", "rlp decoding failed", "error", err)
			return nil, err
		}
	}

	resCh := make(chan miner.SimulationResponse)
	miner.SimAnchorTx <- miner.SimulateAnchorTx{
		Tx:        tx,
		BlockEnv:  env,
		SimRes:    resCh,
		ExtraData: extraData,
	}
	return handleResponse(resCh)
}

func (s *SimulatorAPI) SimulateTxAtState(ctx context.Context, input hexutil.Bytes, stateId uint64) (map[string]interface{}, error) {
	log.Info("Simulator-API: SimulateTxAtState", "stateId", stateId)
	tx := new(types.Transaction)
	if err := rlp.DecodeBytes(input, &tx); err != nil {
		err = tx.UnmarshalBinary(input)
		if err != nil {
			log.Error("Simulator-API: SimulateTxAtState", "rlp decoding failed", "error", err)
			return nil, err
		}
	}
	resCh := make(chan miner.SimulationResponse, 1)
	miner.SimCh <- miner.SimulateTxRequest{
		RawTx:   nil,
		StateId: stateId,
		Tx:      tx,
		SimRes:  resCh,
	}
	return handleResponse(resCh)
}

func (s *SimulatorAPI) SealBlock(ctx context.Context, stateId uint64) (map[string]interface{}, error) {
	log.Info("Simulator-API: SealBlock", "stateId", stateId)
	resCh := make(chan miner.SealBlockResponse, 1)
	miner.SealBlock <- miner.SealBlockRequest{
		StateId:  stateId,
		Response: resCh,
	}
	res := <-resCh
	retMap := make(map[string]interface{})

	if res.Block() == nil {
		log.Error("Simulator-API: SealBlock", "block is nil")
		return nil, errors.New("block is nil")
	}

	retMap["cumulative_builder_payment"] = res.CumulativeBuilderPayment()
	retMap["cumulative_gas_used"] = res.CumulativeGasUsed()
	retMap["built_block"] = ethapi.RPCMarshalBlock(res.Block(), true, true, s.b.ChainConfig())

	return retMap, res.Err()
}

func handleResponse(resCh chan miner.SimulationResponse) (map[string]interface{}, error) {
	res := <-resCh

	// Helper function to create execution result wrapper
	createExecutionResult := func(resultType string, data map[string]interface{}, stateId uint64) map[string]interface{} {
		return map[string]interface{}{
			"execution_result": map[string]interface{}{
				resultType: data,
			},
		}
	}

	// Handle response error or success
	if err := res.Error(); err != nil {
		errData := make(map[string]interface{})
		var revertError miner.CommitError

		switch {
		case errors.As(err, &revertError):
			errData["gas_used"] = hexutils.BytesToHex([]byte(strconv.FormatUint(res.GasUsed(), 10)))
			errData["builder_payment"] = res.BuilderPayment().String()
			errData["state_id"] = res.StateId()
			retMap := createExecutionResult("revert", errData, res.StateId())
			return retMap, nil
		default:
			errData["reason"] = err.Error()
			retMap := createExecutionResult("invalid", errData, res.StateId())
			return retMap, nil
		}
	} else {
		successData := map[string]interface{}{
			"gas_used":        fmt.Sprintf("0x%x", res.GasUsed()),
			"builder_payment": res.BuilderPayment().String(),
			"state_id":        res.StateId(),
		}
		retMap := createExecutionResult("success", successData, res.StateId())
		return retMap, nil
	}
}
