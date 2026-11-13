package miner

// Runner for the shared Unzen zk gas block vectors in taiko-mono's testdata/zk_gas_vectors, found
// through ZK_GAS_VECTORS_DIR. alethia-reth generates every expected value; this runner only reads
// the vectors and asserts them against taiko-geth's sealing and import paths.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

// Import verdicts, as they appear in a vector's expected values.
const (
	zkBlockAccept = "accept" // the block imports
	zkBlockReject = "reject" // the block fails import for a zk gas reason
)

// zkBlockNotZkRejection prefixes the verdict of an import that failed for a reason other than zk
// gas. It matches no expected value: such a failure is a runner bug, not a rejection.
const zkBlockNotZkRejection = "rejected for a reason other than zk gas: "

// zkVectorSchemaVersion is the only vector schema version this runner reads.
const zkVectorSchemaVersion = 1

// zkBlockChainID is the chain of every block vector: Taiko devnet, with Shasta and Unzen active
// from genesis.
const zkBlockChainID = 167001

// zkBlockVector is one block-layer vector file.
type zkBlockVector struct {
	SchemaVersion int              `json:"schema_version"`
	ID            string           `json:"id"`
	Layer         string           `json:"layer"`
	Cells         []string         `json:"cells"`
	Description   string           `json:"description"`
	Input         zkBlockInput     `json:"input"`
	Expected      *zkBlockExpected `json:"expected"`
}

// zkBlockInput is block 1 on top of genesis: its environment, the whole pre-state and the txs to
// build it from, in order. Txs[0] is the anchor.
type zkBlockInput struct {
	Env   zkBlockEnv                         `json:"env"`
	Alloc map[common.Address]zkVectorAccount `json:"alloc"`
	Txs   []zkBlockTx                        `json:"txs"`
}

// zkBlockEnv is the environment of block 1.
type zkBlockEnv struct {
	ChainID    hexutil.Uint64 `json:"chain_id"`
	Number     hexutil.Uint64 `json:"number"`
	Timestamp  hexutil.Uint64 `json:"timestamp"`
	BaseFee    hexutil.Uint64 `json:"base_fee"`
	Coinbase   common.Address `json:"coinbase"`
	GasLimit   hexutil.Uint64 `json:"gas_limit"`
	PrevRandao common.Hash    `json:"prev_randao"`
	ExtraData  hexutil.Bytes  `json:"extra_data"`
}

// zkVectorAccount is the pre-state of one alloc account. Only Balance is required; a missing
// nonce is 0 and missing code or storage is empty.
type zkVectorAccount struct {
	Balance hexutil.U256                  `json:"balance"`
	Nonce   hexutil.Uint64                `json:"nonce"`
	Code    hexutil.Bytes                 `json:"code"`
	Storage map[hexutil.U256]hexutil.U256 `json:"storage"`
}

// zkBlockTx is one EIP-1559 tx without an access list; a nil To is a contract creation.
type zkBlockTx struct {
	Signer         zkTxSigner      `json:"signer"`
	Nonce          hexutil.Uint64  `json:"nonce"`
	To             *common.Address `json:"to"`
	Gas            hexutil.Uint64  `json:"gas"`
	MaxFee         hexutil.U256    `json:"max_fee"`
	MaxPriorityFee hexutil.U256    `json:"max_priority_fee"`
	Value          hexutil.U256    `json:"value"`
	Data           hexutil.Bytes   `json:"data"`
}

// zkTxSigner is who signs a vector tx: the golden touch account, or the account whose private key
// is the signer number as a 32-byte big-endian integer.
type zkTxSigner uint32

// zkGoldenTouchSigner is the golden touch account, which signs the anchor. Signer numbers start
// at 1, so 0 is free to stand for it.
const zkGoldenTouchSigner zkTxSigner = 0

// UnmarshalJSON accepts "golden_touch" or a JSON integer in 1..2^32-1.
func (s *zkTxSigner) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		if name != "golden_touch" {
			return fmt.Errorf("unknown signer %q", name)
		}
		*s = zkGoldenTouchSigner
		return nil
	}
	key, err := strconv.ParseUint(string(data), 10, 32)
	if err != nil || key == 0 {
		return fmt.Errorf("signer %s is neither \"golden_touch\" nor an integer in 1..2^32-1", data)
	}
	*s = zkTxSigner(key)
	return nil
}

