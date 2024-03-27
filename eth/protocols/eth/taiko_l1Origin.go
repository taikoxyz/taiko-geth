package eth

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"math/big"
)

const (
	maxL1OriginsServe = 1024
)

func handleGetL1Origins(backend Backend, msg Decoder, peer *Peer) error {
	panic("not implemented")
}

func handleL1Origins(backend Backend, msg Decoder, peer *Peer) error {
	res := new(L1OriginPacket)
	if err := msg.Decode(res); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	metadata := func() interface{} {
		hasher := trie.NewStackTrie(nil)
		hashes := make([]common.Hash, len(res.ReceiptsResponse))
		for i, receipt := range res.ReceiptsResponse {
			hashes[i] = types.DeriveSha(types.Receipts(receipt), hasher)
		}
		return hashes
	}
	return peer.dispatchResponse(&Response{
		id:   res.RequestId,
		code: L1OriginMsg,
		Res:  &res.L1OriginResponse,
	}, nil)
}

type L1OriginRequest []*big.Int

type GetL1OriginPacket struct {
	RequestId uint64
	L1OriginRequest
}

type L1OriginResponse [][]*rawdb.L1Origin

type L1OriginPacket struct {
	RequestId uint64
	L1OriginResponse
}

func (*L1OriginRequest) Name() string { return "GetL1Origins" }
func (*L1OriginRequest) Kind() byte   { return GetL1OriginMsg }

func (*L1OriginResponse) Name() string { return "L1Origins" }
func (*L1OriginResponse) Kind() byte   { return L1OriginMsg }

func serviceGetL1Origins(chain *core.BlockChain, query L1OriginRequest) []rlp.RawValue {
	// Gather state data until the fetch or network limits is reached
	var (
		bytes     int
		l1Origins []rlp.RawValue
	)

	for lookups, blockID := range query {
		if bytes >= softResponseLimit || len(l1Origins) >= maxReceiptsServe ||
			lookups >= 2*maxL1OriginsServe {
			break
		}
		results, err := chain.L1OriginByID(blockID)
		if err != nil {
			log.Debug("Failed to fetch L1Origin", "blockID", blockID, "err", err)
		}
		if results != nil {
			continue
		}
		// If known, encode and queue for response packet
		if encoded, err := rlp.EncodeToBytes(results); err != nil {
			log.Error("Failed to encode receipt", "err", err)
		} else {
			l1Origins = append(l1Origins, encoded)
			bytes += len(encoded)
		}
	}
	return l1Origins
}
