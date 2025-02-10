package ethapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
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

func (s *TransactionAPI) SimulateAnchorTx(ctx context.Context, input hexutil.Bytes, env common.BlockEnv, extraData string) (map[string]interface{}, error) {

	log.Info("SimulateAnchorTx", "input", input, "env", env, "extraData", extraData)

	tx := new(types.Transaction)
	if err := rlp.DecodeBytes(input, &tx); err != nil {
		log.Warn("PRECONF: RLP decodin failed, trying unmarshalBinary", "error", err)
		err = tx.UnmarshalBinary(input)
		if err != nil {
			log.Error("PRECONF: unmarshalBinary also failed. call failure", "error", err)
			return nil, err
		}
	}

	// log tx details
	log.Info("SimulateAnchorTx",
		"type", tx.Type(),
		"chainId", tx.ChainId(),
		"maxFeePerGas", tx.GasFeeCap(),
		"gasPrice", tx.GasPrice(),
		"nonce", tx.Nonce(),
		"gasLimit", tx.Gas(),
		"to", tx.To(),
		"value", tx.Value(),
		"data", hexutil.Encode(tx.Data()),
		"hash", tx.Hash())

	resCh := make(chan miner.SimulationResponse)
	miner.SimAnchorTx <- miner.SimulateAnchorTx{
		Tx:        tx,
		BlockEnv:  env,
		SimRes:    resCh,
		ExtraData: extraData,
	}
	return handleResponse(resCh)
}

func (s *TransactionAPI) SimulateTxAtState(ctx context.Context, input hexutil.Bytes, stateId uint64) (map[string]interface{}, error) {
	log.Info("GTC-API: SimulateTxAtState", "input", input, "stateId", stateId)
	tx := new(types.Transaction)
	if err := rlp.DecodeBytes(input, &tx); err != nil {
		log.Warn("PRECONF: RLP decodin failed, trying unmarshalBinary", "error", err)
		err = tx.UnmarshalBinary(input)
		if err != nil {
			log.Error("PRECONF: unmarshalBinary also failed. call failure", "error", err)
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

func (s *TransactionAPI) SealBlock(ctx context.Context, stateId uint64) (map[string]interface{}, error) {
	log.Info("GTC-API: SealBlock", "stateId", stateId)
	resCh := make(chan miner.SealBlockResponse, 1)
	miner.SealBlock <- miner.SealBlockRequest{
		StateId:  stateId,
		Response: resCh,
	}
	res := <-resCh
	retMap := make(map[string]interface{})
	retMap["cumulative_builder_payment"] = res.CumulativeBuilderPayment()
	retMap["cumulative_gas_used"] = res.CumulativeGasUsed()
	retMap["built_block"] = RPCMarshalBlock(res.Block(), true, true, s.b.ChainConfig())

	log.Info("GTC-API: SealBlock", "retMap", retMap)

	return retMap, res.Err()
}

func handleResponse(resCh chan miner.SimulationResponse) (map[string]interface{}, error) {
	res := <-resCh

	log.Info("GTC-API: handleResponse", "res", res)

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
		var revertError miner.CommitError // GTC change: miner.RevertCommitError

		log.Info("GTC-API: handleResponse", "error", err, "error type", fmt.Sprintf("%T", err))

		switch {
		case errors.As(err, &revertError):
			log.Info("GTC-API: handleResponse", "revertError", revertError)
			errData["gas_used"] = hexutils.BytesToHex([]byte(strconv.FormatUint(res.GasUsed(), 10)))
			errData["builder_payment"] = res.BuilderPayment().String()
			errData["state_id"] = res.StateId()
			retMap := createExecutionResult("revert", errData, res.StateId())
			return retMap, nil
		default:
			log.Info("GTC-API: handleResponse", "default")
			errData["reason"] = err.Error()
			retMap := createExecutionResult("invalid", errData, res.StateId())
			return retMap, nil
		}
	} else {
		log.Info("GTC-API: handleResponse", "success")
		successData := map[string]interface{}{
			"gas_used":        fmt.Sprintf("0x%x", res.GasUsed()),
			"builder_payment": res.BuilderPayment().String(),
			"state_id":        res.StateId(),
		}
		retMap := createExecutionResult("success", successData, res.StateId())
		return retMap, nil
	}
}