// numberedKey returns the private key of a numbered signer.
func (s zkTxSigner) numberedKey() (*ecdsa.PrivateKey, error) {
	return crypto.ToECDSA(common.BigToHash(new(big.Int).SetUint64(uint64(s))).Bytes())
}

// address returns the signer's account.
func (s zkTxSigner) address() (common.Address, error) {
	if s == zkGoldenTouchSigner {
		return taiko.GoldenTouchAccount, nil
	}
	key, err := s.numberedKey()
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(key.PublicKey), nil
}

// unsigned returns tx as an unsigned EIP-1559 tx on chainID.
func (tx zkBlockTx) unsigned(chainID *big.Int) *types.DynamicFeeTx {
	return &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     uint64(tx.Nonce),
		GasTipCap: (*uint256.Int)(&tx.MaxPriorityFee).ToBig(),
		GasFeeCap: (*uint256.Int)(&tx.MaxFee).ToBig(),
		Gas:       uint64(tx.Gas),
		To:        tx.To,
		Value:     (*uint256.Int)(&tx.Value).ToBig(),
		Data:      tx.Data,
	}
}

// zkBlockExpected is the outcome of building and importing a block vector, in vector-file field
// order. In the result of runZkBlockVector, RawTxs is the re-encoding of the vector's signed txs
// and TruncatedAt is copied from the vector: geth does not report where it truncated, and the
// sentinel rule makes Included carry that instead.
type zkBlockExpected struct {
	RawTxs                []hexutil.Bytes  `json:"raw_txs,omitempty"`
	Included              []int            `json:"included"`
	TruncatedAt           *int             `json:"truncated_at"`
	CumulativeZkGas       []hexutil.Uint64 `json:"cumulative_zk_gas"`
	BlockZkGas            hexutil.Uint64   `json:"block_zk_gas"`
	StateRoot             common.Hash      `json:"state_root"`
	ImportBuilt           string           `json:"import_built"`
	ImportExtended        *string          `json:"import_extended"`
	ImportWrongDifficulty string           `json:"import_wrong_difficulty"`
}

// String renders e the way it appears in a vector file, without the signed txs.
func (e zkBlockExpected) String() string {
	e.RawTxs = nil
	out, err := json.Marshal(e)
	if err != nil {
		return fmt.Sprintf("unrenderable result: %v", err)
	}
	return string(out)
}

// decodeZkVectorJSON decodes one JSON value into v, rejecting unknown fields and trailing data.
func decodeZkVectorJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

