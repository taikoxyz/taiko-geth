package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

var (
	// taikoNPHasher runs JUMPDEST; PUSH3 0x010000; PUSH1 0; KECCAK256; POP;
	// PUSH1 0; JUMP, hashing 64 KiB per iteration until the zk gas budget of a
	// block runs out.
	taikoNPHasher     = common.HexToAddress("0x0000000000000000000000000000000000000022")
	taikoNPHasherCode = common.FromHex("0x5b6201000060002050600056")
	taikoNPRecipient  = common.HexToAddress("0x00000000000000000000000000000000000000aa")
	taikoNPCoinbase   = common.HexToAddress("0x00000000000000000000000000000000000000bb")
	taikoNPEtnaRoot   = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000e7")
	taikoNPUnzenExtra = []byte{25, 0, 0, 0, 0, 0, 1}
	taikoNPEtnaExtra  = []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 7}
)

// taikoNPEnv is a Taiko devnet node serving TaikoEngineAPI.
type taikoNPEnv struct {
	t       *testing.T
	eth     *eth.Ethereum
	api     *TaikoEngineAPI
	config  *params.ChainConfig
	genesis *types.Header
}

// newTaikoNPEnv starts a Taiko devnet node whose Unzen, Cancun, Prague and
// Osaka activate at unzenTime and whose Etna activates at etnaTime (nil:
// never). testAddr is funded and taikoNPHasher holds the hashing loop.
func newTaikoNPEnv(t *testing.T, unzenTime uint64, etnaTime *uint64) *taikoNPEnv {
	t.Helper()
	config := *core.TaikoGenesisBlock(params.TaikoInternalNetworkID.Uint64()).Config
	config.UnzenTime = &unzenTime
	config.CancunTime = &unzenTime
	config.PragueTime = &unzenTime
	config.OsakaTime = &unzenTime
	config.EtnaTime = etnaTime
	genesis := &core.Genesis{
		Config:     &config,
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(10_000_000),
		Difficulty: common.Big0,
		Alloc: types.GenesisAlloc{
			testAddr:      {Balance: testBalance},
			taikoNPHasher: {Code: taikoNPHasherCode},
		},
	}
	n, ethservice := startEthService(t, genesis, nil)
	t.Cleanup(func() { n.Close() })
	return &taikoNPEnv{
		t:       t,
		eth:     ethservice,
		api:     &TaikoEngineAPI{api: newConsensusAPIWithoutHeartbeat(ethservice)},
		config:  ethservice.BlockChain().Config(),
		genesis: ethservice.BlockChain().Genesis().Header(),
	}
}

// nextHeader returns the header of a child of parent, 12 seconds later, with
// the EIP-4396 base fee and the fork's extra data and root. Its state root,
// receipts, bloom, gas used and difficulty are left for the caller.
func (e *taikoNPEnv) nextHeader(parent *types.Header) *types.Header {
	var parentBlockTime uint64
	if parent.Number.Sign() > 0 {
		grandparent := e.eth.BlockChain().GetHeader(parent.ParentHash, parent.Number.Uint64()-1)
		parentBlockTime = parent.Time - grandparent.Time
	}
	time := parent.Time + 12
	extra, root := taikoNPUnzenExtra, common.Hash{}
	if e.config.IsEtna(time) {
		extra, root = taikoNPEtnaExtra, taikoNPEtnaRoot
	}
	var (
		zero         uint64
		withdrawals  = types.EmptyWithdrawalsHash
		requestsHash = types.EmptyRequestsHash
	)
	return &types.Header{
		ParentHash:       parent.Hash(),
		UncleHash:        types.EmptyUncleHash,
		Coinbase:         taikoNPCoinbase,
		Difficulty:       new(big.Int),
		Number:           new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:         parent.GasLimit,
		Time:             time,
		Extra:            common.CopyBytes(extra),
		MixDigest:        common.Hash{0x01},
		BaseFee:          misc.CalcEIP4396BaseFee(e.config, parent, parentBlockTime),
		WithdrawalsHash:  &withdrawals,
		BlobGasUsed:      &zero,
		ExcessBlobGas:    &zero,
		ParentBeaconRoot: &root,
		RequestsHash:     &requestsHash,
	}
}

