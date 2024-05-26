package mevcommit

import (
	"encoding/hex"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/log"

	"context"
	"crypto/tls"
	"errors"
	"math/big"
	"time"

	providerapiv1 "github.com/primevprotocol/mev-commit/gen/go/rpc/providerapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type IProviderService interface {
	Run()
	BlockValue() (int64, int64) // block_number, total bid amount
}

type Provider struct {
	eth         *eth.Ethereum
	server      string
	curBlockNum atomic.Int64
	blockValue  atomic.Int64
	blockNumCh  chan int64
	mu          sync.Mutex
}

func NewProvider(eth *eth.Ethereum, server string) IProviderService {
	return &Provider{
		eth:        eth,
		server:     server,
		blockNumCh: make(chan int64),
	}
}

func (p *Provider) Run() {
	providerCtx, providerCtxCancel := context.WithCancel(context.Background())
	defer providerCtxCancel()

	go p.updater(providerCtx)
	time.Sleep(time.Second * 12)

	providerClient, err := NewProviderClient(p.server)
	if err != nil {
		log.Error("failed to create provider client", "err", err)
		return
	}
	defer providerClient.Close()

	err = providerClient.CheckAndStake()
	if err != nil {
		log.Error("failed to check and stake", "err", err)
		return
	}

	bidS, err := providerClient.ReceiveBids()
	if err != nil {
		log.Error("failed to create bid receiver", "err", err)
		return
	}

	log.Info("connected to mev-commit-provider node", "address", p.server)

	for bid := range bidS {
		log.Info("received new bid", "bid", hex.EncodeToString(bid.BidDigest))

		status := p.bidProcess(bid)

		err = providerClient.SendBidResponse(context.Background(), &providerapiv1.BidResponse{
			BidDigest: bid.BidDigest,
			Status:    status,
		})
		if err != nil {
			log.Error("failed to send bid response", "err", err)
			return
		}

		log.Info("sent bid", "status", status.String())
	}
}

func (p *Provider) bidProcess(bid *providerapiv1.Bid) providerapiv1.BidResponse_Status {
	if bid.BlockNumber != p.curBlockNum.Load() {
		log.Error("mismatched block number", "block", bid.BlockNumber)
		return providerapiv1.BidResponse_STATUS_REJECTED
	}

	for _, txn := range bid.TxHashes {
		hashBytes, err := hex.DecodeString(txn)
		if err != nil {
			log.Error("hex decode string", "txn", txn, "err", err)
			return providerapiv1.BidResponse_STATUS_REJECTED
		}

		if !p.has(common.BytesToHash(hashBytes)) {
			log.Error("tx not found in pool", "txn", txn)
			return providerapiv1.BidResponse_STATUS_REJECTED
		}
	}

	bidAmount, err := strconv.ParseInt(bid.BidAmount, 10, 64)
	if err != nil {
		log.Error("invalid bid amount", "bid", bid.BidAmount)
		return providerapiv1.BidResponse_STATUS_REJECTED
	}

	p.blockValue.Add(bidAmount)
	return providerapiv1.BidResponse_STATUS_ACCEPTED
}

func (p *Provider) has(hash common.Hash) bool {
	return p.eth.TxPool().Has(hash)
}

func (p *Provider) BlockValue() (int64, int64) {
	return p.curBlockNum.Load(), p.blockValue.Load()
}

func (p *Provider) updater(ctx context.Context) {
	eventCh := make(chan core.ChainHeadEvent)
	sub := p.eth.BlockChain().SubscribeChainHeadEvent(eventCh)

	go func() {
		defer sub.Unsubscribe()
		for curBlockNum := range eventCh {
			p.blockNumCh <- curBlockNum.Block.Header().Number.Int64()
		}
	}()

	for {
		select {
		case newBlockNum := <-p.blockNumCh:
			p.mu.Lock()
			p.curBlockNum.Store(newBlockNum + 1)
			p.blockValue.Store(0)
			p.mu.Unlock()
		case <-ctx.Done():
			close(eventCh)
			//close(p.blockNumCh)
			return
		}
	}
}

type ProviderClient struct {
	conn         *grpc.ClientConn
	client       providerapiv1.ProviderClient
	senderC      chan *providerapiv1.BidResponse
	senderClosed chan struct{}
}

