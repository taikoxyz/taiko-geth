package ethapi

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// taikoSimulateHeaderError is the eth_simulateV1 error of a simulated Etna
// block whose header breaks an Etna rule the reference client's block
// executor checks before running the block's calls. The reference answers it
// as an internal error, so this one answers -32603.
type taikoSimulateHeaderError struct{ msg string }

func (e *taikoSimulateHeaderError) Error() string  { return e.msg }
func (e *taikoSimulateHeaderError) ErrorCode() int { return errCodeInternalError }

// checkTaikoSimulateHeader returns a taikoSimulateHeaderError for an Etna
// header of a Taiko chain without a non-zero parent beacon root or without
// 13-byte extraData, in that order, and nil for any other header. The root
// messages are those of the Taiko engine's block assembly, which reports the
// same failures.
func checkTaikoSimulateHeader(config *params.ChainConfig, header *types.Header) error {
	if !config.Taiko || !config.IsEtna(header.Time) {
		return nil
	}
	switch {
	case header.ParentBeaconRoot == nil:
		return &taikoSimulateHeaderError{msg: fmt.Sprintf("parent beacon root missing: have nil, want %v", common.Hash{})}
	case *header.ParentBeaconRoot == (common.Hash{}):
		return &taikoSimulateHeaderError{msg: fmt.Sprintf("invalid parent beacon root: Etna block %v requires a non-zero root", header.Number)}
	case len(header.Extra) != params.EtnaExtraDataLen:
		return &taikoSimulateHeaderError{msg: fmt.Sprintf("Etna block %v requires %d-byte extraData, got %d bytes", header.Number, params.EtnaExtraDataLen, len(header.Extra))}
	}
	return nil
}
