package eth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

var (
	etnaReplayBeneficiary = common.HexToAddress("0x00000000000000000000000000000000000000bb")
	etnaReplayRoot        = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000e7")
	// etnaReplayHasher hashes 64 KiB per loop iteration until the Unzen zk gas
	// budget runs out: JUMPDEST; PUSH3 0x010000; PUSH1 0; KECCAK256; POP;
	// PUSH1 0; JUMP.
	etnaReplayHasher = common.HexToAddress("0x0000000000000000000000000000000000000022")
)

// etnaReplayGenesis returns an Etna-from-genesis genesis with funded test and
// golden-touch accounts, a funded treasury, the zk gas hasher, and the EIP-4788
// and EIP-2935 system contracts.
func etnaReplayGenesis(config *params.ChainConfig) *core.Genesis {
	alloc := types.GenesisAlloc{
		testAddr:                     {Balance: big.NewInt(params.Ether)},
		taiko.GoldenTouchAccount:     {Balance: big.NewInt(params.Ether)},
		etnaReplayHasher:             {Code: common.FromHex("0x5b6201000060002050600056")},
		params.BeaconRootsAddress:    {Nonce: 1, Code: params.BeaconRootsCode},
		params.HistoryStorageAddress: {Nonce: 1, Code: params.HistoryStorageCode},
	}
	alloc[core.TaikoTreasuryAddress(config.ChainID)] = types.Account{Balance: common.Big1}
	return &core.Genesis{
		Config:    config,
		Timestamp: 1000,
		GasLimit:  30_000_000,
		BaseFee:   big.NewInt(params.ShastaInitialBaseFee),
		Alloc:     alloc,
	}
}

// newEtnaReplayChain seals and imports block 1 of an Etna-from-genesis chain.
// Its first transaction is a golden-touch call to the treasury paying a 7 wei
// tip, its second a transfer from testAddr; its extraData shares 25% of the
// base fee with the beneficiary.
func newEtnaReplayChain(t *testing.T) (*Ethereum, *types.Block) {
	t.Helper()
	config := etnaTestChainConfig()
	eth := newEtnaTestEthereum(t, etnaReplayGenesis(config), false)
	parent := eth.blockchain.CurrentBlock()

	signer := types.LatestSigner(config)
	treasury := core.TaikoTreasuryAddress(config.ChainID)
	baseFee := big.NewInt(params.ShastaInitialBaseFee)
	golden := types.MustSignNewTx(goldenTouchTestKey(t), signer, &types.DynamicFeeTx{
		ChainID: config.ChainID, Nonce: 0, GasTipCap: big.NewInt(7), GasFeeCap: big.NewInt(2 * params.ShastaInitialBaseFee),
		Gas: 1_000_000, To: &treasury,
	})
	transfer := types.MustSignNewTx(testKey, signer, &types.DynamicFeeTx{
		ChainID: config.ChainID, Nonce: 0, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2 * params.ShastaInitialBaseFee),
		Gas: params.TxGas, To: &etnaReplayBeneficiary, Value: common.Big1,
	})
	txList, err := rlp.EncodeToBytes(types.Transactions{golden, transfer})
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	root := etnaReplayRoot
	timestamp := parent.Time + 1
	block, err := eth.miner.SealBlockWith(parent, 0, &engine.PayloadAttributes{
		Timestamp:             timestamp,
		Random:                common.HexToHash("0x0a"),
		SuggestedFeeRecipient: etnaReplayBeneficiary,
		Withdrawals:           []*types.Withdrawal{},
		BeaconRoot:            &root,
		BaseFeePerGas:         baseFee,
		BlockMetadata: &engine.BlockMetadata{
			Beneficiary: etnaReplayBeneficiary,
			GasLimit:    30_000_000,
			Timestamp:   timestamp,
			TxList:      txList,
			ExtraData:   []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1},
		},
	})
	if err != nil {
		t.Fatalf("SealBlockWith: %v", err)
	}
	if len(block.Transactions()) != 2 {
		t.Fatalf("sealed %d transactions, want 2", len(block.Transactions()))
	}
	if _, err := eth.blockchain.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("InsertChain: %v", err)
	}
	return eth, block
}