// sealValid executes plain transfers on parent's state and returns the valid
// child block. Each transfer costs exactly the intrinsic zk gas.
func (e *taikoNPEnv) sealValid(parent *types.Header, txs ...*types.Transaction) *types.Block {
	e.t.Helper()
	chain := e.eth.BlockChain()
	header := e.nextHeader(parent)
	header.Difficulty = new(big.Int).SetUint64(uint64(len(txs)) * vm.UnzenZkGasSchedule.TxIntrinsicZkGas)
	statedb, err := chain.StateAt(parent.Root)
	if err != nil {
		e.t.Fatalf("parent state: %v", err)
	}
	body := &types.Body{Transactions: txs, Withdrawals: []*types.Withdrawal{}}
	res, err := core.NewStateProcessor(chain).Process(context.Background(), types.NewBlock(header, body, nil, trie.NewStackTrie(nil)), statedb, vm.Config{})
	if err != nil {
		e.t.Fatalf("execute block %d: %v", header.Number, err)
	}
	header.GasUsed = res.GasUsed
	header.Root = statedb.IntermediateRoot(e.config.IsEIP158(header.Number))
	return types.NewBlock(header, body, res.Receipts, trie.NewStackTrie(nil))
}

// withHeader returns a block with block's body under the given header, so the
// block hash matches a header that execution will reject.
func taikoNPWithHeader(block *types.Block, edit func(*types.Header)) *types.Block {
	header := block.Header()
	edit(header)
	return types.NewBlockWithHeader(header).WithBody(*block.Body())
}

// unexecuted returns a child of parent carrying txs whose header fields other
// than the transaction root are those of an empty block: execution must reject
// it before the state root is compared.
func (e *taikoNPEnv) unexecuted(parent *types.Header, txs ...*types.Transaction) *types.Block {
	body := &types.Body{Transactions: txs, Withdrawals: []*types.Withdrawal{}}
	return types.NewBlock(e.nextHeader(parent), body, nil, trie.NewStackTrie(nil))
}

func (e *taikoNPEnv) transfer(nonce uint64) *types.Transaction {
	return types.MustSignNewTx(testKey, types.LatestSigner(e.config), &types.DynamicFeeTx{
		ChainID:   e.config.ChainID,
		Nonce:     nonce,
		GasTipCap: common.Big0,
		GasFeeCap: big.NewInt(1_000_000_000),
		Gas:       21_000,
		To:        &taikoNPRecipient,
		Value:     common.Big1,
	})
}

func (e *taikoNPEnv) hasherCall(nonce uint64) *types.Transaction {
	return types.MustSignNewTx(testKey, types.LatestSigner(e.config), &types.DynamicFeeTx{
		ChainID:   e.config.ChainID,
		Nonce:     nonce,
		GasTipCap: common.Big0,
		GasFeeCap: big.NewInt(1_000_000_000),
		Gas:       5_000_000,
		To:        &taikoNPHasher,
	})
}

