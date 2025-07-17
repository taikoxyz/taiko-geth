package miner

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

type SimulateTxRequest struct {
	RawTx   []byte                  `json:"tx"`
	StateId uint64                  `json:"stateId"`
	Tx      *types.Transaction      `json:"-"`
	SimRes  chan SimulationResponse `json:"-"`
}

type SimulateAnchorTx struct {
	Tx        *types.Transaction      `json:"-"`
	BlockEnv  common.BlockEnv         `json:"-"`
	SimRes    chan SimulationResponse `json:"-"`
	ExtraData string                  `json:"-"`
}

type SimulateAnchorTxResponse struct {
	StateId uint64 `json:"stateId"`
	Err     error  `json:"err"`
	GasUsed uint64 `json:"gasUsed"`
}

type SealBlockResponse struct {
	block                    *types.Block
	cumulativeBuilderPayment string
	cumulativeGasUsed        uint64
	err                      error
}

func (s SealBlockResponse) Block() *types.Block {
	return s.block
}

func (s SealBlockResponse) CumulativeBuilderPayment() string {
	return s.cumulativeBuilderPayment
}

func (s SealBlockResponse) CumulativeGasUsed() uint64 {
	return s.cumulativeGasUsed
}

func (s SealBlockResponse) Err() error {
	return s.err
}

type SealBlockRequest struct {
	StateId  uint64
	Response chan SealBlockResponse
}

type ReqCommitState struct {
	StateId uint64                   `json:"stateId"`
	SimRes  chan CommitStateResponse `json:"-"`
}

type InnerCommitState struct {
	StateId uint32
}

type SimulationResponse struct {
	stateId        uint64
	error          error
	gasUsed        uint64
	builderPayment *hexutil.U256
}

type CommitStateResponse struct {
	cumulativeGasUsed        uint64
	cumulativeBuilderPayment *hexutil.U256
	error                    error
}

func (c CommitStateResponse) CumulativeGasUsed() uint64 {
	return c.cumulativeGasUsed
}

func (c CommitStateResponse) CumulativeBuilderPayment() *hexutil.U256 {
	return c.cumulativeBuilderPayment
}

func (c CommitStateResponse) Error() error {
	return c.error
}

func (s SimulationResponse) Error() error {
	return s.error
}

func (s SimulationResponse) StateId() uint64 {
	return s.stateId
}

func (s SimulationResponse) GasUsed() uint64 {
	return s.gasUsed
}

func (s SimulationResponse) BuilderPayment() *hexutil.U256 {
	return s.builderPayment
}