// TestStateAtTransactionEtnaFirstTransactionIsOrdinary pins that replaying an
// Etna block charges its first transaction, a golden-touch call to the
// treasury, ordinary fees, and that the replayed state reproduces the block's
// state root.
func TestStateAtTransactionEtnaFirstTransactionIsOrdinary(t *testing.T) {
	eth, block := newEtnaReplayChain(t)
	config := eth.blockchain.Config()
	receipts := eth.blockchain.GetReceiptsByHash(block.Hash())

	tx, blockCtx, statedb, release, err := eth.stateAtTransaction(context.Background(), block, 1, 0)
	if err != nil {
		t.Fatalf("stateAtTransaction: %v", err)
	}
	defer release()

	// The golden touch paid 21,000 gas at the base fee plus its 7 wei tip.
	fee := new(big.Int).Mul(new(big.Int).SetUint64(receipts[0].GasUsed), big.NewInt(params.ShastaInitialBaseFee+7))
	if want := new(big.Int).Sub(big.NewInt(params.Ether), fee); statedb.GetBalance(taiko.GoldenTouchAccount).ToBig().Cmp(want) != 0 {
		t.Fatalf("golden touch balance = %v, want %v", statedb.GetBalance(taiko.GoldenTouchAccount), want)
	}

	// Executing the last transaction on the replayed state reproduces the root.
	msg, err := core.TransactionToMessage(tx, types.MakeSigner(config, block.Number(), block.Time()), block.BaseFee())
	if err != nil {
		t.Fatalf("TransactionToMessage: %v", err)
	}
	msg.BasefeeSharingPctg = core.DecodeShastaBasefeeSharingPctg(block.Extra())
	statedb.SetTxContext(tx.Hash(), 1)
	if _, err := core.ApplyMessage(vm.NewEVM(blockCtx, statedb, config, vm.Config{}), msg, core.NewGasPool(block.GasLimit())); err != nil {
		t.Fatalf("ApplyMessage: %v", err)
	}
	if root := statedb.IntermediateRoot(config.IsEIP158(block.Number())); root != block.Root() {
		t.Fatalf("replayed state root = %v, want %v", root, block.Root())
	}
}

// TestBuildTxListWitnessEtnaCanonicalReplay pins that the witness of an Etna
// block's own transactions, the first a golden-touch call to the treasury,
// matches the header zk gas and carries every state node the block's
// re-execution reads.
func TestBuildTxListWitnessEtnaCanonicalReplay(t *testing.T) {
	eth, block := newEtnaReplayChain(t)
	witness, committed, err := buildTxListWitness(eth.blockchain, block, block.Transactions(), txListWitnessOptions{})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("committed %d transactions, want 2", len(committed))
	}
	// Re-execute the block on a state backed by the witness alone. The chain
	// context only serves the Taiko engine: the stateless executor's default
	// engine pays proof-of-work rewards for the non-zero zk gas difficulty.
	memdb := witness.MakeHashDB()
	statedb, err := state.New(witness.Root(), state.NewDatabase(triedb.NewDatabase(memdb, triedb.HashDefaults), state.NewCodeDB(memdb)))
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	header := block.Header()
	header.Root, header.ReceiptHash = common.Hash{}, common.Hash{}
	replay := types.NewBlockWithHeader(header).WithBody(*block.Body())
	if _, err := core.NewStateProcessor(eth.blockchain).Process(context.Background(), replay, statedb, vm.Config{}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if root := statedb.IntermediateRoot(true); statedb.Error() != nil || root != block.Root() {
		t.Fatalf("witness replay root = %v (state error %v), want %v", root, statedb.Error(), block.Root())
	}
}

// TestBuildTxListWitnessEtnaSkipsAnchorPreReads pins that an Etna replay
// reads neither the golden touch nor the treasury before its transactions.
func TestBuildTxListWitnessEtnaSkipsAnchorPreReads(t *testing.T) {
	eth, block := newEtnaReplayChain(t)
	witness, _, err := buildTxListWitness(eth.blockchain, block, nil, txListWitnessOptions{SkipZkGasDifficultyCheck: true})
	if err != nil {
		t.Fatalf("buildTxListWitness: %v", err)
	}
	for _, addr := range []common.Address{taiko.GoldenTouchAccount, core.TaikoTreasuryAddress(eth.blockchain.Config().ChainID)} {
		if _, ok := witness.Keys[string(addr.Bytes())]; ok {
			t.Fatalf("witness carries %v, which an empty Etna replay never reads", addr)
		}
	}
}