// loadZkGasVectors decodes every vector of layer from <dir>/cases/*.json, in file name order. It
// fails when a file cannot be read or decoded, and when no vector of layer exists.
func loadZkGasVectors[T any](dir, layer string) ([]T, error) {
	cases := filepath.Join(dir, "cases")
	entries, err := os.ReadDir(cases)
	if err != nil {
		return nil, err
	}
	var vectors []T
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		path := filepath.Join(cases, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var head struct {
			SchemaVersion int    `json:"schema_version"`
			ID            string `json:"id"`
			Layer         string `json:"layer"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		switch {
		case head.SchemaVersion != zkVectorSchemaVersion:
			return nil, fmt.Errorf("%s: schema_version %d, want %d", path, head.SchemaVersion, zkVectorSchemaVersion)
		case head.Layer == "":
			return nil, fmt.Errorf("%s: missing layer", path)
		case head.ID != strings.TrimSuffix(name, ".json"):
			return nil, fmt.Errorf("%s: id %q does not match the file name", path, head.ID)
		case head.Layer != layer:
			continue
		}
		var vector T
		if err := decodeZkVectorJSON(data, &vector); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		vectors = append(vectors, vector)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("no %s vectors in %s", layer, cases)
	}
	return vectors, nil
}

// zkGasCorpusDir returns ZK_GAS_VECTORS_DIR, read through lookup. An empty dir with a nil error
// means the corpus test skips; with ZK_GAS_VECTORS_REQUIRED=1 an unset directory is an error
// instead, so that a run that must check the corpus cannot pass without it.
func zkGasCorpusDir(lookup func(string) (string, bool)) (string, error) {
	if dir, _ := lookup("ZK_GAS_VECTORS_DIR"); dir != "" {
		return dir, nil
	}
	if required, _ := lookup("ZK_GAS_VECTORS_REQUIRED"); required == "1" {
		return "", errors.New("ZK_GAS_VECTORS_REQUIRED=1 but ZK_GAS_VECTORS_DIR is not set")
	}
	return "", nil
}

// zkGasKnownDivergent returns the vector ids listed in ZK_GAS_VECTORS_KNOWN_DIVERGENT, read
// through lookup. The variable is for local investigation only and is an error when CI is set.
func zkGasKnownDivergent(lookup func(string) (string, bool)) (map[string]bool, error) {
	list, set := lookup("ZK_GAS_VECTORS_KNOWN_DIVERGENT")
	if _, ci := lookup("CI"); set && ci {
		return nil, errors.New("ZK_GAS_VECTORS_KNOWN_DIVERGENT is refused because CI is set: it is for local investigation only")
	}
	ids := make(map[string]bool)
	for _, id := range strings.Split(list, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids[id] = true
		}
	}
	return ids, nil
}

// writeZkVectorFile writes one vector file into <dir>/cases.
func writeZkVectorFile(t *testing.T, dir, id, body string) {
	t.Helper()
	cases := filepath.Join(dir, "cases")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cases, id+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// decodeZkBlockTxs decodes the vector's signed txs and checks that each one is the EIP-1559 tx of
// the same input entry, signed by its signer on the vector chain.
func decodeZkBlockTxs(t *testing.T, in zkBlockInput, rawTxs []hexutil.Bytes) types.Transactions {
	t.Helper()
	if len(rawTxs) != len(in.Txs) {
		t.Fatalf("%d raw txs for %d input txs", len(rawTxs), len(in.Txs))
	}
	chainID := new(big.Int).SetUint64(uint64(in.Env.ChainID))
	signer := types.LatestSignerForChainID(chainID)
	txs := make(types.Transactions, len(rawTxs))
	for i, raw := range rawTxs {
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(raw); err != nil {
			t.Fatalf("raw tx %d: %v", i, err)
		}
		input := in.Txs[i]
		if tx.Type() != types.DynamicFeeTxType || signer.Hash(tx) != signer.Hash(types.NewTx(input.unsigned(chainID))) {
			t.Fatalf("raw tx %d is not the signed form of input tx %d", i, i)
		}
		sender, err := types.Sender(signer, tx)
		if err != nil {
			t.Fatalf("raw tx %d: sender: %v", i, err)
		}
		if addr, err := input.Signer.address(); err != nil || sender != addr {
			t.Fatalf("raw tx %d is signed by %v, want signer %d (%v, %v)", i, sender, input.Signer, addr, err)
		}
		txs[i] = tx
	}
	return txs
}

// newZkBlockMiner returns a miner and chain on the Taiko engine whose genesis (number 0, timestamp
// 0) holds exactly the vector alloc, on the vector chain with Shasta and Unzen active from genesis.
func newZkBlockMiner(t *testing.T, in zkBlockInput) (*Miner, *core.BlockChain) {
	t.Helper()
	zero := uint64(0)
	config := newUnzenTestChainConfig()
	config.ChainID = big.NewInt(zkBlockChainID)
	config.ShastaTime = &zero

	alloc := make(types.GenesisAlloc, len(in.Alloc))
	for addr, account := range in.Alloc {
		var storage map[common.Hash]common.Hash
		if len(account.Storage) > 0 {
			storage = make(map[common.Hash]common.Hash, len(account.Storage))
			for slot, value := range account.Storage {
				storage[(*uint256.Int)(&slot).Bytes32()] = (*uint256.Int)(&value).Bytes32()
			}
		}
		alloc[addr] = types.Account{
			Balance: (*uint256.Int)(&account.Balance).ToBig(),
			Nonce:   uint64(account.Nonce),
			Code:    account.Code,
			Storage: storage,
		}
	}
	gspec := &core.Genesis{
		Config:     config,
		Alloc:      alloc,
		Timestamp:  0,
		Difficulty: common.Big0,
		BaseFee:    big.NewInt(params.ShastaInitialBaseFee),
		GasLimit:   uint64(in.Env.GasLimit),
	}
	miner, backend := newUnzenTestWorkerWithConfig(t, gspec, testConfig)
	return miner, backend.chain
}

// sealZkBlock seals block 1 of env on top of parent from txs, as the driver hands a proposal's tx
// list to SealBlockWith, and checks that the header carries the vector environment.
func sealZkBlock(t *testing.T, miner *Miner, parent *types.Header, env zkBlockEnv, txs types.Transactions) *types.Block {
	t.Helper()
	txList, err := rlp.EncodeToBytes(txs)
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	baseFee := new(big.Int).SetUint64(uint64(env.BaseFee))
	block, err := miner.SealBlockWith(parent, uint64(env.Timestamp), 0, &engine.BlockMetadata{
		Beneficiary: env.Coinbase,
		GasLimit:    uint64(env.GasLimit),
		Timestamp:   uint64(env.Timestamp),
		MixHash:     env.PrevRandao,
		TxList:      txList,
		ExtraData:   env.ExtraData,
	}, baseFee, nil)
	if err != nil {
		t.Fatalf("seal %d txs: %v", len(txs), err)
	}
	header := block.Header()
	switch {
	case header.Number.Uint64() != uint64(env.Number):
		t.Fatalf("sealed number %v, env %d", header.Number, env.Number)
	case header.BaseFee.Cmp(baseFee) != 0:
		t.Fatalf("sealed base fee %v, env %v", header.BaseFee, baseFee)
	case header.Time != uint64(env.Timestamp) || header.Coinbase != env.Coinbase ||
		header.GasLimit != uint64(env.GasLimit) || header.MixDigest != env.PrevRandao ||
		!bytes.Equal(header.Extra, env.ExtraData):
		t.Fatalf("sealed header does not carry the vector environment: %+v", header)
	}
	return block
}

// importZkBlock imports block on top of parent as a node does, without writing it: header and
// body validation, execution on the parent state, then the post-execution checks (gas used,
// bloom, receipt root, requests hash, state root).
func importZkBlock(chain *core.BlockChain, parent *types.Header, block *types.Block) error {
	if err := chain.Engine().VerifyHeader(chain, block.Header()); err != nil {
		return fmt.Errorf("header: %w", err)
	}
	if err := chain.Validator().ValidateBody(block); err != nil {
		return fmt.Errorf("body: %w", err)
	}
	statedb, err := chain.StateAt(parent.Root)
	if err != nil {
		return fmt.Errorf("parent state: %w", err)
	}
	res, err := chain.Processor().Process(context.Background(), block, statedb, vm.Config{})
	if err != nil {
		return fmt.Errorf("execution: %w", err)
	}
	if err := chain.Validator().ValidateState(block, statedb, res, false); err != nil {
		return fmt.Errorf("post-execution: %w", err)
	}
	return nil
}

// zkGasLimitRejection reports whether an import failed because a tx exceeded the block zk gas
// limit or the body extends past the zk gas truncation point.
func zkGasLimitRejection(err error) bool {
	return errors.Is(err, vm.ErrZkGasLimitExceeded) ||
		strings.Contains(err.Error(), "Unzen block body extends past zk gas truncation point")
}

// zkGasDifficultyRejection reports whether an import failed because the header difficulty is not
// the block zk gas.
func zkGasDifficultyRejection(err error) bool {
	return strings.Contains(err.Error(), "zk gas difficulty mismatch")
}

// zkImportVerdict maps an import result to its verdict. A rejection counts only when one of
// reasons matches the error: the built block and the extended block accept any zk gas reason,
// the block with a wrong difficulty only the difficulty mismatch.
func zkImportVerdict(err error, reasons ...func(error) bool) string {
	if err == nil {
		return zkBlockAccept
	}
	for _, reason := range reasons {
		if reason(err) {
			return zkBlockReject
		}
	}
	return zkBlockNotZkRejection + err.Error()
}

// runZkBlockVector seals the vector's block, seals each prefix of its tx list that ends at an
// included tx for the cumulative zk gas, and imports the built block, the built block plus the
// truncated tx (when the vector truncates) and the built block with its difficulty raised by one.
// It fails the test when the vector is malformed or sealing fails.
func runZkBlockVector(t *testing.T, v zkBlockVector) zkBlockExpected {
	t.Helper()
	return runZkBlockVectorSealedBy(t, v, sealZkBlock)
}

// zkBlockSealer seals block 1 of env on top of parent from txs, as sealZkBlock does.
type zkBlockSealer func(t *testing.T, miner *Miner, parent *types.Header, env zkBlockEnv, txs types.Transactions) *types.Block

// runZkBlockVectorSealedBy is runZkBlockVector with seal sealing the vector's block from all its
// txs; the prefixes are still sealed by sealZkBlock. The trace tests use it to trace exactly the
// block build.
func runZkBlockVectorSealedBy(t *testing.T, v zkBlockVector, seal zkBlockSealer) zkBlockExpected {
	t.Helper()
	if v.Expected == nil {
		t.Fatal("vector has no expected values to read the signed txs from")
	}
	in := v.Input
	if in.Env.ChainID != zkBlockChainID || in.Env.Number != 1 {
		t.Fatalf("block vectors build block 1 on chain %d, got block %d on chain %d", zkBlockChainID, in.Env.Number, in.Env.ChainID)
	}
	txs := decodeZkBlockTxs(t, in, v.Expected.RawTxs)
	index := make(map[common.Hash]int, len(txs))
	for i, tx := range txs {
		if _, dup := index[tx.Hash()]; dup {
			t.Fatalf("input txs %d and %d are the same tx", index[tx.Hash()], i)
		}
		index[tx.Hash()] = i
	}

	miner, chain := newZkBlockMiner(t, in)
	genesis := chain.Genesis().Header()
	block := seal(t, miner, genesis, in.Env, txs)

	got := zkBlockExpected{
		TruncatedAt: v.Expected.TruncatedAt,
		BlockZkGas:  hexutil.Uint64(block.Difficulty().Uint64()),
		StateRoot:   block.Root(),
	}
	for i, tx := range txs {
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf("encode tx %d: %v", i, err)
		}
		got.RawTxs = append(got.RawTxs, raw)
	}
	for _, tx := range block.Transactions() {
		i, ok := index[tx.Hash()]
		if !ok {
			t.Fatalf("sealed block holds tx %v, which is not an input tx", tx.Hash())
		}
		got.Included = append(got.Included, i)
	}
	for n, i := range got.Included {
		prefix := sealZkBlock(t, miner, genesis, in.Env, txs[:i+1])
		if have := prefix.Transactions(); len(have) != n+1 || have[n].Hash() != txs[i].Hash() {
			t.Fatalf("sealing txs 0..%d included %d txs, the full seal included %d of them", i, len(have), n+1)
		}
		got.CumulativeZkGas = append(got.CumulativeZkGas, hexutil.Uint64(prefix.Difficulty().Uint64()))
	}

	got.ImportBuilt = zkImportVerdict(importZkBlock(chain, genesis, block), zkGasLimitRejection, zkGasDifficultyRejection)
	if at := v.Expected.TruncatedAt; at != nil {
		if *at < 0 || *at >= len(txs) {
			t.Fatalf("truncated_at %d is not an input index", *at)
		}
		body := &types.Body{
			Transactions: append(slices.Clone(block.Transactions()), txs[*at]),
			Withdrawals:  block.Withdrawals(),
		}
		// NewBlock recomputes the tx root (and, given no receipts, resets the receipt root, which
		// only the post-execution checks read); the difficulty stays that of the built block.
		extended := types.NewBlock(block.Header(), body, nil, trie.NewStackTrie(nil))
		verdict := zkImportVerdict(importZkBlock(chain, genesis, extended), zkGasLimitRejection, zkGasDifficultyRejection)
		got.ImportExtended = &verdict
	}
	header := block.Header()
	header.Difficulty = new(big.Int).Add(header.Difficulty, common.Big1)
	got.ImportWrongDifficulty = zkImportVerdict(importZkBlock(chain, genesis, block.WithSeal(header)), zkGasDifficultyRejection)
	return got
}

// compareZkBlock reports every field in which got differs from want. A state root difference is
// reported as a setup divergence and an import that failed for a reason other than zk gas as a
// runner error, apart from the zk gas differences.
func compareZkBlock(want, got zkBlockExpected) error {
	var zkGas, setup, runner []string
	differ := func(list *[]string, field string, want, got any) {
		*list = append(*list, fmt.Sprintf("%s: expected %v, geth %v", field, want, got))
	}
	verdict := func(field, want, got string) {
		switch {
		case want == got:
		case strings.HasPrefix(got, zkBlockNotZkRejection):
			differ(&runner, field, strconv.Quote(want), got)
		default:
			differ(&zkGas, field, strconv.Quote(want), strconv.Quote(got))
		}
	}
	optional := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}

	if !slices.EqualFunc(want.RawTxs, got.RawTxs, func(a, b hexutil.Bytes) bool { return bytes.Equal(a, b) }) {
		differ(&runner, "raw_txs", want.RawTxs, got.RawTxs)
	}
	if !slices.Equal(want.Included, got.Included) {
		differ(&zkGas, "included", want.Included, got.Included)
	}
	if !slices.Equal(want.CumulativeZkGas, got.CumulativeZkGas) {
		differ(&zkGas, "cumulative_zk_gas", want.CumulativeZkGas, got.CumulativeZkGas)
	}
	if want.BlockZkGas != got.BlockZkGas {
		differ(&zkGas, "block_zk_gas", want.BlockZkGas, got.BlockZkGas)
	}
	if want.StateRoot != got.StateRoot {
		differ(&setup, "state_root", want.StateRoot, got.StateRoot)
	}
	verdict("import_built", want.ImportBuilt, got.ImportBuilt)
	if (want.ImportExtended == nil) != (got.ImportExtended == nil) {
		differ(&zkGas, "import_extended", optional(want.ImportExtended), optional(got.ImportExtended))
	} else if want.ImportExtended != nil {
		verdict("import_extended", *want.ImportExtended, *got.ImportExtended)
	}
	verdict("import_wrong_difficulty", want.ImportWrongDifficulty, got.ImportWrongDifficulty)

	var errs []error
	for _, group := range []struct {
		label string
		diffs []string
	}{{"zk gas mismatch", zkGas}, {"setup divergence", setup}, {"runner error", runner}} {
		if len(group.diffs) > 0 {
			errs = append(errs, fmt.Errorf("%s: %s", group.label, strings.Join(group.diffs, "; ")))
		}
	}
	return errors.Join(errs...)
}

// zkBlockTransferVectorJSON is the corpus vector smoke.block.transfer with only the expected
// values the runner reads (the signed txs), so the comparison tests run without the corpus.
const zkBlockTransferVectorJSON = `{
	"schema_version": 1,
	"id": "smoke.block.transfer",
	"layer": "block",
	"cells": ["metering.intrinsic.charged"],
	"description": "The anchor plus one 1-wei transfer from signer 1 to an empty account; no truncation.",
	"input": {
		"env": {
			"chain_id": "0x28c59",
			"number": "0x1",
			"timestamp": "0xc",
			"base_fee": "0x17d7840",
			"coinbase": "0x0000000000000000000000000000000000c0ffee",
			"gas_limit": "0x2faf080",
			"prev_randao": "0x2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a",
			"extra_data": "0x4b000000000001"
		},
		"alloc": {
			"0x1670010000000000000000000000000000010001": { "balance": "0x0", "code": "0x00" },
			"0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf": { "balance": "0xde0b6b3a7640000" },
			"0x0000000000000000000000000000000000c0ffee": { "balance": "0x1" }
		},
		"txs": [
			{
				"signer": "golden_touch", "nonce": "0x0", "to": "0x1670010000000000000000000000000000010001",
				"gas": "0xf4240", "max_fee": "0x17d7840", "max_priority_fee": "0x0", "value": "0x0",
				"data": "0x523e6854"
			},
			{
				"signer": 1, "nonce": "0x0", "to": "0x000000000000000000000000000000000000beef",
				"gas": "0x5208", "max_fee": "0x17d7840", "max_priority_fee": "0x0", "value": "0x1",
				"data": "0x"
			}
		]
	},
	"expected": {
		"raw_txs": [
			"0x02f86e83028c59808084017d7840830f42409416700100000000000000000000000000000100018084523e6854c001a077c9819ad91bc8c341abbd41d41d41b851bf4cade2dd943fdf421e796b952ce3a0653034f4ffd280ca8ca71f966f2ee2e7a2f59e6ee0ba1be905805cc46c399718",
			"0x02f86983028c59808084017d784082520894000000000000000000000000000000000000beef0180c080a08215553c15862fb5d468969b52bf648c27a7d1b83a8ad368541ba1865d8cb211a024c3837ff81fbb7646900ddd9621a9d715a14954dfed5164f791284f1e2cc8a5"
		]
	}
}`

// inlineTransferVector returns smoke.block.transfer without reading the corpus.
func inlineTransferVector(t *testing.T) zkBlockVector {
	t.Helper()
	var v zkBlockVector
	if err := decodeZkVectorJSON([]byte(zkBlockTransferVectorJSON), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestZkGasBlockCompareRejectsMutations(t *testing.T) {
	got := runZkBlockVector(t, inlineTransferVector(t))
	if err := compareZkBlock(got, got); err != nil {
		t.Fatalf("identical result rejected: %v", err)
	}
	if len(got.Included) < 2 || len(got.CumulativeZkGas) != len(got.Included) {
		t.Fatalf("unexpected result %s", got)
	}
	// Each mutation and the category its mismatch must be reported under.
	mutations := map[string]struct {
		mutate func(*zkBlockExpected)
		label  string
	}{
		"block_zk_gas": {func(e *zkBlockExpected) { e.BlockZkGas++ }, "zk gas mismatch"},
		"included":     {func(e *zkBlockExpected) { e.Included = e.Included[:len(e.Included)-1] }, "zk gas mismatch"},
		"cumulative_zk_gas[0]": {func(e *zkBlockExpected) {
			e.CumulativeZkGas = append([]hexutil.Uint64(nil), e.CumulativeZkGas...)
			e.CumulativeZkGas[0]++
		}, "zk gas mismatch"},
		"state_root": {func(e *zkBlockExpected) { e.StateRoot[0] ^= 0xff }, "setup divergence"},
	}
	for name, m := range mutations {
		want := got
		m.mutate(&want)
		err := compareZkBlock(want, got)
		switch {
		case err == nil:
			t.Errorf("mutated %s accepted", name)
		case !strings.HasPrefix(err.Error(), m.label+": "):
			t.Errorf("mutated %s reported as %q, want a %s", name, err, m.label)
		}
	}
}

// signZkBlockTxs signs every input tx of in, for vectors a test builds without the corpus.
func signZkBlockTxs(t *testing.T, in zkBlockInput) []hexutil.Bytes {
	t.Helper()
	chainID := new(big.Int).SetUint64(uint64(in.Env.ChainID))
	signer := types.LatestSignerForChainID(chainID)
	raw := make([]hexutil.Bytes, len(in.Txs))
	for i, input := range in.Txs {
		key := goldenTouchTestKey(t)
		if input.Signer != zkGoldenTouchSigner {
			var err error
			if key, err = input.Signer.numberedKey(); err != nil {
				t.Fatal(err)
			}
		}
		enc, err := types.MustSignNewTx(key, signer, input.unsigned(chainID)).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		raw[i] = enc
	}
	return raw
}

// TestZkGasBlockTruncatedVectorRejectsExtendedBlock checks the runner on a block that truncates:
// two txs that each run a loop for 10,000,000 gas do not both fit the block zk gas limit, so
// building stops at the second one and the transfer after it (the sentinel) is left out too. The
// sentinel comes from a second funded signer, so it stays valid without the dropped tx. It asserts
// only what the runner must report, not reference values.
func TestZkGasBlockTruncatedVectorRejectsExtendedBlock(t *testing.T) {
	v := inlineTransferVector(t)
	loop := common.HexToAddress("0x000000000000000000000000000000000000100c")
	v.Input.Alloc[loop] = zkVectorAccount{Code: hexutil.MustDecode("0x5b600056")} // JUMPDEST, PUSH1 0, JUMP
	sentinelSigner := zkTxSigner(2)
	sentinelAccount, err := sentinelSigner.address()
	if err != nil {
		t.Fatal(err)
	}
	v.Input.Alloc[sentinelAccount] = v.Input.Alloc[common.HexToAddress("0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf")]
	transfer := v.Input.Txs[1]
	burn := transfer
	burn.To, burn.Gas, burn.Value = &loop, 10_000_000, hexutil.U256{}
	burn2 := burn
	burn2.Nonce = 1
	sentinel := transfer
	sentinel.Signer = sentinelSigner
	v.Input.Txs = []zkBlockTx{v.Input.Txs[0], burn, burn2, sentinel}
	truncatedAt := 2
	v.Expected = &zkBlockExpected{RawTxs: signZkBlockTxs(t, v.Input), TruncatedAt: &truncatedAt}

	got := runZkBlockVector(t, v)
	if !slices.Equal(got.Included, []int{0, 1}) {
		t.Fatalf("included %v, want [0 1]", got.Included)
	}
	if n := len(got.CumulativeZkGas); n != 2 || got.CumulativeZkGas[0] >= got.CumulativeZkGas[1] || got.CumulativeZkGas[1] != got.BlockZkGas {
		t.Fatalf("cumulative zk gas %v with block zk gas %v", got.CumulativeZkGas, got.BlockZkGas)
	}
	if got.ImportBuilt != zkBlockAccept || got.ImportExtended == nil || *got.ImportExtended != zkBlockReject || got.ImportWrongDifficulty != zkBlockReject {
		t.Fatalf("import verdicts %s", got)
	}
}

func TestZkGasBlockVectorLoaderRejectsEmptyLayer(t *testing.T) {
	if _, err := loadZkGasVectors[zkBlockVector](t.TempDir(), "block"); err == nil {
		t.Fatal("empty dir accepted")
	}
	dir := t.TempDir()
	writeZkVectorFile(t, dir, "only.harness", `{"schema_version": 1, "id": "only.harness", "layer": "harness"}`)
	if _, err := loadZkGasVectors[zkBlockVector](dir, "block"); err == nil {
		t.Fatal("corpus without block vectors accepted")
	}
}

func TestZkGasBlockImportVerdictReasons(t *testing.T) {
	limit := fmt.Errorf("execution: %w", vm.ErrZkGasLimitExceeded)
	difficulty := errors.New("execution: zk gas difficulty mismatch: header has 2, recomputed 1")
	other := errors.New("execution: invalid gas used")
	tests := []struct {
		name    string
		err     error
		reasons []func(error) bool
		want    string
	}{
		{"accepted", nil, nil, zkBlockAccept},
		{"limit, any zk reason", limit, []func(error) bool{zkGasLimitRejection, zkGasDifficultyRejection}, zkBlockReject},
		{"difficulty, any zk reason", difficulty, []func(error) bool{zkGasLimitRejection, zkGasDifficultyRejection}, zkBlockReject},
		{"difficulty, difficulty only", difficulty, []func(error) bool{zkGasDifficultyRejection}, zkBlockReject},
		{"limit, difficulty only", limit, []func(error) bool{zkGasDifficultyRejection}, zkBlockNotZkRejection + limit.Error()},
		{"other, any zk reason", other, []func(error) bool{zkGasLimitRejection, zkGasDifficultyRejection}, zkBlockNotZkRejection + other.Error()},
	}
	for _, tt := range tests {
		if got := zkImportVerdict(tt.err, tt.reasons...); got != tt.want {
			t.Errorf("%s: verdict %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestZkGasCorpusEnvironment(t *testing.T) {
	env := func(vars map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			value, ok := vars[key]
			return value, ok
		}
	}
	if dir, err := zkGasCorpusDir(env(nil)); dir != "" || err != nil {
		t.Errorf("unset corpus: dir %q, error %v; want a skip", dir, err)
	}
	if dir, err := zkGasCorpusDir(env(map[string]string{"ZK_GAS_VECTORS_DIR": "/vectors", "ZK_GAS_VECTORS_REQUIRED": "1"})); dir != "/vectors" || err != nil {
		t.Errorf("set corpus: dir %q, error %v", dir, err)
	}
	if _, err := zkGasCorpusDir(env(map[string]string{"ZK_GAS_VECTORS_REQUIRED": "1"})); err == nil {
		t.Error("ZK_GAS_VECTORS_REQUIRED=1 without ZK_GAS_VECTORS_DIR accepted")
	}
	ids, err := zkGasKnownDivergent(env(map[string]string{"ZK_GAS_VECTORS_KNOWN_DIVERGENT": " a, b ,"}))
	if err != nil || len(ids) != 2 || !ids["a"] || !ids["b"] {
		t.Errorf("known divergent ids %v, error %v", ids, err)
	}
	if _, err := zkGasKnownDivergent(env(map[string]string{"CI": "true"})); err != nil {
		t.Errorf("CI without known divergent ids refused: %v", err)
	}
	if _, err := zkGasKnownDivergent(env(map[string]string{"CI": "true", "ZK_GAS_VECTORS_KNOWN_DIVERGENT": "a"})); err == nil {
		t.Error("ZK_GAS_VECTORS_KNOWN_DIVERGENT accepted with CI set")
	}
}

func TestZkGasBlockVectors(t *testing.T) {
	dir, err := zkGasCorpusDir(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Skip("ZK_GAS_VECTORS_DIR is not set")
	}
	known, err := zkGasKnownDivergent(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := loadZkGasVectors[zkBlockVector](dir, "block")
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	for _, v := range vectors {
		ok := t.Run(v.ID, func(t *testing.T) {
			if known[v.ID] {
				t.Skipf("known divergent, skipped: %s", v.ID)
			}
			if v.Expected == nil {
				t.Fatal("vector has no expected values")
			}
			got := runZkBlockVector(t, v)
			if err := compareZkBlock(*v.Expected, got); err != nil {
				t.Fatal(err)
			}
			t.Logf("geth %s", got)
		})
		if !ok {
			failed = append(failed, v.ID)
		}
	}
	if len(failed) > 0 {
		t.Errorf("%d of %d block vectors failed: %v", len(failed), len(vectors), failed)
	}
}
