package ethapi

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// taikoSimulateRootError is the eth_simulateV1 error of a simulated Etna block
// without a non-zero parent beacon root. The reference client's block executor
// rejects such a block before running its calls and answers an internal
// error, so this one answers -32603.
type taikoSimulateRootError struct{ msg string }

func (e *taikoSimulateRootError) Error() string  { return e.msg }
func (e *taikoSimulateRootError) ErrorCode() int { return errCodeInternalError }

// checkTaikoSimulateRoot returns a taikoSimulateRootError for an Etna header
// of a Taiko chain whose parent beacon root is missing or zero, and nil for
// any other header. The messages are those of the Taiko engine's block
// assembly, which reports the same failures.
func checkTaikoSimulateRoot(config *params.ChainConfig, header *types.Header) error {
	if !config.Taiko || !config.IsEtna(header.Time) {
		return nil
	}
	switch {
	case header.ParentBeaconRoot == nil:
		return &taikoSimulateRootError{msg: fmt.Sprintf("parent beacon root missing: have nil, want %v", common.Hash{})}
	case *header.ParentBeaconRoot == (common.Hash{}):
		return &taikoSimulateRootError{msg: fmt.Sprintf("invalid parent beacon root: Etna block %v requires a non-zero root", header.Number)}
	}
	return nil
}