func (e *taikoNPEnv) blobTx(nonce uint64) *types.Transaction {
	return types.MustSignNewTx(testKey, types.LatestSigner(e.config), &types.BlobTx{
		ChainID:    uint256.MustFromBig(e.config.ChainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(0),
		GasFeeCap:  uint256.NewInt(1_000_000_000),
		Gas:        21_000,
		To:         taikoNPRecipient,
		BlobFeeCap: uint256.NewInt(1),
		BlobHashes: []common.Hash{{0x01}},
	})
}

// taikoNPPayload returns block as a newPayloadV4 payload.
func taikoNPPayload(block *types.Block) engine.TaikoExecutionPayloadV3 {
	txs := make([][]byte, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		txs[i], _ = tx.MarshalBinary()
	}
	return engine.TaikoExecutionPayloadV3{
		ParentHash:       block.ParentHash(),
		FeeRecipient:     block.Coinbase(),
		StateRoot:        block.Root(),
		ReceiptsRoot:     block.ReceiptHash(),
		LogsBloom:        block.Bloom().Bytes(),
		Random:           block.MixDigest(),
		Number:           block.NumberU64(),
		GasLimit:         block.GasLimit(),
		GasUsed:          block.GasUsed(),
		Timestamp:        block.Time(),
		ExtraData:        block.Extra(),
		BaseFeePerGas:    block.BaseFee(),
		BlockHash:        block.Hash(),
		Transactions:     txs,
		Withdrawals:      []*types.Withdrawal{},
		HeaderDifficulty: block.Difficulty().Uint64(),
	}
}

// taikoNPPayloadJSON returns block as the JSON object a driver sends.
func taikoNPPayloadJSON(block *types.Block) map[string]any {
	txs := make([]hexutil.Bytes, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		txs[i], _ = tx.MarshalBinary()
	}
	return map[string]any{
		"parentHash":       block.ParentHash(),
		"feeRecipient":     block.Coinbase(),
		"stateRoot":        block.Root(),
		"receiptsRoot":     block.ReceiptHash(),
		"logsBloom":        hexutil.Bytes(block.Bloom().Bytes()),
		"prevRandao":       block.MixDigest(),
		"blockNumber":      hexutil.Uint64(block.NumberU64()),
		"gasLimit":         hexutil.Uint64(block.GasLimit()),
		"gasUsed":          hexutil.Uint64(block.GasUsed()),
		"timestamp":        hexutil.Uint64(block.Time()),
		"extraData":        hexutil.Bytes(block.Extra()),
		"baseFeePerGas":    (*hexutil.Big)(block.BaseFee()),
		"blockHash":        block.Hash(),
		"transactions":     txs,
		"withdrawals":      []*types.Withdrawal{},
		"blobGasUsed":      hexutil.Uint64(0),
		"excessBlobGas":    hexutil.Uint64(0),
		"headerDifficulty": block.Difficulty().Uint64(),
	}
}

// newPayload submits payload with empty versioned hashes and requests.
func (e *taikoNPEnv) newPayload(payload engine.TaikoExecutionPayloadV3, root common.Hash) (engine.PayloadStatusV1, error) {
	return e.api.NewPayloadV4(context.Background(), payload, []common.Hash{}, &root, []hexutil.Bytes{})
}

// cachedInvalid reports whether hash is in the invalid-ancestor cache.
func (e *taikoNPEnv) cachedInvalid(hash common.Hash) bool {
	e.api.api.invalidLock.Lock()
	defer e.api.api.invalidLock.Unlock()
	_, ok := e.api.api.invalidTipsets[hash]
	return ok
}

// taikoNPErrorDetail returns the data message of an Engine API error.
func taikoNPErrorDetail(err error) string {
	var apiErr *engine.EngineAPIError
	if !errors.As(err, &apiErr) {
		return ""
	}
	enc, _ := json.Marshal(apiErr.ErrorData())
	var data struct {
		Err string `json:"err"`
	}
	_ = json.Unmarshal(enc, &data)
	return data.Err
}

func taikoNPRequireStatus(t *testing.T, status engine.PayloadStatusV1, err error, want string, wantLVH *common.Hash) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != want {
		t.Fatalf("status = %s (%v), want %s", status.Status, status.ValidationError, want)
	}
	switch {
	case wantLVH == nil && status.LatestValidHash != nil:
		t.Fatalf("latestValidHash = %x, want null", *status.LatestValidHash)
	case wantLVH != nil && status.LatestValidHash == nil:
		t.Fatalf("latestValidHash = null, want %x", *wantLVH)
	case wantLVH != nil && *status.LatestValidHash != *wantLVH:
		t.Fatalf("latestValidHash = %x, want %x", *status.LatestValidHash, *wantLVH)
	}
}

func taikoNPHashPtr(h common.Hash) *common.Hash { return &h }