// TestBuildTxListWitnessEtnaZkGasDifficultyMismatch pins that a replay whose
// zk gas differs from the header difficulty fails with the import error
// core.ErrZkGasDifficultyMismatch.
func TestBuildTxListWitnessEtnaZkGasDifficultyMismatch(t *testing.T) {
	eth, block := newEtnaReplayChain(t)
	if _, _, err := buildTxListWitness(eth.blockchain, block, nil, txListWitnessOptions{}); !errors.Is(err, core.ErrZkGasDifficultyMismatch) {
		t.Fatalf("buildTxListWitness error = %v, want %v", err, core.ErrZkGasDifficultyMismatch)
	}
}

// TestBuildTxListWitnessEtnaFirstPosition pins that the first position of an
// Etna tx list is ordinary: blob transactions, invalid senders and recoverable
// errors are skipped, a golden-touch call to the treasury gets no exemption,
// and zk gas exhaustion truncates the list.
func TestBuildTxListWitnessEtnaFirstPosition(t *testing.T) {
	eth, block := newEtnaReplayChain(t)
	config := eth.blockchain.Config()
	signer := types.LatestSigner(config)
	treasury := core.TaikoTreasuryAddress(config.ChainID)
	feeCap := big.NewInt(2 * params.ShastaInitialBaseFee)
	transfer := func(nonce uint64) *types.Transaction {
		return types.MustSignNewTx(testKey, signer, &types.DynamicFeeTx{
			ChainID: config.ChainID, Nonce: nonce, GasTipCap: common.Big1, GasFeeCap: feeCap,
			Gas: params.TxGas, To: &etnaReplayBeneficiary, Value: common.Big1,
		})
	}
	valid := transfer(0)

	for _, tt := range []struct {
		name      string
		first     *types.Transaction
		committed int
	}{
		{"blob transaction", types.MustSignNewTx(testKey, signer, &types.BlobTx{
			ChainID: uint256.MustFromBig(config.ChainID), Nonce: 0, GasTipCap: uint256.NewInt(1), GasFeeCap: uint256.MustFromBig(feeCap),
			Gas: params.TxGas, To: etnaReplayBeneficiary, Value: uint256.NewInt(0), BlobFeeCap: uint256.NewInt(1), BlobHashes: []common.Hash{{0x01}},
		}), 1},
		{"sender of another chain", types.MustSignNewTx(testKey, types.LatestSignerForChainID(big.NewInt(1)), &types.DynamicFeeTx{
			ChainID: big.NewInt(1), Nonce: 0, GasTipCap: common.Big1, GasFeeCap: feeCap, Gas: params.TxGas, To: &etnaReplayBeneficiary,
		}), 1},
		{"nonce too high", transfer(99), 1},
		{"unfunded golden-touch call to the treasury", types.MustSignNewTx(goldenTouchTestKey(t), signer, &types.DynamicFeeTx{
			ChainID: config.ChainID, Nonce: 0, GasTipCap: common.Big0, GasFeeCap: big.NewInt(2_000_000_000_000), Gas: 1_000_000, To: &treasury,
		}), 1},
		{"zk gas exhaustion", types.MustSignNewTx(testKey, signer, &types.DynamicFeeTx{
			ChainID: config.ChainID, Nonce: 0, GasTipCap: common.Big1, GasFeeCap: feeCap, Gas: 5_000_000, To: &etnaReplayHasher,
		}), 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			txList, err := rlp.EncodeToBytes(types.Transactions{tt.first, valid})
			if err != nil {
				t.Fatalf("encode tx list: %v", err)
			}
			txs, err := decodeEtnaTxListWitnessTxs(txList)
			if err != nil || len(txs) != 2 {
				t.Fatalf("decoded %d transactions (%v), want both", len(txs), err)
			}
			_, committed, err := buildTxListWitness(eth.blockchain, block, txs, txListWitnessOptions{SkipZkGasDifficultyCheck: true})
			if err != nil {
				t.Fatalf("buildTxListWitness: %v", err)
			}
			if len(committed) != tt.committed {
				t.Fatalf("committed %d transactions, want %d", len(committed), tt.committed)
			}
			if tt.committed == 1 && committed[0].Hash() != valid.Hash() {
				t.Fatalf("committed %v, want the valid transfer", committed[0].Hash())
			}
		})
	}
}

