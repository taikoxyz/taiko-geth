package eth

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
)

// The transfer of the shared cross-client vectors: a signed 1-wei dynamic-fee
// transfer on chain 167001, and the JSON object the reference client returns
// for it from the taikoAuth transaction-pool methods.
var (
	taikoTxPoolVectorSender = common.HexToAddress("0x90F79bf6EB2c4f870365E785982E1f101E93b906")
	taikoTxPoolVectorRawTx  = common.FromHex("0x02f86983028c598007847735940082520894000000000000000000000000000000000000beef0180c080a024cf56a39ebf0d0db09c5918bb6e283b5261405f4195bd8c4b873bd1d9f17768a0154623306a83cafd5bc518e6e67feac777b5f285c9d933e27f04ab81ddb0cec8")
	taikoTxPoolVectorTxJSON = `{
		"type": "0x2", "chainId": "0x28c59", "nonce": "0x0", "gas": "0x5208",
		"maxFeePerGas": "0x77359400", "maxPriorityFeePerGas": "0x7",
		"to": "0x000000000000000000000000000000000000beef", "value": "0x1", "accessList": [], "input": "0x",
		"r": "0x24cf56a39ebf0d0db09c5918bb6e283b5261405f4195bd8c4b873bd1d9f17768",
		"s": "0x154623306a83cafd5bc518e6e67feac777b5f285c9d933e27f04ab81ddb0cec8",
		"yParity": "0x0", "v": "0x0",
		"hash": "0x0fad705cc325a58b26bb8fd41001ef37ef1cd89c1f1d43eeac6a6609642c5f68",
		"blockHash": null, "blockNumber": null, "transactionIndex": null,
		"from": "0x90f79bf6eb2c4f870365e785982e1f101e93b906", "gasPrice": "0x77359400"
	}`
)

// taikoTxPoolVectorTx decodes the vector transfer.
func taikoTxPoolVectorTx(t *testing.T) *types.Transaction {
	t.Helper()
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(taikoTxPoolVectorRawTx); err != nil {
		t.Fatalf("decode the vector transfer: %v", err)
	}
	return tx
}

// newTaikoTxPoolTestClient returns an in-process client of the taikoAuth API
// of a chain with chain ID 167001 and Etna at etnaTime (nil: never), whose
// genesis funds the vector sender and the given accounts, with txs in its pool.
func newTaikoTxPoolTestClient(t *testing.T, etnaTime *uint64, funded []common.Address, txs ...*types.Transaction) *rpc.Client {
	t.Helper()
	config := etnaTestChainConfig()
	config.ChainID = big.NewInt(167001)
	config.EtnaTime = etnaTime
	alloc := types.GenesisAlloc{taikoTxPoolVectorSender: {Balance: big.NewInt(params.Ether)}}
	for _, addr := range funded {
		alloc[addr] = types.Account{Balance: big.NewInt(params.Ether)}
	}
	eth := newEtnaTestEthereum(t, &core.Genesis{
		Config:   config,
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(params.ShastaInitialBaseFee),
		Alloc:    alloc,
	}, true)
	for i, err := range eth.txPool.Add(txs, true) {
		if err != nil {
			t.Fatalf("adding transaction %d to the pool: %v", i, err)
		}
	}
	server := rpc.NewServer()
	t.Cleanup(server.Stop)
	if err := server.RegisterName("taikoAuth", &TaikoAuthAPIBackend{eth: eth}); err != nil {
		t.Fatalf("RegisterName: %v", err)
	}
	client := rpc.DialInProc(server)
	t.Cleanup(client.Close)
	return client
}

// taikoTxPoolContentCalls are the two taikoAuth transaction-pool methods with
// the parameters of the shared vectors and the given number of lists.
func taikoTxPoolContentCalls(lists uint64) []struct {
	method string
	args   []any
} {
	beneficiary := common.HexToAddress("0x42")
	baseFee := big.NewInt(params.ShastaInitialBaseFee)
	return []struct {
		method string
		args   []any
	}{
		{"taikoAuth_txPoolContent", []any{beneficiary, baseFee, uint64(30_000_000), uint64(131_072), nil, lists}},
		{"taikoAuth_txPoolContentWithMinTip", []any{beneficiary, baseFee, uint64(30_000_000), uint64(131_072), nil, lists, uint64(0)}},
	}
}

