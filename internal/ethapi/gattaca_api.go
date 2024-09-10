package ethapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/status-im/keycard-go/hexutils"
	"strconv"
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

func (s *TransactionAPI) SimulateTxAtState(ctx context.Context, input hexutil.Bytes, stateId uint32) (map[string]interface{}, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		log.Warn("PRECONF: unmarshalBinary failed, trying RLP decode", "error", err)

		// Try to decode using RLP
		err = rlp.DecodeBytes(input, &tx)
		if err != nil {
			log.Error("PRECONF: RLP decoding also failed. call failure", "error", err)
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

func (s *TransactionAPI) CommitState(ctx context.Context, stateId uint32) (map[string]interface{}, error) {
	resCh := make(chan miner.CommitStateResponse, 1)
	miner.CommitCh <- miner.ReqCommitState{
		StateId: stateId,
		SimRes:  resCh,
	}
	res := <-resCh
	ret := make(map[string]interface{})
	ret["cumulative_builder_payment"] = res.CumulativeBuilderPayment()
	ret["cumulative_gas_used"] = res.CumulativeGasUsed()
	return ret, res.Error()
}

func (s *TransactionAPI) SealBlock(ctx context.Context, stateId uint32) (map[string]interface{}, error) {
	resCh := make(chan miner.SealBlockResponse, 1)
	miner.SealBlock <- miner.SealBlockRequest{
		StateId:  stateId,
		Response: resCh,
	}
	res := <-resCh
	retMap := make(map[string]interface{})
	retMap["cumulative_builder_payment"] = res.CumulativeBuilderPayment()
	retMap["built_block"] = RPCMarshalBlock(res.Block(), true, true, s.b.ChainConfig())
	return retMap, res.Err()
}

func handleResponse(resCh chan miner.SimulationResponse) (map[string]interface{}, error) {
	// Initialize result map and get response from channel
	retMap := make(map[string]interface{})
	res := <-resCh

	// Helper function to create execution result wrapper
	createExecutionResult := func(resultType string, data map[string]string, stateId uint32) map[string]interface{} {
		return map[string]interface{}{
			"state_id": stateId,
			"execution_result": map[string]interface{}{
				resultType: data,
			},
			"builder_payment": res.BuilderPayment(),
		}
	}

	// Handle response error or success
	if err := res.Error(); err != nil {
		errData := make(map[string]string)
		var haltError miner.HaltError
		var revertError miner.RevertCommitError

		switch {
		case errors.As(err, &revertError):
			errData["gas_used"] = hexutils.BytesToHex([]byte(strconv.FormatUint(res.GasUsed(), 10)))
			retMap = createExecutionResult("revert", errData, res.StateId())
		case errors.As(err, &haltError):
			errData["reason"] = err.Error()
			retMap = createExecutionResult("halt", errData, res.StateId())
		default:
			errData["reason"] = err.Error()
			retMap = createExecutionResult("invalid", errData, res.StateId())
		}
	} else {
		successData := map[string]string{
			"gas_used": fmt.Sprintf("0x%x", res.GasUsed()),
		}
		retMap = createExecutionResult("success", successData, res.StateId())
	}
	return retMap, nil
}
