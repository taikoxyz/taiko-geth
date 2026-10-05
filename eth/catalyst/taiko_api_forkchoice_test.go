package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// fcuTestGenesisTime is the genesis timestamp of the Taiko test chain.
	// Unzen (with Cancun, Prague and Osaka) activates at genesis, Etna later,
	// so one chain serves pre-Unzen, pre-Etna and Etna targets.
	fcuTestGenesisTime = 1000
	fcuTestUnzenTime   = 1000
	fcuTestEtnaTime    = 1100

	// fcuTestPreEtnaTime and fcuTestEtnaTarget are block-1 targets on
	// either side of the Etna boundary.
	fcuTestPreEtnaTime = 1050
	fcuTestEtnaTarget  = 1200

	fcuTestGasLimit = 30_000_000
)

var (
	fcuTestBeneficiary = common.HexToAddress("0x000000000000000000000000000000000000beef")
	fcuTestPrevRandao  = common.HexToHash("0x0a")
	fcuTestEtnaRoot    = common.HexToHash("0xe7a0")
	fcuTestL1BlockHash = common.HexToHash("0x11")

	// fcuTestEmptyTxList is the RLP encoding of an empty transaction list.
	fcuTestEmptyTxList = []byte{0xc0}
)

// newTaikoFCUTestGenesis returns a Taiko genesis at fcuTestGenesisTime with
// Ontake, Pacaya and Shasta active from genesis, Cancun, Prague, Osaka and
// Unzen at fcuTestUnzenTime, and Etna at fcuTestEtnaTime. It installs the
// EIP-4788 and EIP-2935 system contracts and a no-op TaikoL2 contract for
// anchor transactions.
func newTaikoFCUTestGenesis() *core.Genesis {
	config := *params.TaikoChainConfig
	shasta, unzen, etna := uint64(0), uint64(fcuTestUnzenTime), uint64(fcuTestEtnaTime)
	config.OntakeBlock = common.Big0
	config.PacayaBlock = common.Big0
	config.ShastaTime = &shasta
	config.CancunTime = &unzen
	config.PragueTime = &unzen
	config.OsakaTime = &unzen
	config.UnzenTime = &unzen
	config.EtnaTime = &etna
	return &core.Genesis{
		Config: &config,
		Alloc: types.GenesisAlloc{
			params.BeaconRootsAddress:                 {Nonce: 1, Code: params.BeaconRootsCode, Balance: common.Big0},
			params.HistoryStorageAddress:              {Nonce: 1, Code: params.HistoryStorageCode, Balance: common.Big0},
			core.TaikoTreasuryAddress(config.ChainID): {Code: []byte{0x00}, Balance: common.Big0}, // STOP
		},
		Timestamp:  fcuTestGenesisTime,
		GasLimit:   fcuTestGasLimit,
		Difficulty: common.Big0,
		BaseFee:    big.NewInt(params.ShastaInitialBaseFee),
	}
}

// startTaikoFCUTestService starts a node on the newTaikoFCUTestGenesis chain
// and returns a Taiko engine service without the heartbeat.
func startTaikoFCUTestService(t *testing.T) (*eth.Ethereum, *TaikoEngineAPI) {
	t.Helper()
	n, ethservice := startEthService(t, newTaikoFCUTestGenesis(), nil)
	t.Cleanup(func() { n.Close() })
	return ethservice, &TaikoEngineAPI{api: newConsensusAPIWithoutHeartbeat(ethservice)}
}

// taikoFCUTestAttrs returns valid attributes for block 1 at timestamp. Etna
// targets get a non-zero root and 13-byte extra data, earlier targets a zero
// root and 7-byte extra data; both encode proposal ID 7.
func taikoFCUTestAttrs(config *params.ChainConfig, timestamp uint64, txList []byte) *engine.TaikoPayloadAttributesV3 {
	root := common.Hash{}
	extra := []byte{25, 0, 0, 0, 0, 0, 7}
	if config.IsEtna(timestamp) {
		root = fcuTestEtnaRoot
		extra = []byte{25, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 9}
	}
	return &engine.TaikoPayloadAttributesV3{PayloadAttributes: engine.PayloadAttributes{
		Timestamp:             timestamp,
		Random:                fcuTestPrevRandao,
		SuggestedFeeRecipient: fcuTestBeneficiary,
		Withdrawals:           []*types.Withdrawal{},
		BeaconRoot:            &root,
		BaseFeePerGas:         big.NewInt(params.ShastaInitialBaseFee),
		BlockMetadata: &engine.BlockMetadata{
			Beneficiary: fcuTestBeneficiary,
			GasLimit:    fcuTestGasLimit,
			Timestamp:   timestamp,
			MixHash:     fcuTestPrevRandao,
			TxList:      txList,
			ExtraData:   extra,
		},
		L1Origin: &rawdb.L1Origin{
			BlockID:       big.NewInt(1),
			L1BlockHeight: big.NewInt(100),
			L1BlockHash:   fcuTestL1BlockHash,
		},
	}}
}

// taikoFCUTestAnchorTxList returns a tx list holding only a valid Shasta anchor
// transaction for a block with the given base fee.
func taikoFCUTestAnchorTxList(t *testing.T, config *params.ChainConfig, baseFee *big.Int) ([]byte, *types.Transaction) {
	t.Helper()
	key, err := crypto.HexToECDSA("92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38")
	if err != nil {
		t.Fatalf("golden touch key: %v", err)
	}
	to := core.TaikoTreasuryAddress(config.ChainID)
	anchor := types.MustSignNewTx(key, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     0,
		GasTipCap: common.Big0,
		GasFeeCap: baseFee,
		Gas:       taiko.AnchorV3V4GasLimit,
		To:        &to,
		Data:      taiko.AnchorV4Selector,
	})
	txList, err := rlp.EncodeToBytes(types.Transactions{anchor})
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	return txList, anchor
}

