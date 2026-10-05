package tracers

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// TestTraceCallEtnaBasefeeSharing pins that debug_traceCall at an Etna block
// shares the call's base fee as the block's extraData does: a quarter to the
// beneficiary and the rest to the treasury.
func TestTraceCallEtnaBasefeeSharing(t *testing.T) {
	backend, block := newEtnaTraceBackend(t, 25)
	DefaultDirectory.Register(balanceDeltaTracer, newBalanceDeltaTracer, false)

	from, to := taiko.GoldenTouchAccount, common.HexToAddress("0x00000000000000000000000000000000000000cc")
	gas := hexutil.Uint64(params.TxGas)
	args := ethapi.TransactionArgs{
		From:                 &from,
		To:                   &to,
		Gas:                  &gas,
		MaxFeePerGas:         (*hexutil.Big)(big.NewInt(2 * params.ShastaInitialBaseFee)),
		MaxPriorityFeePerGas: (*hexutil.Big)(big.NewInt(etnaTraceTip)),
	}
	tracer := balanceDeltaTracer
	result, err := NewAPI(backend).TraceCall(context.Background(), args, rpc.BlockNumberOrHashWithHash(block.Hash(), false), &TraceCallConfig{TraceConfig: TraceConfig{Tracer: &tracer}})
	if err != nil {
		t.Fatalf("TraceCall: %v", err)
	}
	var got map[common.Address]*big.Int
	if err := json.Unmarshal(result.(json.RawMessage), &got); err != nil {
		t.Fatalf("decode trace: %v", err)
	}

	baseFee := new(big.Int).Mul(big.NewInt(int64(params.TxGas)), big.NewInt(params.ShastaInitialBaseFee))
	share := new(big.Int).Div(new(big.Int).Mul(baseFee, big.NewInt(25)), big.NewInt(100))
	tip := new(big.Int).Mul(big.NewInt(int64(params.TxGas)), big.NewInt(etnaTraceTip))
	treasury := core.TaikoTreasuryAddress(backend.chainConfig.ChainID)
	for addr, want := range map[common.Address]*big.Int{
		treasury:             new(big.Int).Sub(baseFee, share),
		etnaTraceBeneficiary: new(big.Int).Add(share, tip),
	} {
		if got[addr] == nil || got[addr].Cmp(want) != 0 {
			t.Fatalf("balance change of %v = %v, want %v", addr, got[addr], want)
		}
	}
}