// TestTaikoNewPayloadV4StrictDecoding checks the JSON-RPC decoding of
// engine_newPayloadV4: every argument is required and non-null, the payload
// has exactly the 17 standard keys plus a decimal headerDifficulty, and any
// other key is rejected even when null.
func TestTaikoNewPayloadV4StrictDecoding(t *testing.T) {
	env := newTaikoNPEnv(t, 0, nil)
	block := env.sealValid(env.genesis)

	srv := rpc.NewServer()
	if err := srv.RegisterName("engine", env.api); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer srv.Stop()
	client := rpc.DialInProc(srv)
	defer client.Close()

	call := func(args ...any) (engine.PayloadStatusV1, error) {
		var status engine.PayloadStatusV1
		err := client.CallContext(context.Background(), &status, "engine_newPayloadV4", args...)
		return status, err
	}
	payload := func(edit func(map[string]any)) map[string]any {
		obj := taikoNPPayloadJSON(block)
		edit(obj)
		return obj
	}
	var (
		null   = json.RawMessage("null")
		hashes = []common.Hash{}
		root   = common.Hash{}
		reqs   = []hexutil.Bytes{}
	)
	cases := map[string][]any{
		"txHash null":              {payload(func(p map[string]any) { p["txHash"] = null }), hashes, root, reqs},
		"txHash zero":              {payload(func(p map[string]any) { p["txHash"] = common.Hash{} }), hashes, root, reqs},
		"withdrawalsHash null":     {payload(func(p map[string]any) { p["withdrawalsHash"] = null }), hashes, root, reqs},
		"taikoBlock null":          {payload(func(p map[string]any) { p["taikoBlock"] = null }), hashes, root, reqs},
		"slotNumber null":          {payload(func(p map[string]any) { p["slotNumber"] = null }), hashes, root, reqs},
		"blockAccessList null":     {payload(func(p map[string]any) { p["blockAccessList"] = null }), hashes, root, reqs},
		"targetGasLimit null":      {payload(func(p map[string]any) { p["targetGasLimit"] = null }), hashes, root, reqs},
		"headerDifficulty missing": {payload(func(p map[string]any) { delete(p, "headerDifficulty") }), hashes, root, reqs},
		"headerDifficulty null":    {payload(func(p map[string]any) { p["headerDifficulty"] = null }), hashes, root, reqs},
		"headerDifficulty hex":     {payload(func(p map[string]any) { p["headerDifficulty"] = "0x0" }), hashes, root, reqs},
		"headerDifficulty string":  {payload(func(p map[string]any) { p["headerDifficulty"] = "0" }), hashes, root, reqs},
		"headerDifficulty 2^64":    {payload(func(p map[string]any) { p["headerDifficulty"] = json.RawMessage("18446744073709551616") }), hashes, root, reqs},
		"headerDifficulty case": {payload(func(p map[string]any) {
			delete(p, "headerDifficulty")
			p["HeaderDifficulty"] = 0
		}), hashes, root, reqs},
		"transactions missing": {payload(func(p map[string]any) { delete(p, "transactions") }), hashes, root, reqs},
		"transactions null":    {payload(func(p map[string]any) { p["transactions"] = null }), hashes, root, reqs},
		"withdrawals null":     {payload(func(p map[string]any) { p["withdrawals"] = null }), hashes, root, reqs},
		"blobGasUsed missing":  {payload(func(p map[string]any) { delete(p, "blobGasUsed") }), hashes, root, reqs},
		"excessBlobGas null":   {payload(func(p map[string]any) { p["excessBlobGas"] = null }), hashes, root, reqs},
		"payload null":         {null, hashes, root, reqs},
		"versionedHashes null": {payload(func(map[string]any) {}), null, root, reqs},
		"root null":            {payload(func(map[string]any) {}), hashes, null, reqs},
		"requests null":        {payload(func(map[string]any) {}), hashes, root, null},
		"requests missing":     {payload(func(map[string]any) {}), hashes, root},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := call(args...); taikoRPCErrorCode(t, err) != -32602 {
				t.Fatalf("error = %v (code %d), want -32602", err, taikoRPCErrorCode(t, err))
			}
		})
	}
	// The unmodified payload decodes and imports.
	status, err := call(payload(func(map[string]any) {}), hashes, root, reqs)
	taikoNPRequireStatus(t, status, err, engine.VALID, taikoNPHashPtr(block.Hash()))
}

// TestTaikoNewPayloadV4UnsupportedFork checks that a payload before Unzen gets
// -38005, after the missing-argument checks and before the root checks.
func TestTaikoNewPayloadV4UnsupportedFork(t *testing.T) {
	const unzenTime = 1000
	env := newTaikoNPEnv(t, unzenTime, nil)
	payload := engine.TaikoExecutionPayloadV3{
		ParentHash:    env.genesis.Hash(),
		LogsBloom:     make([]byte, 256),
		Number:        1,
		Timestamp:     unzenTime - 1,
		BaseFeePerGas: big.NewInt(params.ShastaInitialBaseFee),
		Transactions:  [][]byte{},
		Withdrawals:   []*types.Withdrawal{},
	}
	nonzero := common.Hash{0x01}
	if _, err := env.newPayload(payload, nonzero); taikoRPCErrorCode(t, err) != -38005 {
		t.Fatalf("pre-Unzen payload: error = %v, want -38005", err)
	}
	_, err := env.api.NewPayloadV4(context.Background(), payload, nil, &nonzero, []hexutil.Bytes{})
	if taikoRPCErrorCode(t, err) != -32602 {
		t.Fatalf("pre-Unzen payload with null versioned hashes: error = %v, want -32602", err)
	}
	payload.Timestamp = unzenTime
	if _, err := env.newPayload(payload, nonzero); taikoRPCErrorCode(t, err) != -32602 || taikoNPErrorDetail(err) != errPreEtnaBeaconRoot.Error() {
		t.Fatalf("Unzen payload with a nonzero root: error = %v (%s), want -32602 %q", err, taikoNPErrorDetail(err), errPreEtnaBeaconRoot)
	}
}