// fcuTestCachedPayload returns the execution payload of the last build,
// failing unless that build has the given id.
func fcuTestCachedPayload(t *testing.T, api *TaikoEngineAPI, id engine.PayloadID) *engine.ExecutableData {
	t.Helper()
	envelope := api.resolveLastPayload(id)
	if envelope == nil {
		t.Fatalf("payload %v is not the last built payload", id)
	}
	return envelope.ExecutionPayload
}

// fcuTestLastPayload returns the payload object of the last build.
func fcuTestLastPayload(api *TaikoEngineAPI) *miner.Payload {
	api.lastPayloadLock.RLock()
	defer api.lastPayloadLock.RUnlock()
	return api.lastPayload
}

// fcuTestBuiltBlock rebuilds the block cached under id with the given parent
// beacon root, failing unless the root reproduces the cached block hash.
func fcuTestBuiltBlock(t *testing.T, api *TaikoEngineAPI, id engine.PayloadID, root common.Hash) *types.Block {
	t.Helper()
	data := *fcuTestCachedPayload(t, api, id)
	if data.HeaderDifficulty == nil {
		data.HeaderDifficulty = new(big.Int)
	}
	block, err := engine.ExecutableDataToBlock(data, nil, &root, nil)
	if err != nil {
		t.Fatalf("rebuild payload %v: %v", id, err)
	}
	return block
}

// fcuTestInsertEtnaBlock builds an empty Etna block on top of the genesis at
// fcuTestEtnaTarget and imports it without moving the head.
func fcuTestInsertEtnaBlock(t *testing.T, ethservice *eth.Ethereum, api *TaikoEngineAPI) *types.Block {
	t.Helper()
	config := ethservice.BlockChain().Config()
	update := engine.ForkchoiceStateV1{HeadBlockHash: ethservice.BlockChain().Genesis().Hash()}
	res, err := api.ForkchoiceUpdatedV3(context.Background(), update, taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList))
	if err != nil || res.PayloadID == nil {
		t.Fatalf("build Etna block: id %v, err %v", res.PayloadID, err)
	}
	block := fcuTestBuiltBlock(t, api, *res.PayloadID, fcuTestEtnaRoot)
	if _, err := ethservice.BlockChain().InsertBlockWithoutSetHead(context.Background(), block, false); err != nil {
		t.Fatalf("import Etna block: %v", err)
	}
	return block
}

// TestTaikoPayloadIDVector pins the payload ID of the shared cross-client
// vector: parent 0, timestamp 1000, zero randao and recipient, empty
// withdrawals, no root, a zero tx-list hash and empty extra data.
func TestTaikoPayloadIDVector(t *testing.T) {
	args := miner.BuildPayloadArgs{
		Timestamp:   1000,
		Withdrawals: []*types.Withdrawal{},
		Version:     engine.PayloadV2,
		TxListHash:  &common.Hash{},
	}
	want := engine.PayloadID{0x02, 0x84, 0xe3, 0x12, 0x99, 0xa2, 0xad, 0x9e}
	if got := args.Id(); got != want {
		t.Fatalf("payload id = %v, want %v", got, want)
	}
}