func NewProviderClient(
	serverAddr string,
) (*ProviderClient, error) {
	// Since we don't know if the server has TLS enabled on its rpc
	// endpoint, we try different strategies from most secure to
	// least secure. In the future, when only TLS-enabled servers
	// are allowed, only the TLS system pool certificate strategy
	// should be used.
	var (
		conn *grpc.ClientConn
		err  error
	)
	for _, e := range []struct {
		strategy   string
		isSecure   bool
		credential credentials.TransportCredentials
	}{
		{"TLS system pool certificate", true, credentials.NewClientTLSFromCert(nil, "")},
		{"TLS skip verification", false, credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})},
		{"TLS disabled", false, insecure.NewCredentials()},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		log.Info("dialing to grpc server", "strategy", e.strategy)
		conn, err = grpc.DialContext(
			ctx,
			serverAddr,
			grpc.WithBlock(),
			grpc.WithTransportCredentials(e.credential),
		)
		if err != nil {
			log.Error("failed to dial grpc server", "err", err)
			cancel()
			continue
		}

		cancel()
		if !e.isSecure {
			log.Warn("established connection with the grpc server has potential security risk")
		}
		break
	}
	if conn == nil {
		return nil, errors.New("dialing of grpc server failed")
	}

	b := &ProviderClient{
		conn:         conn,
		client:       providerapiv1.NewProviderClient(conn),
		senderC:      make(chan *providerapiv1.BidResponse),
		senderClosed: make(chan struct{}),
	}

	if err := b.startSender(); err != nil {
		return nil, errors.Join(err, b.Close())
	}
	return b, nil
}

func (b *ProviderClient) Close() error {
	close(b.senderC)
	return b.conn.Close()
}

func (b *ProviderClient) CheckAndStake() error {
	stakeAmt, err := b.client.GetStake(context.Background(), &providerapiv1.EmptyMessage{})
	if err != nil {
		log.Error("failed to get stake amount", "err", err)
		return err
	}

	log.Info("stake amount", "stake", stakeAmt.Amount)

	stakedAmt, set := big.NewInt(0).SetString(stakeAmt.Amount, 10)
	if !set {
		log.Error("failed to parse stake amount")
		return errors.New("failed to parse stake amount")
	}

	if stakedAmt.Cmp(big.NewInt(0)) > 0 {
		log.Error("provider already staked")
		return nil
	}

	_, err = b.client.RegisterStake(context.Background(), &providerapiv1.StakeRequest{
		Amount: "10000000000000000000",
	})
	if err != nil {
		log.Error("failed to register stake", "err", err)
		return err
	}

	log.Info("staked 10 ETH")
	return nil
}

func (b *ProviderClient) startSender() error {
	stream, err := b.client.SendProcessedBids(context.Background())
	if err != nil {
		return err
	}

	go func() {
		defer close(b.senderClosed)
		for {
			select {
			case <-stream.Context().Done():
				log.Warn("closing client conn")
				return
			case resp, more := <-b.senderC:
				if !more {
					log.Warn("closed sender chan")
					return
				}
				err := stream.Send(resp)
				if err != nil {
					log.Error("failed sending response", "err", err)
				}
			}
		}
	}()

	return nil
}

// ReceiveBids opens a new RPC connection with the mev-commit node to receive bids.
// Each call to this function opens a new connection and the bids are randomly
// assigned to one of the existing connections from mev-commit node. So if you run
// multiple listeners, they will get unique bids in a non-deterministic fashion.
func (b *ProviderClient) ReceiveBids() (chan *providerapiv1.Bid, error) {
	emptyMessage := &providerapiv1.EmptyMessage{}
	bidStream, err := b.client.ReceiveBids(context.Background(), emptyMessage)
	if err != nil {
		return nil, err
	}

	bidC := make(chan *providerapiv1.Bid)
	go func() {
		defer close(bidC)
		for {
			bid, err := bidStream.Recv()
			if err != nil {
				log.Error("failed receiving bid", "err", err)
				return
			}
			select {
			case <-bidStream.Context().Done():
			case bidC <- bid:
			}
		}
	}()

	return bidC, nil
}

// SendBidResponse can be used to send the status of the bid back to the mev-commit
// node. The provider can use his own logic to decide upon the bid and once he is
// ready to make a decision, this status has to be sent back to mev-commit to decide
// what to do with this bid. The sender is a single global worker which sends back
// the messages on grpc.
func (b *ProviderClient) SendBidResponse(
	ctx context.Context,
	bidResponse *providerapiv1.BidResponse,
) error {

	select {
	case <-b.senderClosed:
		return errors.New("sender closed")
	default:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case b.senderC <- bidResponse:
		return nil
	}
}
