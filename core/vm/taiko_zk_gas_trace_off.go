//go:build !zkgastrace

package vm

import "github.com/ethereum/go-ethereum/common"

// CHANGE(taiko): without the zkgastrace build tag the zk gas trace exporter is off. The hooks in
// the production files sit behind this false constant, so the compiler drops them; the empty
// functions below only keep those calls type-checked. See taiko_zk_gas_trace.go.
const zkGasTraceEnabled = false

func zkGasTraceStepPre(*EVM, *Contract, *Stack, *Memory, uint64, OpCode, uint64) {}

func zkGasTraceSpawnCharged(*ZkGasMeter, int, *zkGasPendingStep, error) {}

func zkGasTraceStepCharged(*ZkGasMeter, int, *zkGasPendingStep, uint64, uint64, error) {}

func zkGasTracePrecompileCharged(*ZkGasMeter, common.Address, uint64, uint64, error, error) {}

func zkGasTraceIntrinsicCharged(*ZkGasMeter, error) {}

func zkGasTraceCommitted(*ZkGasMeter) {}

func zkGasTraceReset(*ZkGasMeter) {}