// TestTaikoPayloadID pins taikoPayloadID to the attribute-only preimage: the
// expected values are sha256 digests computed independently of Go, and only
// the fields of that preimage change the ID.
func TestTaikoPayloadID(t *testing.T) {
	newAttrs := func(root *common.Hash, extra []byte) *engine.PayloadAttributes {
		return &engine.PayloadAttributes{
			Timestamp:     1000,
			Withdrawals:   []*types.Withdrawal{},
			BeaconRoot:    root,
			BaseFeePerGas: big.NewInt(params.ShastaInitialBaseFee),
			BlockMetadata: &engine.BlockMetadata{TxList: fcuTestEmptyTxList, ExtraData: extra},
			L1Origin:      &rawdb.L1Origin{BlockID: big.NewInt(1)},
		}
	}
	var (
		zeroRoot  = common.Hash{}
		etnaRoot  = common.BytesToHash(common.FromHex("0x" + "33333333333333333333333333333333" + "33333333333333333333333333333333"))
		otherRoot = common.BytesToHash(common.FromHex("0x" + "34343434343434343434343434343434" + "34343434343434343434343434343434"))
		etnaHead  = common.BytesToHash(common.FromHex("0x" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
		etnaExtra = []byte{50, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 9}
	)
	tests := []struct {
		name  string
		head  common.Hash
		attrs *engine.PayloadAttributes
		want  string
	}{
		{"pre-Etna without root", common.Hash{}, newAttrs(nil, nil), "0x02f00df254c1be3b"},
		{"pre-Etna zero root hashes like no root", common.Hash{}, newAttrs(&zeroRoot, nil), "0x02f00df254c1be3b"},
		{"Etna root", etnaHead, newAttrs(&etnaRoot, etnaExtra), "0x021fd6b6f422f646"},
		{"another Etna root", etnaHead, newAttrs(&otherRoot, etnaExtra), "0x025a84ab32634d04"},
		{"zero root with Etna extra", etnaHead, newAttrs(&zeroRoot, etnaExtra), "0x0298681f56c0d8e4"},
	}
	for _, tt := range tests {
		if got := taikoPayloadID(tt.head, tt.attrs); got.String() != tt.want {
			t.Errorf("%s: payload id = %v, want %v", tt.name, got, tt.want)
		}
	}

	// The proposal ID and the anchor block number in extraData both bind the ID.
	base := taikoPayloadID(etnaHead, newAttrs(&etnaRoot, etnaExtra))
	for _, extra := range [][]byte{
		{50, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 9},
		{50, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 10},
	} {
		if taikoPayloadID(etnaHead, newAttrs(&etnaRoot, extra)) == base {
			t.Errorf("extra data %x does not change the payload id", extra)
		}
	}
	// Fields outside the preimage never change the ID.
	other := newAttrs(&etnaRoot, etnaExtra)
	other.BaseFeePerGas = big.NewInt(1)
	other.BlockMetadata.Beneficiary = common.HexToAddress("0x01")
	other.BlockMetadata.GasLimit = 1
	other.BlockMetadata.MixHash = common.HexToHash("0x01")
	other.BlockMetadata.Timestamp = 1
	other.L1Origin = &rawdb.L1Origin{BlockID: big.NewInt(2), L1BlockHeight: big.NewInt(3)}
	if got := taikoPayloadID(etnaHead, other); got != base {
		t.Errorf("payload id = %v after changing fields outside the preimage, want %v", got, base)
	}
}

// TestTaikoForkchoiceUpdatedV3AttributeChecks covers every attribute check.
// A row also carries the later failures it can, so its code shows which
// check runs first.
func TestTaikoForkchoiceUpdatedV3AttributeChecks(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	config := ethservice.BlockChain().Config()
	update := engine.ForkchoiceStateV1{HeadBlockHash: ethservice.BlockChain().Genesis().Hash()}

	type mutation func(*engine.TaikoPayloadAttributesV3)
	var (
		slotNumber     mutation = func(a *engine.TaikoPayloadAttributesV3) { a.SlotNumber = new(uint64) }
		noWithdrawals  mutation = func(a *engine.TaikoPayloadAttributesV3) { a.Withdrawals = nil }
		noRoot         mutation = func(a *engine.TaikoPayloadAttributesV3) { a.BeaconRoot = nil }
		targetGasLimit mutation = func(a *engine.TaikoPayloadAttributesV3) { a.TargetGasLimitSet = true }
		preUnzen       mutation = func(a *engine.TaikoPayloadAttributesV3) {
			a.Timestamp = fcuTestUnzenTime - 1
			a.BlockMetadata.Timestamp = a.Timestamp
		}
		metadataTime mutation = func(a *engine.TaikoPayloadAttributesV3) { a.BlockMetadata.Timestamp++ }
		zeroRoot     mutation = func(a *engine.TaikoPayloadAttributesV3) { a.BeaconRoot = new(common.Hash) }
		anchorTx     mutation = func(a *engine.TaikoPayloadAttributesV3) { a.AnchorTransactionSet = true }
		withdrawal   mutation = func(a *engine.TaikoPayloadAttributesV3) {
			a.Withdrawals = []*types.Withdrawal{{Index: 1, Validator: 1, Address: common.HexToAddress("0x01"), Amount: 1}}
		}
		shortExtra  mutation = func(a *engine.TaikoPayloadAttributesV3) { a.BlockMetadata.ExtraData = a.BlockMetadata.ExtraData[:7] }
		bigBaseFee  mutation = func(a *engine.TaikoPayloadAttributesV3) { a.BaseFeePerGas = new(big.Int).Lsh(common.Big1, 64) }
		nonZeroRoot mutation = func(a *engine.TaikoPayloadAttributesV3) {
			root := common.HexToHash("0x01")
			a.BeaconRoot = &root
		}
	)
	tests := []struct {
		name      string
		timestamp uint64
		mutations []mutation
		code      int
	}{
		{"slot number", fcuTestEtnaTarget, []mutation{slotNumber, noWithdrawals, noRoot, preUnzen, targetGasLimit}, -38003},
		{"missing withdrawals", fcuTestEtnaTarget, []mutation{noWithdrawals, noRoot, preUnzen, targetGasLimit}, -38003},
		{"missing root", fcuTestEtnaTarget, []mutation{noRoot, preUnzen, targetGasLimit}, -38003},
		{"pre-Unzen target", fcuTestEtnaTarget, []mutation{preUnzen, targetGasLimit}, -38005},
		{"pre-Unzen target with a non-zero root", fcuTestPreEtnaTime, []mutation{preUnzen, nonZeroRoot}, -38005},
		{"target gas limit", fcuTestEtnaTarget, []mutation{targetGasLimit, metadataTime, zeroRoot, anchorTx, withdrawal, shortExtra, bigBaseFee}, -32602},
		{"target gas limit before Etna", fcuTestPreEtnaTime, []mutation{targetGasLimit}, -32602},
		{"Etna metadata timestamp", fcuTestEtnaTarget, []mutation{metadataTime}, -32602},
		{"Etna zero root", fcuTestEtnaTarget, []mutation{zeroRoot}, -32602},
		{"Etna anchor transaction", fcuTestEtnaTarget, []mutation{anchorTx}, -32602},
		{"Etna withdrawals", fcuTestEtnaTarget, []mutation{withdrawal}, -32602},
		{"Etna extra data length", fcuTestEtnaTarget, []mutation{shortExtra}, -32602},
		{"Etna base fee above u64", fcuTestEtnaTarget, []mutation{bigBaseFee}, -32602},
		{"pre-Etna non-zero root", fcuTestPreEtnaTime, []mutation{nonZeroRoot}, -32602},
	}
	for _, tt := range tests {
		attrs := taikoFCUTestAttrs(config, tt.timestamp, fcuTestEmptyTxList)
		for _, mutate := range tt.mutations {
			mutate(attrs)
		}
		_, err := api.ForkchoiceUpdatedV3(context.Background(), update, attrs)
		if err == nil {
			t.Errorf("%s: no error, want code %d", tt.name, tt.code)
			continue
		}
		if code := rpcErrorCode(t, err); code != tt.code {
			t.Errorf("%s: code %d (%v), want %d", tt.name, code, err, tt.code)
		}
	}
	if origin, _ := rawdb.ReadL1Origin(ethservice.ChainDb(), big.NewInt(1)); origin != nil {
		t.Fatalf("rejected attributes wrote an L1 origin: %+v", origin)
	}
}

// TestTaikoForkchoiceUpdatedV3ForkchoiceState checks the forkchoice outcomes
// that win over the attributes: -38002 for a zero head or an inconsistent
// state, and a plain SYNCING status for an unknown head, whether the
// attributes are absent, valid or invalid. None writes an L1 origin.
func TestTaikoForkchoiceUpdatedV3ForkchoiceState(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	config := ethservice.BlockChain().Config()
	genesis := ethservice.BlockChain().Genesis().Hash()
	unknown := common.HexToHash("0xdead")

	valid := taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList)
	invalid := taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList)
	invalid.TargetGasLimitSet = true

	tests := []struct {
		name   string
		update engine.ForkchoiceStateV1
		attrs  *engine.TaikoPayloadAttributesV3
		code   int    // expected error code, or 0 for a status
		status string // expected status when code is 0
	}{
		{"zero head", engine.ForkchoiceStateV1{}, nil, -38002, ""},
		{"zero head, valid attributes", engine.ForkchoiceStateV1{}, valid, -38002, ""},
		{"zero head, invalid attributes", engine.ForkchoiceStateV1{}, invalid, -38002, ""},
		{"unknown head", engine.ForkchoiceStateV1{HeadBlockHash: unknown}, nil, 0, engine.SYNCING},
		{"unknown head, valid attributes", engine.ForkchoiceStateV1{HeadBlockHash: unknown}, valid, 0, engine.SYNCING},
		{"unknown head, invalid attributes", engine.ForkchoiceStateV1{HeadBlockHash: unknown}, invalid, 0, engine.SYNCING},
		{"unknown finalized, valid attributes", engine.ForkchoiceStateV1{HeadBlockHash: genesis, FinalizedBlockHash: unknown}, valid, -38002, ""},
		{"unknown finalized, invalid attributes", engine.ForkchoiceStateV1{HeadBlockHash: genesis, FinalizedBlockHash: unknown}, invalid, -38002, ""},
		{"unknown safe, valid attributes", engine.ForkchoiceStateV1{HeadBlockHash: genesis, SafeBlockHash: unknown}, valid, -38002, ""},
		{"unknown safe, invalid attributes", engine.ForkchoiceStateV1{HeadBlockHash: genesis, SafeBlockHash: unknown}, invalid, -38002, ""},
	}
	for _, tt := range tests {
		res, err := api.ForkchoiceUpdatedV3(context.Background(), tt.update, tt.attrs)
		if tt.code != 0 {
			if err == nil {
				t.Errorf("%s: no error, want code %d", tt.name, tt.code)
			} else if code := rpcErrorCode(t, err); code != tt.code {
				t.Errorf("%s: code %d (%v), want %d", tt.name, code, err, tt.code)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", tt.name, err)
			continue
		}
		if res.PayloadStatus.Status != tt.status || res.PayloadID != nil {
			t.Errorf("%s: status %s, id %v, want %s without an id", tt.name, res.PayloadStatus.Status, res.PayloadID, tt.status)
		}
	}
	db := ethservice.ChainDb()
	if origin, _ := rawdb.ReadL1Origin(db, big.NewInt(1)); origin != nil {
		t.Fatalf("a non-VALID forkchoice wrote an L1 origin: %+v", origin)
	}
	if head, _ := rawdb.ReadHeadL1Origin(db); head != nil {
		t.Fatalf("a non-VALID forkchoice wrote the head L1 origin: %v", head)
	}
}

// TestTaikoForkchoiceUpdatedV3AppliesForkchoiceWithInvalidAttributes checks
// that invalid attributes still move the head, forward and back.
func TestTaikoForkchoiceUpdatedV3AppliesForkchoiceWithInvalidAttributes(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	chain := ethservice.BlockChain()
	block := fcuTestInsertEtnaBlock(t, ethservice, api)
	if chain.CurrentBlock().Hash() == block.Hash() {
		t.Fatal("the imported block must not be the head yet")
	}
	invalid := taikoFCUTestAttrs(chain.Config(), fcuTestEtnaTarget+1, fcuTestEmptyTxList)
	invalid.TargetGasLimitSet = true

	for _, head := range []common.Hash{block.Hash(), chain.Genesis().Hash()} {
		_, err := api.ForkchoiceUpdatedV3(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: head}, invalid)
		if err == nil || rpcErrorCode(t, err) != -32602 {
			t.Fatalf("forkchoice to %v: err %v, want code -32602", head, err)
		}
		if got := chain.CurrentBlock().Hash(); got != head {
			t.Fatalf("head = %v after the forkchoice update, want %v", got, head)
		}
	}
}