// etnaWitnessCorpusEntry is one input of the shared cross-client tx-list
// vectors (miner/testdata/etna_txlist_corpus.json): whether the list decodes
// and, for every decoded transaction, its hash and the sender recovered
// without a chain-ID check (null when recovery fails).
type etnaWitnessCorpusEntry struct {
	Name  string        `json:"name"`
	Input hexutil.Bytes `json:"input"`
	OK    bool          `json:"ok"`
	Txs   []struct {
		Hash   common.Hash     `json:"hash"`
		Sender *common.Address `json:"sender"`
	} `json:"txs"`
}

// etnaWitnessCorpus reads the shared cross-client tx-list vectors.
func etnaWitnessCorpus(t *testing.T) []etnaWitnessCorpusEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "miner", "testdata", "etna_txlist_corpus.json"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var corpus []etnaWitnessCorpusEntry
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	if len(corpus) < 40 {
		t.Fatalf("corpus has %d entries, want at least 40", len(corpus))
	}
	return corpus
}

// sortedExecutionWitness returns a copy of w with its state, codes and keys
// sorted, so that two witnesses compare as sets.
func sortedExecutionWitness(w *stateless.ExecutionWitness) *stateless.ExecutionWitness {
	sorted := &stateless.ExecutionWitness{
		State:   slices.Clone(w.State),
		Codes:   slices.Clone(w.Codes),
		Keys:    slices.Clone(w.Keys),
		Headers: slices.Clone(w.Headers),
	}
	for _, list := range [][]hexutil.Bytes{sorted.State, sorted.Codes, sorted.Keys} {
		slices.SortFunc(list, func(a, b hexutil.Bytes) int { return bytes.Compare(a, b) })
	}
	return sorted
}

// witnessErrorCode returns the JSON-RPC error code of err, or 0 if it has none.
func witnessErrorCode(err error) int {
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.ErrorCode()
	}
	return 0
}

// TestExecutionWitnessForTxListEtnaSharedGrammar pins that the tx list of an
// Etna block is decoded with the grammar the Etna sealer uses: the block's
// own transactions, framed in shapes only that grammar accepts, produce the
// witness of the canonical encoding, zk gas difficulty check included.
func TestExecutionWitnessForTxListEtnaSharedGrammar(t *testing.T) {
	eth, block := newEtnaReplayChain(t)
	bn := rpc.BlockNumberOrHashWithHash(block.Hash(), false)
	canonical, err := rlp.EncodeToBytes(block.Transactions())
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	want, err := executionWitnessForTxList(eth.blockchain, bn, canonical, nil, nil)
	if err != nil {
		t.Fatalf("canonical tx list: %v", err)
	}
	// Bare typed items: each transaction's type byte and payload list, without
	// the RLP string header of the canonical encoding.
	bare := rlp.NewEncoderBuffer(nil)
	list := bare.List()
	for _, tx := range block.Transactions() {
		enc, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf("encode transaction: %v", err)
		}
		bare.Write(enc)
	}
	bare.ListEnd(list)

	for _, tt := range []struct {
		name   string
		txList []byte
	}{
		{"trailing bytes after the list", append(slices.Clone(canonical), 0xc0, 0x00)},
		{"bare typed items", bare.ToBytes()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := executionWitnessForTxList(eth.blockchain, bn, tt.txList, nil, nil)
			if err != nil {
				t.Fatalf("executionWitnessForTxList: %v", err)
			}
			if !reflect.DeepEqual(sortedExecutionWitness(got), sortedExecutionWitness(want)) {
				t.Fatal("witness differs from the witness of the canonical encoding")
			}
		})
	}
}

