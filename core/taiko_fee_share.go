package core

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// etnaBasefeeSharing returns the base-fee share of an Etna block context with
// the given extraData. Only the 13-byte Etna layout carries one; any other
// length leaves the base fee undistributed.
func etnaBasefeeSharing(extra []byte) (pctg uint8, redistribute bool) {
	if len(extra) != params.EtnaExtraDataLen {
		return 0, false
	}
	return extra[params.ShastaExtraDataBasefeeSharingPctgIndex], true
}

// TaikoRPCBasefeeSharing returns the base-fee share of an eth_call,
// eth_estimateGas, eth_simulateV1, eth_createAccessList or debug_traceCall
// message executed in the block context of header. From Etna on, a 13-byte
// extraData shares extra[0] with the coinbase, and any other length (an Etna
// genesis) redistributes nothing. Before Etna, calls keep sending the whole
// base fee to the treasury.
func TaikoRPCBasefeeSharing(config *params.ChainConfig, header *types.Header) (pctg uint8, redistribute bool) {
	if !config.IsEtna(header.Time) {
		return 0, true
	}
	return etnaBasefeeSharing(header.Extra)
}

// SetTaikoRPCBasefeeSharing sets the base-fee share of an RPC message executed
// in the block context of header, as TaikoRPCBasefeeSharing returns it. Before
// Etna it leaves msg untouched: the zero share of a message built from call
// arguments already sends the whole base fee to the treasury.
func SetTaikoRPCBasefeeSharing(msg *Message, config *params.ChainConfig, header *types.Header) {
	if !config.IsEtna(header.Time) {
		return
	}
	pctg, redistribute := TaikoRPCBasefeeSharing(config, header)
	msg.BasefeeSharingPctg, msg.SkipBasefeeRedistribution = pctg, !redistribute
}