// TestTaikoForkchoiceUpdatedV3TimestampAgainstHead checks that a target
// before the head is -38003 while an equal timestamp builds.
func TestTaikoForkchoiceUpdatedV3TimestampAgainstHead(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	chain := ethservice.BlockChain()
	block := fcuTestInsertEtnaBlock(t, ethservice, api)
	update := engine.ForkchoiceStateV1{HeadBlockHash: block.Hash()}

	earlier := taikoFCUTestAttrs(chain.Config(), block.Time()-1, fcuTestEmptyTxList)
	earlier.L1Origin.BlockID = big.NewInt(2)
	_, err := api.ForkchoiceUpdatedV3(context.Background(), update, earlier)
	if err == nil || rpcErrorCode(t, err) != -38003 {
		t.Fatalf("earlier target: err %v, want code -38003", err)
	}
	if got := chain.CurrentBlock().Hash(); got != block.Hash() {
		t.Fatalf("head = %v after a rejected target, want %v", got, block.Hash())
	}
	if origin, _ := rawdb.ReadL1Origin(ethservice.ChainDb(), big.NewInt(2)); origin != nil {
		t.Fatalf("a rejected target wrote an L1 origin: %+v", origin)
	}

	equal := taikoFCUTestAttrs(chain.Config(), block.Time(), fcuTestEmptyTxList)
	equal.L1Origin.BlockID = big.NewInt(2)
	res, err := api.ForkchoiceUpdatedV3(context.Background(), update, equal)
	if err != nil {
		t.Fatalf("equal timestamp: %v", err)
	}
	if res.PayloadStatus.Status != engine.VALID || res.PayloadID == nil {
		t.Fatalf("equal timestamp: status %s, id %v, want VALID with an id", res.PayloadStatus.Status, res.PayloadID)
	}
}