// TestTaikoNewPayloadV4EtnaParams checks the Etna -32602 rows in order: each
// payload also carries every later violation, so only the first check may
// answer.
func TestTaikoNewPayloadV4EtnaParams(t *testing.T) {
	env := newTaikoNPEnv(t, 0, new(uint64))
	block := env.sealValid(env.genesis)
	withdrawal := []*types.Withdrawal{{Index: 0, Validator: 1, Address: taikoNPRecipient, Amount: 1}}
	hashes := []common.Hash{{0x01}}
	requests := []hexutil.Bytes{{0x00, 0x01}}

	tests := []struct {
		name     string
		root     common.Hash
		edit     func(*engine.TaikoExecutionPayloadV3)
		hashes   []common.Hash
		requests []hexutil.Bytes
		want     error
	}{
		{"zero root", common.Hash{}, func(p *engine.TaikoExecutionPayloadV3) {
			p.Withdrawals, p.BlobGasUsed, p.ExcessBlobGas = withdrawal, 1, 1
		}, hashes, requests, errEtnaZeroBeaconRoot},
		{"withdrawals", taikoNPEtnaRoot, func(p *engine.TaikoExecutionPayloadV3) {
			p.Withdrawals, p.BlobGasUsed, p.ExcessBlobGas = withdrawal, 1, 1
		}, hashes, requests, errEtnaWithdrawals},
		{"blob gas used", taikoNPEtnaRoot, func(p *engine.TaikoExecutionPayloadV3) { p.BlobGasUsed = 1 }, hashes, requests, errEtnaBlobGas},
		{"excess blob gas", taikoNPEtnaRoot, func(p *engine.TaikoExecutionPayloadV3) { p.ExcessBlobGas = 1 }, hashes, requests, errEtnaBlobGas},
		{"versioned hashes", taikoNPEtnaRoot, func(*engine.TaikoExecutionPayloadV3) {}, hashes, requests, errEtnaVersionedHashes},
		{"requests", taikoNPEtnaRoot, func(*engine.TaikoExecutionPayloadV3) {}, []common.Hash{}, requests, errEtnaRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := taikoNPPayload(block)
			tt.edit(&payload)
			_, err := env.api.NewPayloadV4(context.Background(), payload, tt.hashes, &tt.root, tt.requests)
			if taikoRPCErrorCode(t, err) != -32602 || taikoNPErrorDetail(err) != tt.want.Error() {
				t.Fatalf("error = %v (%s), want -32602 %q", err, taikoNPErrorDetail(err), tt.want)
			}
		})
	}
	// The block itself is valid, and the genesis payload is exempt from the
	// root rule: it is already known.
	status, err := env.newPayload(taikoNPPayload(block), taikoNPEtnaRoot)
	taikoNPRequireStatus(t, status, err, engine.VALID, taikoNPHashPtr(block.Hash()))

	genesis := env.eth.BlockChain().GetBlockByHash(env.genesis.Hash())
	status, err = env.newPayload(taikoNPPayload(genesis), common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.VALID, taikoNPHashPtr(genesis.Hash()))
}

