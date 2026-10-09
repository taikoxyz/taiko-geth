package ethapi

import (
	"context"

	"github.com/ethereum/go-ethereum/rpc"
)

// taikoPendingBlockNull reports whether the backend answers the pending block
// as null without an error. A Taiko backend does so when the head or the next
// block is an Etna block, which is never built locally, so the pending-tagged
// calls that read that block answer null too, as in the reference client.
func taikoPendingBlockNull(ctx context.Context, b Backend) bool {
	block, err := b.BlockByNumber(ctx, rpc.PendingBlockNumber)
	return block == nil && err == nil
}