// TestTaikoForkchoiceUpdatedV3BuildFailure checks that a failed build is
// -32603 and leaves neither a payload nor an L1 origin behind.
func TestTaikoForkchoiceUpdatedV3BuildFailure(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	genesis := ethservice.BlockChain().Genesis().Hash()
	// Before Etna an L2 block needs its anchor transaction, so an empty list
	// cannot be sealed.
	attrs := taikoFCUTestAttrs(ethservice.BlockChain().Config(), fcuTestPreEtnaTime, fcuTestEmptyTxList)
	_, err := api.ForkchoiceUpdatedV3(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: genesis}, attrs)
	if err == nil || rpcErrorCode(t, err) != -32603 {
		t.Fatalf("err %v, want code -32603", err)
	}
	if fcuTestLastPayload(api) != nil {
		t.Fatal("a failed build stored a payload")
	}
	db := ethservice.ChainDb()
	if origin, _ := rawdb.ReadL1Origin(db, big.NewInt(1)); origin != nil {
		t.Fatalf("a failed build wrote an L1 origin: %+v", origin)
	}
	if head, _ := rawdb.ReadHeadL1Origin(db); head != nil {
		t.Fatalf("a failed build wrote the head L1 origin: %v", head)
	}
}

// TestTaikoForkchoiceUpdatedV3UnzenBuild builds a pre-Etna block through the
// service. Its ID equals the one the V2 wire path derived from the sealed
// block, the anchorTransaction flag is ignored before Etna, and the block's
// zk-gas difficulty does not stop it from becoming the head.
func TestTaikoForkchoiceUpdatedV3UnzenBuild(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	config := ethservice.BlockChain().Config()
	genesis := ethservice.BlockChain().Genesis().Hash()

	txList, anchor := taikoFCUTestAnchorTxList(t, config, big.NewInt(params.ShastaInitialBaseFee))
	attrs := taikoFCUTestAttrs(config, fcuTestPreEtnaTime, txList)
	attrs.AnchorTransactionSet = true

	res, err := api.ForkchoiceUpdatedV3(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: genesis}, attrs)
	if err != nil {
		t.Fatalf("forkchoice: %v", err)
	}
	if res.PayloadStatus.Status != engine.VALID || res.PayloadID == nil {
		t.Fatalf("status %s, id %v, want VALID with an id", res.PayloadStatus.Status, res.PayloadID)
	}
	id := *res.PayloadID
	if want := taikoPayloadID(genesis, &attrs.PayloadAttributes); id != want {
		t.Fatalf("payload id = %v, want %v", id, want)
	}
	if id.Version() != engine.PayloadV2 {
		t.Fatalf("payload id version = %d, want %d", id.Version(), engine.PayloadV2)
	}
	data := fcuTestCachedPayload(t, api, id)
	if len(data.Transactions) != 1 {
		t.Fatalf("block has %d transactions, want the anchor only", len(data.Transactions))
	}
	var tx types.Transaction
	if err := tx.UnmarshalBinary(data.Transactions[0]); err != nil || tx.Hash() != anchor.Hash() {
		t.Fatalf("first transaction %v (err %v), want anchor %v", tx.Hash(), err, anchor.Hash())
	}
	// The V2 wire path hashed the sealed block's fields into the ID.
	txListHash := crypto.Keccak256Hash(txList)
	legacy := (&miner.BuildPayloadArgs{
		Parent:       data.ParentHash,
		Timestamp:    data.Timestamp,
		FeeRecipient: data.FeeRecipient,
		Random:       data.Random,
		Withdrawals:  data.Withdrawals,
		Version:      engine.PayloadV2,
		TxListHash:   &txListHash,
		Extra:        data.ExtraData,
	}).Id()
	if id != legacy {
		t.Fatalf("payload id = %v, want the V2-era id %v", id, legacy)
	}

	// Unzen stores zk gas in the difficulty, which must not mark the block as a
	// terminal proof-of-work block when it becomes the head.
	block := fcuTestBuiltBlock(t, api, id, common.Hash{})
	if block.Difficulty().Sign() == 0 {
		t.Fatal("the anchor block used no zk gas")
	}
	if _, err := ethservice.BlockChain().InsertBlockWithoutSetHead(context.Background(), block, false); err != nil {
		t.Fatalf("import the Unzen block: %v", err)
	}
	res, err = api.ForkchoiceUpdatedV3(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: block.Hash()}, nil)
	if err != nil || res.PayloadStatus.Status != engine.VALID {
		t.Fatalf("forkchoice to the Unzen block: status %s, err %v, want VALID", res.PayloadStatus.Status, err)
	}
	if got := ethservice.BlockChain().CurrentBlock().Hash(); got != block.Hash() {
		t.Fatalf("head = %v, want the Unzen block %v", got, block.Hash())
	}
}