// TestTaikoNewPayloadV4UnzenSideData checks that an Unzen payload rejects a
// nonzero root with -32602 and Osaka side data with an uncached INVALID.
func TestTaikoNewPayloadV4UnzenSideData(t *testing.T) {
	env := newTaikoNPEnv(t, 0, nil)
	block := env.sealValid(env.genesis)
	withdrawal := []*types.Withdrawal{{Index: 0, Validator: 1, Address: taikoNPRecipient, Amount: 1}}

	// The root check comes before the side-data check.
	payload := taikoNPPayload(block)
	payload.Withdrawals = withdrawal
	if _, err := env.newPayload(payload, common.Hash{0x01}); taikoRPCErrorCode(t, err) != -32602 || taikoNPErrorDetail(err) != errPreEtnaBeaconRoot.Error() {
		t.Fatalf("nonzero root: error = %v (%s), want -32602 %q", err, taikoNPErrorDetail(err), errPreEtnaBeaconRoot)
	}

	tests := []struct {
		name     string
		edit     func(*engine.TaikoExecutionPayloadV3)
		hashes   []common.Hash
		requests []hexutil.Bytes
	}{
		{"withdrawals", func(p *engine.TaikoExecutionPayloadV3) { p.Withdrawals = withdrawal }, []common.Hash{}, []hexutil.Bytes{}},
		{"blob gas used", func(p *engine.TaikoExecutionPayloadV3) { p.BlobGasUsed = 1 }, []common.Hash{}, []hexutil.Bytes{}},
		{"excess blob gas", func(p *engine.TaikoExecutionPayloadV3) { p.ExcessBlobGas = 1 }, []common.Hash{}, []hexutil.Bytes{}},
		{"versioned hashes", func(*engine.TaikoExecutionPayloadV3) {}, []common.Hash{{0x01}}, []hexutil.Bytes{}},
		{"requests", func(*engine.TaikoExecutionPayloadV3) {}, []common.Hash{}, []hexutil.Bytes{{0x00, 0x01}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := taikoNPPayload(block)
			tt.edit(&payload)
			status, err := env.api.NewPayloadV4(context.Background(), payload, tt.hashes, new(common.Hash), tt.requests)
			taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(env.genesis.Hash()))
			if *status.ValidationError != errPreEtnaOsakaFields.Error() {
				t.Fatalf("validation error = %q, want %q", *status.ValidationError, errPreEtnaOsakaFields)
			}
			if env.cachedInvalid(block.Hash()) {
				t.Fatal("side-data rejection was cached as invalid")
			}
		})
	}
	// Nothing was cached: the same block hash still imports.
	status, err := env.newPayload(taikoNPPayload(block), common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.VALID, taikoNPHashPtr(block.Hash()))
}

// TestTaikoNewPayloadV4BlobTransactions checks that an Etna blob transaction
// fails conversion (INVALID, not cached, even under a wrong block hash), while
// before Etna it is a block validation failure: cached once the parent is
// known, SYNCING while it is not.
func TestTaikoNewPayloadV4BlobTransactions(t *testing.T) {
	t.Run("etna", func(t *testing.T) {
		env := newTaikoNPEnv(t, 0, new(uint64))
		payload := taikoNPPayload(env.sealValid(env.genesis))
		enc, _ := env.blobTx(0).MarshalBinary()
		payload.Transactions = append(payload.Transactions, enc)

		status, err := env.newPayload(payload, taikoNPEtnaRoot)
		taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(env.genesis.Hash()))
		if !strings.Contains(*status.ValidationError, errEtnaBlobTransactions.Error()) {
			t.Fatalf("validation error = %q, want the blob transaction rejection", *status.ValidationError)
		}
		if env.cachedInvalid(payload.BlockHash) {
			t.Fatal("blob transaction rejection was cached as invalid")
		}
	})
	t.Run("unzen", func(t *testing.T) {
		env := newTaikoNPEnv(t, 0, nil)
		block := env.unexecuted(env.genesis, env.blobTx(0))
		status, err := env.newPayload(taikoNPPayload(block), common.Hash{})
		taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(env.genesis.Hash()))
		if !env.cachedInvalid(block.Hash()) {
			t.Fatal("Unzen blob block was not cached as invalid")
		}
		orphan := taikoNPWithHeader(block, func(h *types.Header) { h.ParentHash = common.Hash{0xde} })
		status, err = env.newPayload(taikoNPPayload(orphan), common.Hash{})
		taikoNPRequireStatus(t, status, err, engine.SYNCING, nil)
	})
}

