package core

import (
	"errors"

	"github.com/ethereum/go-ethereum/core/vm"
)

var (
	// ErrZkGasBodyPastTruncation is returned when an imported block's body
	// continues past the transaction at which execution ran out of zk gas.
	// Import truncates execution there, so the body cannot match it.
	ErrZkGasBodyPastTruncation = errors.New("block body extends past zk gas truncation point")

	// ErrZkGasDifficultyMismatch is returned when an imported block's header
	// difficulty differs from the zk gas its execution used.
	ErrZkGasDifficultyMismatch = errors.New("zk gas difficulty mismatch")
)

// IsZkGasValidationError reports whether err, as returned by block import
// (StateProcessor.Process, or BlockChain.InsertBlockWithoutSetHead, which
// returns its error unchanged), is a zk-gas limit exhaustion or a mismatch
// between the header difficulty and the block's zk gas.
func IsZkGasValidationError(err error) bool {
	return errors.Is(err, vm.ErrZkGasLimitExceeded) ||
		errors.Is(err, ErrZkGasBodyPastTruncation) ||
		errors.Is(err, ErrZkGasDifficultyMismatch)
}
