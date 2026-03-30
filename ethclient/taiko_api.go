package ethclient

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

// HeadL1Origin returns the latest L2 block's corresponding L1 origin.
func (ec *Client) HeadL1Origin(ctx context.Context) (*rawdb.L1Origin, error) {
	var res *rawdb.L1Origin

	if err := ec.c.CallContext(ctx, &res, "taiko_headL1Origin"); err != nil {
		return nil, err
	}

	return res, nil
}

// L1OriginByID returns the L2 block's corresponding L1 origin.
func (ec *Client) L1OriginByID(ctx context.Context, blockID *big.Int) (*rawdb.L1Origin, error) {
	var res *rawdb.L1Origin

	if err := ec.c.CallContext(ctx, &res, "taiko_l1OriginByID", hexutil.EncodeBig(blockID)); err != nil {
		return nil, err
	}

	return res, nil
}

// LastL1OriginByBatchID returns the L1 origin of the last block for the given batch.
func (ec *Client) LastL1OriginByBatchID(ctx context.Context, batchID *big.Int) (*rawdb.L1Origin, error) {
	var res *rawdb.L1Origin

	if err := ec.c.CallContext(ctx, &res, "taikoAuth_lastL1OriginByBatchID", hexutil.EncodeBig(batchID)); err != nil {
		return nil, err
	}

	return res, nil
}

// LastBlockIDByBatchID returns the ID of the last block for the given batch.
func (ec *Client) LastBlockIDByBatchID(ctx context.Context, batchID *big.Int) (*hexutil.Big, error) {
	var res *hexutil.Big

	if err := ec.c.CallContext(ctx, &res, "taikoAuth_lastBlockIDByBatchID", hexutil.EncodeBig(batchID)); err != nil {
		return nil, err
	}

	return res, nil
}

// LastCertainBlockIDByBatchID returns the ID of the last block for the given batch in the rawdb.
func (ec *Client) LastCertainBlockIDByBatchID(ctx context.Context, batchID *big.Int) (*hexutil.Big, error) {
	var res *hexutil.Big

	if err := ec.c.CallContext(ctx, &res, "taikoAuth_lastCertainBlockIDByBatchID", hexutil.EncodeBig(batchID)); err != nil {
		return nil, err
	}

	return res, nil
}

// LastCertainL1OriginByBatchID returns the L1 origin of the last block for the given batch in the rawdb.
func (ec *Client) LastCertainL1OriginByBatchID(ctx context.Context, batchID *big.Int) (*rawdb.L1Origin, error) {
	var res *rawdb.L1Origin

	if err := ec.c.CallContext(ctx, &res, "taikoAuth_lastCertainL1OriginByBatchID", hexutil.EncodeBig(batchID)); err != nil {
		return nil, err
	}

	return res, nil
}

// GetSyncMode returns the current sync mode of the L2 node.
func (ec *Client) GetSyncMode(ctx context.Context) (string, error) {
	var res string

	if err := ec.c.CallContext(ctx, &res, "taiko_getSyncMode"); err != nil {
		return "", err
	}

	return res, nil
}
