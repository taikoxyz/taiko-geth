package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// taikoEngineTestUnzenTime is the Unzen (and Cancun, Prague, Osaka) activation
// time of the Taiko test chain, so timestamp 49 is pre-Unzen.
const taikoEngineTestUnzenTime = 50

// newTaikoEngineTestGenesis returns a Taiko genesis at timestamp 0 with Unzen
// and the Ethereum forks it implies at taikoEngineTestUnzenTime.
func newTaikoEngineTestGenesis() *core.Genesis {
	zero, unzen := uint64(0), uint64(taikoEngineTestUnzenTime)
	config := *params.TaikoChainConfig
	config.ChainID = big.NewInt(167001)
	config.ShanghaiTime = &zero
	config.CancunTime = &unzen
	config.PragueTime = &unzen
	config.OsakaTime = &unzen
	config.UnzenTime = &unzen
	return &core.Genesis{
		Config: &config,
		Alloc: types.GenesisAlloc{
			params.BeaconRootsAddress:                 {Code: params.BeaconRootsCode, Balance: common.Big0},
			core.TaikoTreasuryAddress(config.ChainID): {Code: []byte{0x00}, Balance: common.Big0}, // STOP
		},
		Timestamp:  0,
		Difficulty: common.Big0,
		BaseFee:    big.NewInt(params.InitialBaseFee),
	}
}

// newEngineTestNode starts a node whose engine namespace is mounted by Register,
// the way cmd/geth mounts it.
func newEngineTestNode(t *testing.T, genesis *core.Genesis) (*node.Node, *eth.Ethereum) {
	t.Helper()
	n, err := node.New(&node.Config{P2P: p2p.Config{ListenAddr: "0.0.0.0:0", NoDiscovery: true, MaxPeers: 25}})
	if err != nil {
		t.Fatalf("can't create node: %v", err)
	}
	ethservice, err := eth.New(n, &ethconfig.Config{
		Genesis:        genesis,
		SyncMode:       ethconfig.FullSync,
		TrieTimeout:    time.Minute,
		TrieDirtyCache: 256,
		TrieCleanCache: 256,
		Miner:          miner.DefaultConfig,
	})
	if err != nil {
		n.Close()
		t.Fatalf("can't create eth service: %v", err)
	}
	if err := Register(n, ethservice); err != nil {
		n.Close()
		t.Fatalf("can't register engine API: %v", err)
	}
	if err := n.Start(); err != nil {
		n.Close()
		t.Fatalf("can't start node: %v", err)
	}
	t.Cleanup(func() { n.Close() })
	return n, ethservice
}

// rpcErrorCode returns the JSON-RPC error code carried by err.
func rpcErrorCode(t *testing.T, err error) int {
	t.Helper()
	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("want a JSON-RPC error, have %v", err)
	}
	return rpcErr.ErrorCode()
}

func TestTaikoEngineAPIRegistration(t *testing.T) {
	n, _ := newEngineTestNode(t, newTaikoEngineTestGenesis())
	client := n.Attach()
	defer client.Close()

	var capabilities []string
	if err := client.Call(&capabilities, "engine_exchangeCapabilities", []string{"engine_forkchoiceUpdatedV2"}); err != nil {
		t.Fatalf("engine_exchangeCapabilities: %v", err)
	}
	want := []string{"engine_forkchoiceUpdatedV3", "engine_getPayloadV5", "engine_newPayloadV4"}
	if !slices.Equal(capabilities, want) {
		t.Fatalf("capabilities = %v, want %v", capabilities, want)
	}

	// The upstream methods are not mounted on Taiko chains.
	for _, method := range []string{
		"engine_forkchoiceUpdatedV2",
		"engine_newPayloadV2",
		"engine_getPayloadV2",
		"engine_getPayloadV4",
		"engine_getBlobsV1",
		"engine_getClientVersionV1",
		"engine_exchangeTransitionConfigurationV1",
	} {
		err := client.Call(new(json.RawMessage), method)
		if err == nil {
			t.Fatalf("%s: want -32601, have a result", method)
		}
		if code := rpcErrorCode(t, err); code != -32601 {
			t.Fatalf("%s: error code = %d (%v), want -32601", method, code, err)
		}
	}

	// The testing namespace stays registered next to the Taiko engine service.
	modules, err := client.SupportedModules()
	if err != nil {
		t.Fatalf("rpc_modules: %v", err)
	}
	for _, namespace := range []string{"engine", "testing"} {
		if _, ok := modules[namespace]; !ok {
			t.Fatalf("namespace %q not registered: %v", namespace, modules)
		}
	}
}