// TestTaikoForkchoiceUpdatedV3EtnaBuild checks the Etna block built from the
// attributes: randomness, base fee, extra data and root come from them as
// given, and its ID covers the root.
func TestTaikoForkchoiceUpdatedV3EtnaBuild(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	config := ethservice.BlockChain().Config()
	genesis := ethservice.BlockChain().Genesis().Hash()

	attrs := taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList)
	attrs.BlockMetadata.MixHash = common.HexToHash("0x0b") // Etna takes prevRandao instead
	attrs.BaseFeePerGas = big.NewInt(30_000_000)           // used as given, not recomputed

	res, err := api.ForkchoiceUpdatedV3(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: genesis}, attrs)
	if err != nil {
		t.Fatalf("forkchoice: %v", err)
	}
	if res.PayloadStatus.Status != engine.VALID || res.PayloadID == nil {
		t.Fatalf("status %s, id %v, want VALID with an id", res.PayloadStatus.Status, res.PayloadID)
	}
	id := *res.PayloadID
	noRoot := attrs.PayloadAttributes
	noRoot.BeaconRoot = nil
	if id != taikoPayloadID(genesis, &attrs.PayloadAttributes) || id == taikoPayloadID(genesis, &noRoot) {
		t.Fatalf("payload id %v does not cover the Etna root", id)
	}
	data := fcuTestCachedPayload(t, api, id)
	switch {
	case data.ParentHash != genesis || data.Number != 1 || data.Timestamp != fcuTestEtnaTarget:
		t.Fatalf("block %d at %d on %v, want block 1 at %d on the genesis", data.Number, data.Timestamp, data.ParentHash, fcuTestEtnaTarget)
	case data.FeeRecipient != fcuTestBeneficiary:
		t.Fatalf("fee recipient = %v, want %v", data.FeeRecipient, fcuTestBeneficiary)
	case data.Random != fcuTestPrevRandao:
		t.Fatalf("prevRandao = %v, want %v", data.Random, fcuTestPrevRandao)
	case data.GasLimit != fcuTestGasLimit:
		t.Fatalf("gas limit = %d, want %d", data.GasLimit, fcuTestGasLimit)
	case data.BaseFeePerGas.Cmp(attrs.BaseFeePerGas) != 0:
		t.Fatalf("base fee = %v, want %v", data.BaseFeePerGas, attrs.BaseFeePerGas)
	case hexutil.Encode(data.ExtraData) != hexutil.Encode(attrs.BlockMetadata.ExtraData):
		t.Fatalf("extra data = %x, want %x", data.ExtraData, attrs.BlockMetadata.ExtraData)
	case data.Withdrawals == nil || len(data.Withdrawals) != 0 || len(data.Transactions) != 0:
		t.Fatalf("withdrawals %v, %d transactions, want an empty body", data.Withdrawals, len(data.Transactions))
	}
	// The root is in the header: only it reproduces the block hash.
	fcuTestBuiltBlock(t, api, id, fcuTestEtnaRoot)
	other := *data
	if other.HeaderDifficulty == nil {
		other.HeaderDifficulty = new(big.Int)
	}
	if _, err := engine.ExecutableDataToBlock(other, nil, &common.Hash{1}, nil); err == nil {
		t.Fatal("another root reproduced the block hash")
	}
}

// TestTaikoForkchoiceUpdatedV3L1OriginWrites checks the L1 origin written for
// a built block. A confirmed block also moves the head L1 origin and maps the
// extraData proposal ID to it; the metadata batch ID is ignored.
func TestTaikoForkchoiceUpdatedV3L1OriginWrites(t *testing.T) {
	tests := []struct {
		name      string
		height    *big.Int
		confirmed bool
	}{
		{"confirmed", big.NewInt(100), true},
		{"preconfirmation without an L1 height", nil, false},
		{"preconfirmation with a zero L1 height", big.NewInt(0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ethservice, api := startTaikoFCUTestService(t)
			db := ethservice.ChainDb()
			genesis := ethservice.BlockChain().Genesis().Hash()

			attrs := taikoFCUTestAttrs(ethservice.BlockChain().Config(), fcuTestEtnaTarget, fcuTestEmptyTxList)
			attrs.BlockMetadata.BatchID = big.NewInt(77)
			attrs.L1Origin = &rawdb.L1Origin{
				BlockID:            big.NewInt(1),
				L1BlockHeight:      tt.height,
				L1BlockHash:        fcuTestL1BlockHash,
				BuildPayloadArgsID: [8]byte{1, 2, 3},
				IsForcedInclusion:  true,
			}
			res, err := api.ForkchoiceUpdatedV3(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: genesis}, attrs)
			if err != nil || res.PayloadID == nil {
				t.Fatalf("forkchoice: id %v, err %v", res.PayloadID, err)
			}
			built := fcuTestCachedPayload(t, api, *res.PayloadID).BlockHash

			origin, err := rawdb.ReadL1Origin(db, big.NewInt(1))
			if err != nil || origin == nil {
				t.Fatalf("L1 origin missing: %v", err)
			}
			if origin.L2BlockHash != built || origin.L1BlockHash != fcuTestL1BlockHash ||
				origin.BuildPayloadArgsID != [8]byte{1, 2, 3} || !origin.IsForcedInclusion {
				t.Fatalf("L1 origin = %+v, want the attributes' origin for block %v", origin, built)
			}
			head, _ := rawdb.ReadHeadL1Origin(db)
			mapped, _ := rawdb.ReadBatchToLastBlockID(db, big.NewInt(7))
			if tt.confirmed {
				if head == nil || head.Cmp(common.Big1) != 0 {
					t.Fatalf("head L1 origin = %v, want 1", head)
				}
				if mapped == nil || mapped.ToInt().Cmp(common.Big1) != 0 {
					t.Fatalf("proposal 7 maps to %v, want block 1", mapped)
				}
			} else if head != nil || mapped != nil {
				t.Fatalf("preconfirmation wrote head %v and proposal mapping %v", head, mapped)
			}
			if batch, _ := rawdb.ReadBatchToLastBlockID(db, big.NewInt(77)); batch != nil {
				t.Fatalf("the metadata batch ID was mapped to %v", batch)
			}
		})
	}
}

