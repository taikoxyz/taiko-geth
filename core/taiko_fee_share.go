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

// setEtnaBasefeeSharing sets the base-fee share of a message executed in the
// block context of header, from Etna on: only an extraData of exactly 13 bytes
// carries a share, extra[0]. Any other length burns the base fee, as in the
// reference client, which never redistributes it without an authoritative fee
// context. Before Etna it leaves msg untouched.
//
// ApplyTransaction applies it to every header with an Etna timestamp that
// reaches it: the miner's sealing and transaction-pool preselection, and the
// test chain generator. Canonical import (StateProcessor.Process) never calls
// ApplyTransaction and decodes the fee share itself. A sealed Etna block
// always carries 13 bytes (the engine API rejects any other length), so only
// preselection's simulated headers can burn:
//   - the child of an Etna genesis, which inherits the genesis' empty or
//     7-byte extraData verbatim;
//   - in the fork window, the time.Now() child of a pre-Etna parent. It
//     carries the miner's current extraData: normally the last sealed block's
//     7 bytes, or the client-version default on a node that never sealed, so
//     it burns; once the node has built its first Etna payload it carries 13
//     bytes and shares extra[0].
func setEtnaBasefeeSharing(msg *Message, config *params.ChainConfig, header *types.Header) {
	if !config.IsEtna(header.Time) {
		return
	}
	var redistribute bool
	msg.BasefeeSharingPctg, redistribute = etnaBasefeeSharing(header.Extra)
	msg.SkipBasefeeRedistribution = !redistribute
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
	setEtnaBasefeeSharing(msg, config, header)
}