// TestTaikoNewPayloadV4LatestValidHash checks latestValidHash for conversion
// failures: the parent hash when the parent is known (also for a block hash
// mismatch), the first valid ancestor found through the invalid-ancestor cache,
// and null otherwise.
func TestTaikoNewPayloadV4LatestValidHash(t *testing.T) {
	env := newTaikoNPEnv(t, 0, nil)
	genesis := env.genesis.Hash()
	block := env.sealValid(env.genesis)

	// A block hash mismatch on a known parent.
	payload := taikoNPPayload(block)
	payload.BlockHash = common.Hash{0x01}
	status, err := env.newPayload(payload, common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.INVALID, &genesis)
	if env.cachedInvalid(payload.BlockHash) || env.cachedInvalid(block.Hash()) {
		t.Fatal("block hash mismatch was cached as invalid")
	}

	// A block hash mismatch on an unknown parent.
	payload = taikoNPPayload(block)
	payload.ParentHash = common.Hash{0xde}
	status, err = env.newPayload(payload, common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.INVALID, nil)

	// A well-formed block on an unknown parent.
	orphan := taikoNPWithHeader(block, func(h *types.Header) { h.ParentHash = common.Hash{0xde} })
	status, err = env.newPayload(taikoNPPayload(orphan), common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.SYNCING, nil)

	// A block hash mismatch on an unknown parent that is cached as invalid:
	// the walk returns the invalid block's parent.
	bad := taikoNPWithHeader(block, func(h *types.Header) { h.Root = common.Hash{0x01} })
	status, err = env.newPayload(taikoNPPayload(bad), common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.INVALID, &genesis)
	if !env.cachedInvalid(bad.Hash()) {
		t.Fatal("state root mismatch was not cached as invalid")
	}
	payload = taikoNPPayload(block)
	payload.ParentHash, payload.Number = bad.Hash(), 2
	status, err = env.newPayload(payload, common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.INVALID, &genesis)
}

// TestTaikoNewPayloadV4BaseFeeAbove64Bits checks that a base fee above 64
// bits fails conversion even when the block hash commits to it: INVALID with
// the parent hash, and not cached.
func TestTaikoNewPayloadV4BaseFeeAbove64Bits(t *testing.T) {
	env := newTaikoNPEnv(t, 0, nil)
	block := taikoNPWithHeader(env.sealValid(env.genesis), func(h *types.Header) {
		h.BaseFee = new(big.Int).Lsh(common.Big1, 64)
	})
	status, err := env.newPayload(taikoNPPayload(block), common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(env.genesis.Hash()))
	if env.cachedInvalid(block.Hash()) {
		t.Fatal("base fee conversion failure was cached as invalid")
	}
}

// TestTaikoNewPayloadV4ParentTimestamp checks that a timestamp below the
// parent's is left to header verification: INVALID with the parent hash, and
// cached like any other block validation failure.
func TestTaikoNewPayloadV4ParentTimestamp(t *testing.T) {
	env := newTaikoNPEnv(t, 0, nil)
	parent := env.sealValid(env.genesis)
	status, err := env.newPayload(taikoNPPayload(parent), common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.VALID, taikoNPHashPtr(parent.Hash()))

	child := taikoNPWithHeader(env.sealValid(parent.Header()), func(h *types.Header) { h.Time = parent.Time() - 1 })
	status, err = env.newPayload(taikoNPPayload(child), common.Hash{})
	taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(parent.Hash()))
	if !env.cachedInvalid(child.Hash()) {
		t.Fatal("timestamp failure was not cached as invalid")
	}
}

// TestTaikoNewPayloadV4IsServed checks that Register serves engine_newPayloadV4
// on Taiko chains: a call without arguments fails argument decoding (-32602)
// instead of naming an unknown method (-32601).
func TestTaikoNewPayloadV4IsServed(t *testing.T) {
	n, _ := taikoEngineTestNode(t, newTaikoEngineTestGenesis())
	client := n.Attach()
	defer client.Close()

	err := client.Call(new(json.RawMessage), "engine_newPayloadV4")
	if code := taikoRPCErrorCode(t, err); code != -32602 {
		t.Fatalf("engine_newPayloadV4 without arguments: error code = %d (%v), want -32602", code, err)
	}
}

// TestTaikoNewPayloadV4Import checks a valid import before and from Etna, and
// that an execution failure on a parent with nonzero difficulty (zk gas)
// reports the parent hash. A later payload linking to the cached block gets
// the invalid-ancestor answer, which keeps the zero hash for such a parent.
func TestTaikoNewPayloadV4Import(t *testing.T) {
	for name, etnaTime := range map[string]*uint64{"unzen": nil, "etna": new(uint64)} {
		t.Run(name, func(t *testing.T) {
			env := newTaikoNPEnv(t, 0, etnaTime)
			root := common.Hash{}
			if etnaTime != nil {
				root = taikoNPEtnaRoot
			}
			parent := env.sealValid(env.genesis, env.transfer(0))
			if parent.Difficulty().Sign() == 0 {
				t.Fatal("test block has no zk gas")
			}
			for range 2 { // the second answer is for an already known block
				status, err := env.newPayload(taikoNPPayload(parent), root)
				taikoNPRequireStatus(t, status, err, engine.VALID, taikoNPHashPtr(parent.Hash()))
			}

			bad := taikoNPWithHeader(env.sealValid(parent.Header()), func(h *types.Header) { h.Root = common.Hash{0x01} })
			status, err := env.newPayload(taikoNPPayload(bad), root)
			taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(parent.Hash()))

			status, err = env.newPayload(taikoNPPayload(bad), root)
			taikoNPRequireStatus(t, status, err, engine.INVALID, &common.Hash{})
			if *status.ValidationError != "links to previously rejected block" {
				t.Fatalf("validation error = %q, want the invalid-ancestor answer", *status.ValidationError)
			}
		})
	}
}

