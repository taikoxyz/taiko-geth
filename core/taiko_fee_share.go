package core

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// EtnaSimulationExtraData returns the extraData that a simulation runs an Etna
// child of parent with, before the child's real extraData exists.
//
// A 13-byte parent is copied, which models an inherited fee share and anchor.
// A 7-byte parent (the last Shasta or Unzen block) keeps its fee share and
// proposal ID with a zero anchor block number, and an empty genesis gets 13
// zero bytes. The anchor block number takes part in no execution, so these
// defaults cannot change a simulation result. Any other length is returned
// unchanged: block execution rejects it, and the simulated child then runs
// without base-fee redistribution.
func EtnaSimulationExtraData(parent *types.Header) []byte {
	switch {
	case len(parent.Extra) == params.ShastaExtraDataLen:
		extra := make([]byte, params.EtnaExtraDataLen)
		copy(extra, parent.Extra)
		return extra
	case len(parent.Extra) == 0 && parent.Number.Sign() == 0:
		return make([]byte, params.EtnaExtraDataLen)
	default:
		return common.CopyBytes(parent.Extra)
	}
}

// etnaBasefeeSharing returns the base-fee share of an Etna block context with
// the given extraData. Only the 13-byte Etna layout carries one; any other
// length leaves the base fee undistributed.
func etnaBasefeeSharing(extra []byte) (pctg uint8, redistribute bool) {
	if len(extra) != params.EtnaExtraDataLen {
		return 0, false
	}
	return extra[params.ShastaExtraDataBasefeeSharingPctgIndex], true
}