// TestTaikoEngineAPIMethodSet pins the exported methods of TaikoEngineAPI: the
// RPC server serves every one of them, so an exported helper would become an
// engine_* method.
func TestTaikoEngineAPIMethodSet(t *testing.T) {
	typ := reflect.TypeOf(&TaikoEngineAPI{})
	have := make([]string, typ.NumMethod())
	for i := range have {
		have[i] = typ.Method(i).Name
	}
	want := []string{"ExchangeCapabilities", "ForkchoiceUpdatedV3", "GetPayloadV5", "NewPayloadV4"}
	if !slices.Equal(have, want) {
		t.Fatalf("exported methods = %v, want %v", have, want)
	}
}

func TestRegisterKeepsConsensusAPIOnNonTaikoChains(t *testing.T) {
	genesis, _ := generateMergeChain(0, true)
	n, _ := newEngineTestNode(t, genesis)
	client := n.Attach()
	defer client.Close()

	var capabilities []string
	if err := client.Call(&capabilities, "engine_exchangeCapabilities", []string{}); err != nil {
		t.Fatalf("engine_exchangeCapabilities: %v", err)
	}
	if !slices.Contains(capabilities, "engine_getPayloadV2") || !slices.Contains(capabilities, "engine_getClientVersionV1") {
		t.Fatalf("upstream capabilities missing: %v", capabilities)
	}
	// engine_getPayloadV2 is served: an unknown ID is -38001, not -32601.
	err := client.Call(new(json.RawMessage), "engine_getPayloadV2", engine.PayloadID{0x02})
	if code := rpcErrorCode(t, err); code != engine.UnknownPayload.ErrorCode() {
		t.Fatalf("engine_getPayloadV2 error code = %d (%v), want -38001", code, err)
	}
}

// taikoV5TestBlock returns a sealed-looking Unzen block with header difficulty
// 7, the block the shared cross-client getPayloadV5 test serves.
func taikoV5TestBlock(timestamp uint64) *types.Block {
	var (
		root            common.Hash
		blobGasUsed     = uint64(0)
		excessBlobGas   = uint64(0)
		requestsHash    = types.EmptyRequestsHash
		withdrawalsHash = types.EmptyWithdrawalsHash
	)
	if timestamp >= 100 {
		root = common.Hash{31: 42}
	}
	header := &types.Header{
		ParentHash:       common.Hash{31: 0x11},
		UncleHash:        types.EmptyUncleHash,
		Coinbase:         common.Address{19: 0x22},
		Root:             common.Hash{31: 0x33},
		TxHash:           types.EmptyTxsHash,
		ReceiptHash:      common.Hash{31: 0x44},
		Difficulty:       big.NewInt(7),
		Number:           big.NewInt(1),
		GasLimit:         30_000_000,
		GasUsed:          0,
		Time:             timestamp,
		Extra:            []byte{},
		MixDigest:        common.Hash{31: 0x55},
		BaseFee:          big.NewInt(1),
		WithdrawalsHash:  &withdrawalsHash,
		BlobGasUsed:      &blobGasUsed,
		ExcessBlobGas:    &excessBlobGas,
		ParentBeaconRoot: &root,
		RequestsHash:     &requestsHash,
	}
	return types.NewBlockWithHeader(header).WithBody(types.Body{Withdrawals: []*types.Withdrawal{}})
}

// putTaikoV5TestJob stores block as the built payload of id, the way the Taiko
// forkchoice update stores a sealed block.
func putTaikoV5TestJob(t *testing.T, api *TaikoEngineAPI, id engine.PayloadID, block *types.Block) {
	t.Helper()
	parent := api.api.eth.BlockChain().CurrentBlock()
	payload, err := api.api.eth.Miner().BuildPayload(context.Background(), &miner.BuildPayloadArgs{
		Parent:      parent.Hash(),
		Timestamp:   100,
		Withdrawals: []*types.Withdrawal{},
		BeaconRoot:  &common.Hash{},
		Version:     engine.PayloadV2,
	}, false)
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	payload.SetFullBlock(block, common.Big0)
	api.api.localBlocks.put(id, payload)
}