// decodeTaikoJSON decodes raw into a generic JSON value.
func decodeTaikoJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

// TestTxPoolContentResponseShape pins the taikoAuth transaction-pool response
// on every fork, as the reference client returns it: an empty selection is
// one empty list, and a selected transaction is the RPC object of a pending
// transaction with the list's real gas used. The response still decodes into
// []*miner.PreBuiltTxList, the type the Go driver reads it as.
func TestTxPoolContentResponseShape(t *testing.T) {
	zero := uint64(0)
	for _, fork := range []struct {
		name     string
		etnaTime *uint64
	}{
		{"etna", &zero},
		{"before etna", nil},
	} {
		t.Run(fork.name+"/empty pool", func(t *testing.T) {
			client := newTaikoTxPoolTestClient(t, fork.etnaTime, nil)
			for _, call := range taikoTxPoolContentCalls(1) {
				var raw json.RawMessage
				if err := client.Call(&raw, call.method, call.args...); err != nil {
					t.Fatalf("%s: %v", call.method, err)
				}
				want := decodeTaikoJSON(t, []byte(`[{"txList": [], "estimatedGasUsed": 0, "bytesLength": 0}]`))
				if got := decodeTaikoJSON(t, raw); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s = %s, want one empty list", call.method, raw)
				}
				var lists []*miner.PreBuiltTxList
				if err := json.Unmarshal(raw, &lists); err != nil || len(lists) != 1 || len(lists[0].TxList) != 0 {
					t.Fatalf("%s decodes to %v (%v), want one empty list", call.method, lists, err)
				}
			}
		})
		t.Run(fork.name+"/one transfer", func(t *testing.T) {
			tx := taikoTxPoolVectorTx(t)
			client := newTaikoTxPoolTestClient(t, fork.etnaTime, nil, tx)
			for _, call := range taikoTxPoolContentCalls(1) {
				var raw json.RawMessage
				if err := client.Call(&raw, call.method, call.args...); err != nil {
					t.Fatalf("%s: %v", call.method, err)
				}
				var lists []struct {
					TxList           []json.RawMessage `json:"txList"`
					EstimatedGasUsed json.Number       `json:"estimatedGasUsed"`
					BytesLength      json.Number       `json:"bytesLength"`
				}
				if err := json.Unmarshal(raw, &lists); err != nil || len(lists) != 1 || len(lists[0].TxList) != 1 {
					t.Fatalf("%s = %s (%v), want one list with one transaction", call.method, raw, err)
				}
				if got, want := decodeTaikoJSON(t, lists[0].TxList[0]), decodeTaikoJSON(t, []byte(taikoTxPoolVectorTxJSON)); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s transaction = %s, want the reference object %s", call.method, lists[0].TxList[0], taikoTxPoolVectorTxJSON)
				}
				if lists[0].EstimatedGasUsed != "21000" {
					t.Fatalf("%s estimatedGasUsed = %s, want 21000", call.method, lists[0].EstimatedGasUsed)
				}
				if lists[0].BytesLength == "0" {
					t.Fatalf("%s bytesLength = 0, want the compressed list size", call.method)
				}

				var decoded []*miner.PreBuiltTxList
				if err := json.Unmarshal(raw, &decoded); err != nil {
					t.Fatalf("decoding %s into []*miner.PreBuiltTxList: %v", call.method, err)
				}
				if len(decoded) != 1 || len(decoded[0].TxList) != 1 || decoded[0].TxList[0].Hash() != tx.Hash() || decoded[0].EstimatedGasUsed != params.TxGas {
					t.Fatalf("%s decodes to %+v, want the transfer and 21000 gas", call.method, decoded)
				}
			}
		})
	}
}

