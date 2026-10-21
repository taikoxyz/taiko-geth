package catalyst

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
)

// taikoEtnaVectorFile is one file of the shared cross-client vectors in testdata/etna:
// JSON-RPC requests captured from a live run against the reference client, each with
// the reference response, replayed in order on a fresh internal-devnet node.
type taikoEtnaVectorFile struct {
	Description string                `json:"description"`
	NetworkID   uint64                `json:"networkId"`
	EtnaTime    uint64                `json:"etnaTime"`
	GenesisHash common.Hash           `json:"genesisHash"`
	Steps       []taikoEtnaVectorStep `json:"steps"`
}

type taikoEtnaVectorStep struct {
	Name   string            `json:"name"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
	Mode   string            `json:"mode"`
	Result json.RawMessage   `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// taikoEtnaVectorHeaderKeys are the block-object fields compared by the "header" mode.
var taikoEtnaVectorHeaderKeys = []string{
	"hash", "parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot", "receiptsRoot",
	"logsBloom", "difficulty", "number", "gasLimit", "gasUsed", "timestamp", "extraData", "mixHash",
	"nonce", "baseFeePerGas", "withdrawalsRoot", "blobGasUsed", "excessBlobGas", "parentBeaconBlockRoot",
	"requestsHash", "transactions", "withdrawals",
}

// TestTaikoEtnaVectors replays the Unzen/Etna Engine API, taikoAuth and eth RPC vectors
// and requires the same responses as the reference client (message text excluded).
func TestTaikoEtnaVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "etna", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no vector files in testdata/etna")
	}
	for _, path := range files {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			replayTaikoEtnaVectors(t, path)
		})
	}
}

func replayTaikoEtnaVectors(t *testing.T, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file taikoEtnaVectorFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(file.Steps) == 0 {
		t.Fatalf("%s has no steps", path)
	}
	client, backend := newTaikoEtnaVectorNode(t, file)
	for i, step := range file.Steps {
		args := make([]any, len(step.Params))
		for j, p := range step.Params {
			args[j] = p
		}
		var result json.RawMessage
		gotCode := 0
		if err := client.Call(&result, step.Method, args...); err != nil {
			var rpcErr rpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("step %d %q (%s): %v", i, step.Name, step.Method, err)
			}
			gotCode = rpcErr.ErrorCode()
		}
		wantCode := 0
		if step.Error != nil {
			wantCode = step.Error.Code
		}
		if gotCode != 0 || wantCode != 0 {
			if gotCode != wantCode {
				t.Fatalf("step %d %q (%s): error code %d, want %d (result %s)", i, step.Name, step.Method, gotCode, wantCode, result)
			}
		} else {
			got := projectTaikoEtnaVector(t, step.Mode, result)
			want := projectTaikoEtnaVector(t, step.Mode, step.Result)
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(want)
				t.Fatalf("step %d %q (%s, mode %s):\n got: %s\nwant: %s", i, step.Name, step.Method, step.Mode, gotJSON, wantJSON)
			}
		}
		if step.Method == "eth_sendRawTransaction" {
			if err := backend.TxPool().Sync(); err != nil {
				t.Fatalf("step %d %q: sync txpool: %v", i, step.Name, err)
			}
		}
	}
}

// newTaikoEtnaVectorNode starts an in-memory internal-devnet node with the file's Etna
// time and the engine, taiko and taikoAuth APIs, and returns an in-process client.
func newTaikoEtnaVectorNode(t *testing.T, file taikoEtnaVectorFile) (*rpc.Client, *eth.Ethereum) {
	t.Helper()
	genesis := core.TaikoGenesisBlock(file.NetworkID)
	etnaTime := file.EtnaTime
	genesis.Config.EtnaTime = &etnaTime
	if hash := genesis.ToBlock().Hash(); hash != file.GenesisHash {
		t.Fatalf("genesis hash %s, vectors were captured on %s", hash, file.GenesisHash)
	}
	stack, err := node.New(&node.Config{P2P: p2p.Config{ListenAddr: "127.0.0.1:0", NoDiscovery: true, MaxPeers: 0}})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	t.Cleanup(func() { stack.Close() })
	config := ethconfig.Defaults
	config.Genesis = genesis
	config.NetworkId = file.NetworkID
	config.SyncMode = ethconfig.FullSync
	config.Miner = miner.DefaultConfig
	backend, err := eth.New(stack, &config)
	if err != nil {
		t.Fatalf("create eth service: %v", err)
	}
	if err := Register(stack, backend); err != nil {
		t.Fatalf("register engine API: %v", err)
	}
	stack.RegisterAPIs([]rpc.API{
		{Namespace: "taiko", Service: eth.NewTaikoAPIBackend(backend)},
		{Namespace: "taikoAuth", Service: eth.NewTaikoAuthAPIBackend(backend), Authenticated: true},
	})
	if err := stack.Start(); err != nil {
		t.Fatalf("start node: %v", err)
	}
	backend.SetSynced()
	return stack.Attach(), backend
}

// projectTaikoEtnaVector reduces a JSON-RPC result to the part a comparison mode checks.
func projectTaikoEtnaVector(t *testing.T, mode string, raw json.RawMessage) any {
	t.Helper()
	var v any
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	obj := func(x any) map[string]any { m, _ := x.(map[string]any); return m }
	list := func(x any) []any { l, _ := x.([]any); return l }
	switch mode {
	case "exact":
		return v
	case "forkchoice":
		ps := obj(obj(v)["payloadStatus"])
		return map[string]any{"status": ps["status"], "latestValidHash": ps["latestValidHash"], "payloadId": obj(v)["payloadId"]}
	case "payloadStatus":
		return map[string]any{"status": obj(v)["status"], "latestValidHash": obj(v)["latestValidHash"]}
	case "sortedStrings":
		var out []string
		for _, s := range list(v) {
			out = append(out, fmt.Sprint(s))
		}
		sort.Strings(out)
		return out
	case "lowerKeys":
		return lowerTaikoEtnaVectorKeys(v)
	case "header":
		if v == nil {
			return nil
		}
		out := make(map[string]any, len(taikoEtnaVectorHeaderKeys))
		for _, k := range taikoEtnaVectorHeaderKeys {
			out[k] = obj(v)[k]
		}
		return out
	case "txLists":
		// Every list in order, with each transaction object field by field
		// and the list's gas used. bytesLength is each client's own DA-size
		// measure, so only whether it is zero counts.
		if v == nil {
			return nil
		}
		out := []any{}
		for _, l := range list(v) {
			out = append(out, map[string]any{
				"txList":           obj(l)["txList"],
				"estimatedGasUsed": obj(l)["estimatedGasUsed"],
				"bytesLength":      fmt.Sprint(obj(l)["bytesLength"]) != "0",
			})
		}
		return out
	case "simulate":
		var out [][]map[string]any
		for _, b := range list(v) {
			var calls []map[string]any
			for _, c := range list(obj(b)["calls"]) {
				calls = append(calls, map[string]any{"status": obj(c)["status"], "returnData": obj(c)["returnData"], "gasUsed": obj(c)["gasUsed"]})
			}
			out = append(out, calls)
		}
		return out
	default:
		t.Fatalf("unknown comparison mode %q", mode)
		return nil
	}
}

func lowerTaikoEtnaVectorKeys(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[strings.ToLower(k)] = lowerTaikoEtnaVectorKeys(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = lowerTaikoEtnaVectorKeys(val)
		}
		return out
	default:
		return v
	}
}