func TestTaikoEngineAPIGetPayloadV5(t *testing.T) {
	_, ethservice := newEngineTestNode(t, newTaikoEngineTestGenesis())
	api := NewTaikoEngineAPI(ethservice)
	server := rpc.NewServer()
	defer server.Stop()
	if err := server.RegisterName("engine", api); err != nil {
		t.Fatalf("register engine API: %v", err)
	}
	client := rpc.DialInProc(server)
	defer client.Close()

	// The payload IDs repeat the job timestamp in every byte, so none of them
	// carries the 0x02 version byte the service assigns.
	blocks := make(map[uint64]*types.Block)
	for _, timestamp := range []uint64{49, 99, 100} {
		blocks[timestamp] = taikoV5TestBlock(timestamp)
		id := engine.PayloadID{}
		for i := range id {
			id[i] = byte(timestamp)
		}
		putTaikoV5TestJob(t, api, id, blocks[timestamp])
	}
	call := func(id engine.PayloadID) (json.RawMessage, error) {
		var result json.RawMessage
		err := client.Call(&result, "engine_getPayloadV5", id)
		return result, err
	}

	// A job whose own block is pre-Unzen is -38005.
	if _, err := call(engine.PayloadID{49, 49, 49, 49, 49, 49, 49, 49}); rpcErrorCode(t, err) != engine.UnsupportedFork.ErrorCode() {
		t.Fatalf("pre-Unzen job: error = %v, want -38005", err)
	}

	// Unzen jobs return exactly the Osaka envelope, with blockValue = difficulty.
	for _, timestamp := range []uint64{99, 100} {
		id := engine.PayloadID{}
		for i := range id {
			id[i] = byte(timestamp)
		}
		have, err := call(id)
		if err != nil {
			t.Fatalf("job %d: %v", timestamp, err)
		}
		block := blocks[timestamp]
		hash := func(b byte) string { return common.Hash{31: b}.Hex() }
		want := `{"executionPayload":{` +
			`"parentHash":"` + hash(0x11) + `",` +
			`"feeRecipient":"0x0000000000000000000000000000000000000022",` +
			`"stateRoot":"` + hash(0x33) + `",` +
			`"receiptsRoot":"` + hash(0x44) + `",` +
			`"logsBloom":"0x` + strings.Repeat("0", 512) + `",` +
			`"prevRandao":"` + hash(0x55) + `",` +
			`"blockNumber":"0x1","gasLimit":"0x1c9c380","gasUsed":"0x0",` +
			`"timestamp":"` + (map[uint64]string{99: "0x63", 100: "0x64"})[timestamp] + `",` +
			`"extraData":"0x","baseFeePerGas":"0x1",` +
			`"blockHash":"` + block.Hash().Hex() + `",` +
			`"transactions":[],"withdrawals":[],"blobGasUsed":"0x0","excessBlobGas":"0x0"},` +
			`"blockValue":"0x7",` +
			`"blobsBundle":{"commitments":[],"proofs":[],"blobs":[]},` +
			`"shouldOverrideBuilder":false,"executionRequests":[]}`
		if string(have) != want {
			t.Fatalf("job %d envelope mismatch:\nhave %s\nwant %s", timestamp, have, want)
		}
	}

	// Unknown IDs are -38001 whatever their version byte, including the V3
	// byte the upstream getPayloadV5 requires.
	for _, id := range []engine.PayloadID{{}, {0x03, 1, 2, 3, 4, 5, 6, 7}} {
		_, err := call(id)
		if code := rpcErrorCode(t, err); code != engine.UnknownPayload.ErrorCode() {
			t.Fatalf("unknown id %v: error code = %d (%v), want -38001", id, code, err)
		}
		if err.Error() != "Unknown payload" {
			t.Fatalf("unknown id %v: message = %q, want %q", id, err.Error(), "Unknown payload")
		}
	}
}

func TestTaikoEngineAPIGetPayloadV5RejectsAmsterdamJobs(t *testing.T) {
	genesis := newTaikoEngineTestGenesis()
	amsterdam := uint64(200)
	blobSchedule := *genesis.Config.BlobScheduleConfig
	blobSchedule.Amsterdam = blobSchedule.Osaka
	genesis.Config.BlobScheduleConfig = &blobSchedule
	genesis.Config.AmsterdamTime = &amsterdam
	_, ethservice := newEngineTestNode(t, genesis)
	api := NewTaikoEngineAPI(ethservice)

	id := engine.PayloadID{0x02, 0xc8}
	putTaikoV5TestJob(t, api, id, taikoV5TestBlock(amsterdam))
	if _, err := api.GetPayloadV5(id); !errors.Is(err, engine.UnsupportedFork) {
		t.Fatalf("Amsterdam job: error = %v, want -38005", err)
	}
	id = engine.PayloadID{0x02, 0xc7}
	putTaikoV5TestJob(t, api, id, taikoV5TestBlock(amsterdam-1))
	if _, err := api.GetPayloadV5(id); err != nil {
		t.Fatalf("Osaka job just before Amsterdam: %v", err)
	}
}