// TestTxPoolContentDecodesEveryTxType pins that a selected transaction of
// every type the pool admits round-trips through the RPC object into
// []*miner.PreBuiltTxList with its hash, and that the object names its
// sender.
func TestTxPoolContentDecodesEveryTxType(t *testing.T) {
	zero := uint64(0)
	chainID := big.NewInt(167001)
	signer := types.LatestSignerForChainID(chainID)
	var (
		keys  = make([]*ecdsa.PrivateKey, 4)
		addrs = make([]common.Address, 4)
	)
	for i := range keys {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		keys[i], addrs[i] = key, crypto.PubkeyToAddress(key.PublicKey)
	}
	to := common.HexToAddress("0xbeef")
	fee, tip := big.NewInt(2*params.GWei), big.NewInt(params.GWei)
	auth, err := types.SignSetCode(keys[3], types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: to,
		Nonce:   1, // the sender's own nonce after the set-code transaction
	})
	if err != nil {
		t.Fatalf("SignSetCode: %v", err)
	}
	txs := []*types.Transaction{
		types.MustSignNewTx(keys[0], signer, &types.LegacyTx{Nonce: 0, GasPrice: fee, Gas: params.TxGas, To: &to, Value: common.Big1}),
		types.MustSignNewTx(keys[1], signer, &types.AccessListTx{ChainID: chainID, Nonce: 0, GasPrice: fee, Gas: 30_000, To: &to, Value: common.Big1,
			AccessList: types.AccessList{{Address: to, StorageKeys: []common.Hash{{0x01}}}}}),
		types.MustSignNewTx(keys[2], signer, &types.DynamicFeeTx{ChainID: chainID, Nonce: 0, GasTipCap: tip, GasFeeCap: fee, Gas: params.TxGas, To: &to, Value: common.Big1}),
		types.MustSignNewTx(keys[3], signer, &types.SetCodeTx{ChainID: uint256.MustFromBig(chainID), Nonce: 0, GasTipCap: uint256.MustFromBig(tip), GasFeeCap: uint256.MustFromBig(fee),
			Gas: 100_000, To: to, Value: new(uint256.Int), AuthList: []types.SetCodeAuthorization{auth}}),
	}
	client := newTaikoTxPoolTestClient(t, &zero, addrs, txs...)

	var raw json.RawMessage
	if err := client.Call(&raw, "taikoAuth_txPoolContent", common.HexToAddress("0x42"), big.NewInt(params.ShastaInitialBaseFee), uint64(30_000_000), uint64(131_072), nil, uint64(1)); err != nil {
		t.Fatalf("taikoAuth_txPoolContent: %v", err)
	}
	var objects []struct {
		TxList []struct {
			Hash common.Hash    `json:"hash"`
			From common.Address `json:"from"`
		} `json:"txList"`
	}
	if err := json.Unmarshal(raw, &objects); err != nil || len(objects) != 1 {
		t.Fatalf("response %s (%v), want one list", raw, err)
	}
	var decoded []*miner.PreBuiltTxList
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding into []*miner.PreBuiltTxList: %v", err)
	}
	if len(decoded) != 1 || len(decoded[0].TxList) != len(txs) {
		t.Fatalf("decoded %d lists, want one list with %d transactions: %s", len(decoded), len(txs), raw)
	}
	senders := make(map[common.Hash]common.Address)
	for i, tx := range txs {
		senders[tx.Hash()] = addrs[i]
	}
	for i, tx := range decoded[0].TxList {
		sender, ok := senders[tx.Hash()]
		if !ok {
			t.Fatalf("decoded transaction %d has an unknown hash %v", i, tx.Hash())
		}
		if objects[0].TxList[i].Hash != tx.Hash() || objects[0].TxList[i].From != sender {
			t.Fatalf("object %d = (hash %v, from %v), want (%v, %v)", i, objects[0].TxList[i].Hash, objects[0].TxList[i].From, tx.Hash(), sender)
		}
		delete(senders, tx.Hash())
	}
}

// TestTxPoolContentNoListsBeforeEtna pins that a pre-Etna preselection asked
// for no list keeps answering without an error, with no list.
func TestTxPoolContentNoListsBeforeEtna(t *testing.T) {
	client := newTaikoTxPoolTestClient(t, nil, nil, taikoTxPoolVectorTx(t))
	for _, call := range taikoTxPoolContentCalls(0) {
		var raw json.RawMessage
		if err := client.Call(&raw, call.method, call.args...); err != nil {
			t.Fatalf("%s: %v", call.method, err)
		}
		if string(raw) != "[]" {
			t.Fatalf("%s = %s, want []", call.method, raw)
		}
	}
}