// TestTaikoForkchoiceUpdatedV3CachedPayload checks that repeating the
// forkchoice update of the last built payload returns its ID without
// rebuilding and rewrites the L1 origin with its block.
func TestTaikoForkchoiceUpdatedV3CachedPayload(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	db := ethservice.ChainDb()
	config := ethservice.BlockChain().Config()
	update := engine.ForkchoiceStateV1{HeadBlockHash: ethservice.BlockChain().Genesis().Hash()}

	first, err := api.ForkchoiceUpdatedV3(context.Background(), update, taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList))
	if err != nil || first.PayloadID == nil {
		t.Fatalf("first forkchoice: id %v, err %v", first.PayloadID, err)
	}
	built := fcuTestCachedPayload(t, api, *first.PayloadID).BlockHash
	stored := fcuTestLastPayload(api)

	// Clobber the records; the origin is not part of the payload ID.
	rawdb.WriteL1Origin(db, big.NewInt(1), &rawdb.L1Origin{BlockID: big.NewInt(1)})
	rawdb.WriteHeadL1Origin(db, big.NewInt(99))
	again := taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList)
	again.L1Origin.L1BlockHash = common.HexToHash("0x22")

	second, err := api.ForkchoiceUpdatedV3(context.Background(), update, again)
	if err != nil || second.PayloadID == nil || *second.PayloadID != *first.PayloadID {
		t.Fatalf("second forkchoice: id %v, err %v, want the cached id %v", second.PayloadID, err, *first.PayloadID)
	}
	if fcuTestLastPayload(api) != stored {
		t.Fatalf("payload %v was built again, want the last built payload reused", *first.PayloadID)
	}
	origin, _ := rawdb.ReadL1Origin(db, big.NewInt(1))
	if origin == nil || origin.L2BlockHash != built || origin.L1BlockHash != common.HexToHash("0x22") {
		t.Fatalf("L1 origin = %+v, want block %v with the repeated origin", origin, built)
	}
	if head, _ := rawdb.ReadHeadL1Origin(db); head == nil || head.Cmp(common.Big1) != 0 {
		t.Fatalf("head L1 origin = %v, want 1", head)
	}
}

// TestTaikoForkchoiceUpdatedV3OverRPC calls the method the way a driver does,
// through Register and JSON, where slotNumber, targetGasLimit and
// anchorTransaction are keys on the wire and null means absent.
func TestTaikoForkchoiceUpdatedV3OverRPC(t *testing.T) {
	n, ethservice := newEngineTestNode(t, newTaikoFCUTestGenesis())
	client := n.Attach()
	defer client.Close()

	config := ethservice.BlockChain().Config()
	genesis := ethservice.BlockChain().Genesis().Hash()
	state := engine.ForkchoiceStateV1{HeadBlockHash: genesis}
	attrs := taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList)
	enc, err := json.Marshal(&attrs.PayloadAttributes)
	if err != nil {
		t.Fatalf("encode attributes: %v", err)
	}
	wire := func(extra map[string]any) map[string]any {
		var fields map[string]any
		if err := json.Unmarshal(enc, &fields); err != nil {
			t.Fatalf("decode attributes: %v", err)
		}
		maps.Copy(fields, extra)
		return fields
	}

	var res engine.ForkChoiceResponse
	if err := client.Call(&res, "engine_forkchoiceUpdatedV3", state, nil); err != nil || res.PayloadStatus.Status != engine.VALID {
		t.Fatalf("null attributes: status %s, err %v, want VALID", res.PayloadStatus.Status, err)
	}
	for _, tt := range []struct {
		name  string
		extra map[string]any
		code  int
	}{
		{"slotNumber", map[string]any{"slotNumber": "0x1"}, -38003},
		{"targetGasLimit", map[string]any{"targetGasLimit": "0x1c9c380"}, -32602},
		{"anchorTransaction", map[string]any{"anchorTransaction": "0x02"}, -32602},
	} {
		err := client.Call(new(json.RawMessage), "engine_forkchoiceUpdatedV3", state, wire(tt.extra))
		if err == nil {
			t.Fatalf("%s: no error, want code %d", tt.name, tt.code)
		}
		if code := rpcErrorCode(t, err); code != tt.code {
			t.Fatalf("%s: code %d (%v), want %d", tt.name, code, err, tt.code)
		}
	}
	res = engine.ForkChoiceResponse{}
	err = client.Call(&res, "engine_forkchoiceUpdatedV3", state, wire(map[string]any{"targetGasLimit": nil, "anchorTransaction": nil, "slotNumber": nil}))
	if err != nil || res.PayloadID == nil {
		t.Fatalf("null optional keys: id %v, err %v, want a payload id", res.PayloadID, err)
	}
	if want := taikoPayloadID(genesis, &attrs.PayloadAttributes); *res.PayloadID != want {
		t.Fatalf("payload id = %v, want %v", *res.PayloadID, want)
	}
}

// fcuTestBuild runs a forkchoice update on the genesis that builds the block
// described by attrs and returns its payload ID.
func fcuTestBuild(t *testing.T, ethservice *eth.Ethereum, api *TaikoEngineAPI, attrs *engine.TaikoPayloadAttributesV3) engine.PayloadID {
	t.Helper()
	update := engine.ForkchoiceStateV1{HeadBlockHash: ethservice.BlockChain().Genesis().Hash()}
	res, err := api.ForkchoiceUpdatedV3(context.Background(), update, attrs)
	if err != nil || res.PayloadStatus.Status != engine.VALID || res.PayloadID == nil {
		t.Fatalf("forkchoice at %d: status %s, id %v, err %v, want VALID with an id", attrs.Timestamp, res.PayloadStatus.Status, res.PayloadID, err)
	}
	return *res.PayloadID
}