// TestTaikoNewPayloadV4ZkGas checks that a zk-gas exhaustion or a zk-gas /
// difficulty mismatch is an internal error before Etna, and is not cached,
// while from Etna it is an INVALID block like any other.
func TestTaikoNewPayloadV4ZkGas(t *testing.T) {
	tests := []struct {
		name  string
		etna  bool
		block func(*taikoNPEnv) *types.Block
	}{
		{"exhaustion", false, func(e *taikoNPEnv) *types.Block { return e.unexecuted(e.genesis, e.hasherCall(0)) }},
		{"difficulty mismatch", false, func(e *taikoNPEnv) *types.Block {
			return taikoNPWithHeader(e.sealValid(e.genesis), func(h *types.Header) { h.Difficulty = common.Big1 })
		}},
		{"exhaustion at index 1", false, func(e *taikoNPEnv) *types.Block {
			return e.unexecuted(e.genesis, e.transfer(0), e.hasherCall(1))
		}},
		{"exhaustion", true, func(e *taikoNPEnv) *types.Block { return e.unexecuted(e.genesis, e.hasherCall(0)) }},
		{"exhaustion at index 1", true, func(e *taikoNPEnv) *types.Block {
			return e.unexecuted(e.genesis, e.transfer(0), e.hasherCall(1))
		}},
		{"difficulty mismatch", true, func(e *taikoNPEnv) *types.Block {
			return taikoNPWithHeader(e.sealValid(e.genesis), func(h *types.Header) { h.Difficulty = common.Big1 })
		}},
	}
	for _, tt := range tests {
		name := "unzen " + tt.name
		etnaTime, root := (*uint64)(nil), common.Hash{}
		if tt.etna {
			name, etnaTime, root = "etna "+tt.name, new(uint64), taikoNPEtnaRoot
		}
		t.Run(name, func(t *testing.T) {
			env := newTaikoNPEnv(t, 0, etnaTime)
			block := tt.block(env)
			for range 2 { // a rejection before Etna is not cached
				status, err := env.newPayload(taikoNPPayload(block), root)
				if !tt.etna {
					if taikoRPCErrorCode(t, err) != -32603 {
						t.Fatalf("status = %+v, error = %v, want -32603", status, err)
					}
					if env.cachedInvalid(block.Hash()) {
						t.Fatal("zk gas failure before Etna was cached as invalid")
					}
					continue
				}
				taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(env.genesis.Hash()))
				if !env.cachedInvalid(block.Hash()) {
					t.Fatal("Etna zk gas failure was not cached as invalid")
				}
			}
		})
	}
}

// TestTaikoNewPayloadV4EtnaExtraData checks that an Etna block with a 7-byte
// extraData is INVALID while the 13-byte one imports.
func TestTaikoNewPayloadV4EtnaExtraData(t *testing.T) {
	env := newTaikoNPEnv(t, 0, new(uint64))
	block := env.sealValid(env.genesis)
	short := taikoNPWithHeader(block, func(h *types.Header) { h.Extra = common.CopyBytes(taikoNPUnzenExtra) })

	status, err := env.newPayload(taikoNPPayload(short), taikoNPEtnaRoot)
	taikoNPRequireStatus(t, status, err, engine.INVALID, taikoNPHashPtr(env.genesis.Hash()))
	if !strings.Contains(*status.ValidationError, "extra-data length") {
		t.Fatalf("validation error = %q, want the extra-data length rejection", *status.ValidationError)
	}
	status, err = env.newPayload(taikoNPPayload(block), taikoNPEtnaRoot)
	taikoNPRequireStatus(t, status, err, engine.VALID, taikoNPHashPtr(block.Hash()))
}