// TestDecodeEtnaTxListWitnessTxsCorpus pins the Etna witness decoding against
// the shared cross-client tx-list vectors: a list outside the grammar is a
// -32603 error, and a decodable list keeps exactly the transactions whose
// signer the vectors recover, in list order.
func TestDecodeEtnaTxListWitnessTxsCorpus(t *testing.T) {
	for _, tc := range etnaWitnessCorpus(t) {
		t.Run(tc.Name, func(t *testing.T) {
			txs, err := decodeEtnaTxListWitnessTxs(tc.Input)
			if !tc.OK {
				if code := witnessErrorCode(err); err == nil || code != -32603 {
					t.Fatalf("decoded %d transactions (error %v, code %d), want a -32603 error", len(txs), err, code)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			var want []common.Hash
			for _, tx := range tc.Txs {
				if tx.Sender != nil {
					want = append(want, tx.Hash)
				}
			}
			got := make([]common.Hash, len(txs))
			for i, tx := range txs {
				got[i] = tx.Hash()
			}
			if !slices.Equal(got, want) {
				t.Fatalf("kept %v, want %v", got, want)
			}
		})
	}
}

// witnessSizeErrorCode calls debug_executionWitnessForTxList over JSON-RPC
// for block with txList and returns the code of the error it must answer.
func witnessSizeErrorCode(t *testing.T, eth *Ethereum, block common.Hash, txList []byte) int {
	t.Helper()
	server := rpc.NewServer()
	t.Cleanup(server.Stop)
	if err := server.RegisterName("debug", NewDebugAPI(eth)); err != nil {
		t.Fatalf("register debug API: %v", err)
	}
	client := rpc.DialInProc(server)
	defer client.Close()
	err := client.Call(new(json.RawMessage), "debug_executionWitnessForTxList", block.Hex(), hexutil.Bytes(txList))
	if err == nil {
		t.Fatal("produced a witness, want a size-limit error")
	}
	return witnessErrorCode(err)
}

// TestExecutionWitnessForTxListSizeLimitErrorCodes pins the JSON-RPC code of
// the tx-list size-limit errors: -32603 for an Etna block, like the
// reference client, and the default -32000 before Etna.
func TestExecutionWitnessForTxListSizeLimitErrorCodes(t *testing.T) {
	etna, etnaBlock := newEtnaReplayChain(t)

	// A chain whose genesis is an Unzen block, with Etna scheduled later.
	config := etnaTestChainConfig()
	later := uint64(5000)
	config.EtnaTime = &later
	preEtna := newEtnaTestEthereum(t, etnaReplayGenesis(config), false)
	preEtnaBlock := preEtna.blockchain.Genesis()
	if config.IsEtna(preEtnaBlock.Time()) || !config.IsUnzen(preEtnaBlock.Time()) {
		t.Fatalf("block at %d is not a pre-Etna Unzen block", preEtnaBlock.Time())
	}

	// Random bytes do not compress: below the raw limit, they still exceed
	// one blob of data once compressed.
	incompressible := make([]byte, 2*maxTxListCompressedBytes)
	if _, err := rand.Read(incompressible); err != nil {
		t.Fatalf("random tx list: %v", err)
	}
	for _, tt := range []struct {
		name   string
		txList []byte
		want   string
	}{
		{"raw limit", make([]byte, maxTxListRawBytes+1), "exceeds raw limit"},
		{"compressed limit", incompressible, "exceeds block data limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkTxListSize(tt.txList); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("size check: %v, want an error containing %q", err, tt.want)
			}
			if code := witnessSizeErrorCode(t, etna, etnaBlock.Hash(), tt.txList); code != -32603 {
				t.Fatalf("Etna block: code %d, want -32603", code)
			}
			if code := witnessSizeErrorCode(t, preEtna, preEtnaBlock.Hash(), tt.txList); code != -32000 {
				t.Fatalf("pre-Etna block: code %d, want -32000", code)
			}
		})
	}
}

// TestExecutionWitnessForTxListEtnaCorpus replays every list of the shared
// cross-client tx-list vectors on an Etna block: a list the grammar accepts
// produces a witness, and any other list is a -32603 error, the code of the
// reference client.
func TestExecutionWitnessForTxListEtnaCorpus(t *testing.T) {
	eth, block := newEtnaReplayChain(t)
	bn := rpc.BlockNumberOrHashWithHash(block.Hash(), false)
	opts := &txListWitnessOptions{SkipZkGasDifficultyCheck: true}
	for _, tc := range etnaWitnessCorpus(t) {
		t.Run(tc.Name, func(t *testing.T) {
			witness, err := executionWitnessForTxList(eth.blockchain, bn, tc.Input, nil, opts)
			if tc.OK {
				if err != nil || witness == nil {
					t.Fatalf("executionWitnessForTxList: %v, want a witness", err)
				}
				return
			}
			if err == nil {
				t.Fatal("produced a witness, want the list rejected")
			}
			if code := witnessErrorCode(err); code != -32603 {
				t.Fatalf("error %v has code %d, want -32603", err, code)
			}
		})
	}
}