// fcuTestGetPayloadV5 returns the JSON engine_getPayloadV5 envelope of id and
// the hash of the block it carries.
func fcuTestGetPayloadV5(t *testing.T, api *TaikoEngineAPI, id engine.PayloadID) (string, common.Hash) {
	t.Helper()
	envelope, err := api.GetPayloadV5(id)
	if err != nil {
		t.Fatalf("getPayloadV5(%v): %v", id, err)
	}
	enc, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	return string(enc), envelope.ExecutionPayload.BlockHash
}

// fcuTestUnknownPayload fails unless engine_getPayloadV5 answers -38001 for id.
func fcuTestUnknownPayload(t *testing.T, api *TaikoEngineAPI, id engine.PayloadID) {
	t.Helper()
	if _, err := api.GetPayloadV5(id); !errors.Is(err, engine.UnknownPayload) {
		t.Fatalf("getPayloadV5(%v): err %v, want -38001", id, err)
	}
}

// TestTaikoGetPayloadV5ServesOnlyTheLastBuiltPayload checks that a build
// replaces the previous payload, the way the reference client's payload
// service keeps only the last resolved one, and that the last payload is
// served any number of times.
func TestTaikoGetPayloadV5ServesOnlyTheLastBuiltPayload(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	config := ethservice.BlockChain().Config()

	x := fcuTestBuild(t, ethservice, api, taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList))
	fcuTestGetPayloadV5(t, api, x)
	z := fcuTestBuild(t, ethservice, api, taikoFCUTestAttrs(config, fcuTestEtnaTarget+1, fcuTestEmptyTxList))
	if x == z {
		t.Fatalf("both builds have payload id %v", x)
	}
	fcuTestUnknownPayload(t, api, x)

	first, _ := fcuTestGetPayloadV5(t, api, z)
	second, _ := fcuTestGetPayloadV5(t, api, z)
	if first != second {
		t.Fatalf("repeated getPayloadV5 differs:\nfirst  %s\nsecond %s", first, second)
	}
}

// TestTaikoForkchoiceUpdatedV3RebuildsAnOlderPayload checks that repeating the
// attributes of a replaced payload seals the same block again under the same
// ID, rewrites its L1 origin and makes it the last payload.
func TestTaikoForkchoiceUpdatedV3RebuildsAnOlderPayload(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	db := ethservice.ChainDb()
	config := ethservice.BlockChain().Config()

	x := fcuTestBuild(t, ethservice, api, taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList))
	xEnvelope, xHash := fcuTestGetPayloadV5(t, api, x)
	xPayload := fcuTestLastPayload(api)
	z := fcuTestBuild(t, ethservice, api, taikoFCUTestAttrs(config, fcuTestEtnaTarget+1, fcuTestEmptyTxList))
	_, zHash := fcuTestGetPayloadV5(t, api, z)
	if origin, _ := rawdb.ReadL1Origin(db, big.NewInt(1)); origin == nil || origin.L2BlockHash != zHash {
		t.Fatalf("L1 origin = %+v, want block %v", origin, zHash)
	}

	if again := fcuTestBuild(t, ethservice, api, taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList)); again != x {
		t.Fatalf("rebuilt payload id = %v, want %v", again, x)
	}
	if rebuilt := fcuTestLastPayload(api); rebuilt == nil || rebuilt == xPayload {
		t.Fatalf("payload %v was not built again", x)
	}
	envelope, hash := fcuTestGetPayloadV5(t, api, x)
	if hash != xHash || envelope != xEnvelope {
		t.Fatalf("rebuilt payload:\nhave %s\nwant %s", envelope, xEnvelope)
	}
	fcuTestUnknownPayload(t, api, z)
	if origin, _ := rawdb.ReadL1Origin(db, big.NewInt(1)); origin == nil || origin.L2BlockHash != xHash {
		t.Fatalf("L1 origin = %+v, want the rebuilt block %v", origin, xHash)
	}
}

// TestTaikoForkchoiceUpdatedV3BuildFailureKeepsTheLastPayload checks that a
// failed build leaves the last built payload and its L1 origin in place.
func TestTaikoForkchoiceUpdatedV3BuildFailureKeepsTheLastPayload(t *testing.T) {
	ethservice, api := startTaikoFCUTestService(t)
	config := ethservice.BlockChain().Config()
	genesis := ethservice.BlockChain().Genesis().Hash()

	x := fcuTestBuild(t, ethservice, api, taikoFCUTestAttrs(config, fcuTestEtnaTarget, fcuTestEmptyTxList))
	want, xHash := fcuTestGetPayloadV5(t, api, x)

	// Before Etna an L2 block needs its anchor transaction, so an empty list
	// cannot be sealed.
	failing := taikoFCUTestAttrs(config, fcuTestPreEtnaTime, fcuTestEmptyTxList)
	_, err := api.ForkchoiceUpdatedV3(context.Background(), engine.ForkchoiceStateV1{HeadBlockHash: genesis}, failing)
	if err == nil || rpcErrorCode(t, err) != -32603 {
		t.Fatalf("failing build: err %v, want code -32603", err)
	}
	fcuTestUnknownPayload(t, api, taikoPayloadID(genesis, &failing.PayloadAttributes))
	if have, _ := fcuTestGetPayloadV5(t, api, x); have != want {
		t.Fatalf("payload after a failed build:\nhave %s\nwant %s", have, want)
	}
	if origin, _ := rawdb.ReadL1Origin(ethservice.ChainDb(), big.NewInt(1)); origin == nil || origin.L2BlockHash != xHash {
		t.Fatalf("L1 origin = %+v, want block %v", origin, xHash)
	}
}
