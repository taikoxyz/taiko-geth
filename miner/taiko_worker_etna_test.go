package miner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
)

var (
	// etnaTestRoot stands for the parent beacon root of an Etna block: the
	// state root of the L1 block at the final anchorBlockNumber.
	etnaTestRoot = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000e7")
	// etnaTestExtraData is a 13-byte Etna extraData: a 25% base fee share,
	// proposal ID 1 and anchor block number 9.
	etnaTestExtraData = []byte{25, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 9}
	// preEtnaTestExtraData is a 7-byte Shasta extraData: a 25% base fee share
	// and proposal ID 1.
	preEtnaTestExtraData = []byte{25, 0, 0, 0, 0, 0, 1}
	// etnaTestHasher hashes 64 KiB per loop iteration until the Unzen zk gas
	// budget runs out: JUMPDEST; PUSH3 0x010000; PUSH1 0; KECCAK256; POP;
	// PUSH1 0; JUMP.
	etnaTestHasher     = common.HexToAddress("0x0000000000000000000000000000000000000022")
	etnaTestHasherCode = common.FromHex("0x5b6201000060002050600056")
)

// newEtnaTestChainConfig returns the Unzen test chain config with Shasta and
// Etna active from genesis.
func newEtnaTestChainConfig() *params.ChainConfig {
	config := newUnzenTestChainConfig()
	zero := uint64(0)
	config.ShastaTime = &zero
	config.EtnaTime = &zero
	return config
}

// newPreEtnaTestChainConfig returns the Etna test chain config with Etna
// unscheduled.
func newPreEtnaTestChainConfig() *params.ChainConfig {
	config := newEtnaTestChainConfig()
	config.EtnaTime = nil
	return config
}

// newEtnaTestGenesis extends the Unzen test genesis with the zk gas hasher and a
// funded golden touch account.
func newEtnaTestGenesis(config *params.ChainConfig) *core.Genesis {
	gspec := newUnzenTestGenesis(config)
	gspec.Alloc[etnaTestHasher] = types.Account{Code: etnaTestHasherCode, Balance: common.Big0}
	gspec.Alloc[taiko.GoldenTouchAccount] = types.Account{Balance: testBankFunds}
	return gspec
}

// etnaTestAttributes returns Etna payload attributes for the block after
// parent. prevRandao and the metadata mix hash differ on purpose, and the base
// fee is the one block 1 must carry.
func etnaTestAttributes(parent *types.Header, txList []byte) *engine.PayloadAttributes {
	timestamp := parent.Time + 1
	root := etnaTestRoot
	return &engine.PayloadAttributes{
		Timestamp:             timestamp,
		Random:                common.HexToHash("0x0a"),
		SuggestedFeeRecipient: testUserAddress,
		Withdrawals:           []*types.Withdrawal{},
		BeaconRoot:            &root,
		BaseFeePerGas:         new(big.Int).SetUint64(params.ShastaInitialBaseFee),
		BlockMetadata: &engine.BlockMetadata{
			Beneficiary: testUserAddress,
			GasLimit:    30_000_000,
			Timestamp:   timestamp,
			MixHash:     common.HexToHash("0x0b"),
			TxList:      txList,
			ExtraData:   bytes.Clone(etnaTestExtraData),
		},
		L1Origin: &rawdb.L1Origin{BlockID: big.NewInt(1)},
	}
}

// preEtnaTestAttributes returns the Etna test attributes in their pre-Etna
// shape: no parent beacon root and a 7-byte extraData.
func preEtnaTestAttributes(parent *types.Header, txList []byte) *engine.PayloadAttributes {
	attrs := etnaTestAttributes(parent, txList)
	attrs.BeaconRoot = nil
	attrs.BlockMetadata.ExtraData = bytes.Clone(preEtnaTestExtraData)
	return attrs
}

func encodeTestTxList(t *testing.T, txs ...*types.Transaction) []byte {
	t.Helper()
	enc, err := rlp.EncodeToBytes(types.Transactions(txs))
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	return enc
}

// bankTransfer signs a 1-wei transfer from the test bank.
func bankTransfer(t *testing.T, config *params.ChainConfig, nonce uint64) *types.Transaction {
	t.Helper()
	return types.MustSignNewTx(testBankKey, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(2 * params.InitialBaseFee),
		Gas:       params.TxGas,
		To:        &testUserAddress,
		Value:     common.Big1,
	})
}

// sealAndImportEtna seals a block from attrs on top of the current head and
// imports it through the regular chain insertion path, which re-executes it
// and checks every header commitment.
func sealAndImportEtna(t *testing.T, w *Miner, b *testWorkerBackend, attrs *engine.PayloadAttributes) *types.Block {
	t.Helper()
	block, err := w.sealBlockWith(b.chain.CurrentBlock(), 0, attrs)
	if err != nil {
		t.Fatalf("sealBlockWith: %v", err)
	}
	if n, err := b.chain.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("importing the sealed block failed (inserted %d): %v", n, err)
	}
	return block
}

func beaconRootStorageSlot(timestamp uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(timestamp%8191 + 8191))
}

// TestPrepareWork_EtnaRequiresNonZeroBeaconRoot pins that an Etna block is
// prepared with its non-zero parent beacon root, which the EIP-4788 call
// records while building, and that a missing or zero root fails.
func TestPrepareWork_EtnaRequiresNonZeroBeaconRoot(t *testing.T) {
	config := newEtnaTestChainConfig()
	w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))

	for _, root := range []*common.Hash{nil, new(common.Hash)} {
		genParams := unzenGenerateParams(b.chain.CurrentBlock())
		genParams.beaconRoot = root
		if env, err := w.prepareWork(context.Background(), genParams, false); err == nil {
			env.discard()
			t.Fatalf("expected root %v to be rejected for an Etna block", root)
		}
	}

	genParams := unzenGenerateParams(b.chain.CurrentBlock())
	root := etnaTestRoot
	genParams.beaconRoot = &root
	env, err := w.prepareWork(context.Background(), genParams, false)
	if err != nil {
		t.Fatalf("prepareWork: %v", err)
	}
	defer env.discard()
	if env.header.ParentBeaconRoot == nil || *env.header.ParentBeaconRoot != etnaTestRoot {
		t.Fatalf("parent beacon root = %v, want %v", env.header.ParentBeaconRoot, etnaTestRoot)
	}
	if got := env.state.GetState(params.BeaconRootsAddress, beaconRootStorageSlot(env.header.Time)); got != etnaTestRoot {
		t.Fatalf("EIP-4788 contract did not record the parent beacon root: slot = %v", got)
	}
}

// TestSealBlockWith_EtnaSealedBlockMatchesImport seals an Etna block and imports
// it, so every commitment the sealer produced is re-checked by execution.
func TestSealBlockWith_EtnaSealedBlockMatchesImport(t *testing.T) {
	config := newEtnaTestChainConfig()
	w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))

	tx := bankTransfer(t, config, 0)
	attrs := etnaTestAttributes(b.chain.CurrentBlock(), encodeTestTxList(t, tx))
	block := sealAndImportEtna(t, w, b, attrs)

	if txs := block.Transactions(); len(txs) != 1 || txs[0].Hash() != tx.Hash() {
		t.Fatalf("sealed block must contain exactly the transfer, got %d transactions", len(txs))
	}
	if root := block.BeaconRoot(); root == nil || *root != etnaTestRoot {
		t.Fatalf("parent beacon root = %v, want %v", root, etnaTestRoot)
	}
	if got := block.Difficulty().Uint64(); got != vm.TxIntrinsicZkGas {
		t.Fatalf("difficulty = %d, want the transfer's zk gas %d", got, vm.TxIntrinsicZkGas)
	}
	statedb, err := b.chain.StateAt(block.Root())
	if err != nil {
		t.Fatalf("StateAt: %v", err)
	}
	if got := statedb.GetState(params.BeaconRootsAddress, beaconRootStorageSlot(block.Time())); got != etnaTestRoot {
		t.Fatalf("imported state lacks the EIP-4788 root write: slot = %v", got)
	}
}

// TestSealBlockWith_EtnaHeaderFields pins where every Etna header field comes
// from, and that a contract reads the metadata gas limit through GASLIMIT and
// prevRandao through PREVRANDAO while the block is sealed. Importing the block
// re-executes it with the header values, so a build/import difference fails.
func TestSealBlockWith_EtnaHeaderFields(t *testing.T) {
	config := newEtnaTestChainConfig()
	gspec := newEtnaTestGenesis(config)
	// GASLIMIT; PUSH1 0; SSTORE; PREVRANDAO; PUSH1 1; SSTORE; STOP
	probe := common.HexToAddress("0x0000000000000000000000000000000000000044")
	gspec.Alloc[probe] = types.Account{Code: common.FromHex("0x456000554460015500"), Balance: common.Big0}
	w, b := newUnzenTestWorker(t, gspec)
	parent := b.chain.CurrentBlock()

	call := types.MustSignNewTx(testBankKey, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(2 * params.InitialBaseFee),
		Gas:       100_000,
		To:        &probe,
	})
	attrs := etnaTestAttributes(parent, encodeTestTxList(t, call))
	attrs.SuggestedFeeRecipient = common.HexToAddress("0x0000000000000000000000000000000000000055")
	// Withdrawals in the attributes are not credited: Etna blocks have none.
	withdrawalRecipient := common.HexToAddress("0x0000000000000000000000000000000000000066")
	attrs.Withdrawals = []*types.Withdrawal{{Index: 0, Validator: 1, Address: withdrawalRecipient, Amount: 1}}
	meta := attrs.BlockMetadata
	if target := core.CalcGasLimit(parent.GasLimit, testConfig.GasCeil); target == meta.GasLimit {
		t.Fatalf("the metadata gas limit must differ from the miner's own target %d", target)
	}

	block := sealAndImportEtna(t, w, b, attrs)
	header := block.Header()
	if txs := block.Transactions(); len(txs) != 1 || txs[0].Hash() != call.Hash() {
		t.Fatalf("sealed block must contain exactly the probe call, got %d transactions", len(txs))
	}
	if header.Time != meta.Timestamp {
		t.Fatalf("time = %d, want the metadata timestamp %d", header.Time, meta.Timestamp)
	}
	if header.Coinbase != meta.Beneficiary {
		t.Fatalf("coinbase = %v, want the metadata beneficiary %v", header.Coinbase, meta.Beneficiary)
	}
	if header.GasLimit != meta.GasLimit {
		t.Fatalf("gas limit = %d, want the metadata gas limit %d", header.GasLimit, meta.GasLimit)
	}
	if header.MixDigest != attrs.Random {
		t.Fatalf("mix digest = %v, want prevRandao %v", header.MixDigest, attrs.Random)
	}
	if header.BaseFee.Cmp(attrs.BaseFeePerGas) != 0 {
		t.Fatalf("base fee = %v, want %v", header.BaseFee, attrs.BaseFeePerGas)
	}
	if !bytes.Equal(header.Extra, meta.ExtraData) {
		t.Fatalf("extra = %x, want the metadata extraData %x", header.Extra, meta.ExtraData)
	}
	if header.ParentBeaconRoot == nil || *header.ParentBeaconRoot != etnaTestRoot {
		t.Fatalf("parent beacon root = %v, want %v", header.ParentBeaconRoot, etnaTestRoot)
	}
	if header.Difficulty.Sign() == 0 {
		t.Fatal("difficulty must carry the block's zk gas")
	}
	if header.WithdrawalsHash == nil || *header.WithdrawalsHash != types.EmptyWithdrawalsHash || len(block.Withdrawals()) != 0 {
		t.Fatalf("withdrawals = %d (root %v), want none", len(block.Withdrawals()), header.WithdrawalsHash)
	}
	statedb, err := b.chain.StateAt(block.Root())
	if err != nil {
		t.Fatalf("StateAt: %v", err)
	}
	if got, want := statedb.GetState(probe, common.Hash{}), common.BigToHash(new(big.Int).SetUint64(meta.GasLimit)); got != want {
		t.Fatalf("GASLIMIT stored %v, want the metadata gas limit %v", got, want)
	}
	if got := statedb.GetState(probe, common.BigToHash(common.Big1)); got != attrs.Random {
		t.Fatalf("PREVRANDAO stored %v, want prevRandao %v", got, attrs.Random)
	}
	if got := statedb.GetBalance(withdrawalRecipient); !got.IsZero() {
		t.Fatalf("withdrawal recipient balance = %v, want 0", got)
	}
}

// TestSealBlockWith_EtnaRejectsMismatchedMetadataTimestamp pins that an Etna
// block is never sealed from attributes whose two timestamps differ.
func TestSealBlockWith_EtnaRejectsMismatchedMetadataTimestamp(t *testing.T) {
	config := newEtnaTestChainConfig()
	w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))
	attrs := etnaTestAttributes(b.chain.CurrentBlock(), []byte{0xc0})
	attrs.BlockMetadata.Timestamp++
	if _, err := w.sealBlockWith(b.chain.CurrentBlock(), 0, attrs); err == nil {
		t.Fatal("sealed an Etna block from mismatched timestamps")
	}
}

// TestSealBlockWith_EtnaTakesRandomnessAndBaseFeeFromAttributes pins that an
// Etna block uses the payload attributes' prevRandao and base fee as given,
// instead of the metadata mix hash and an EIP-4396 recomputation.
func TestSealBlockWith_EtnaTakesRandomnessAndBaseFeeFromAttributes(t *testing.T) {
	config := newEtnaTestChainConfig()
	w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))

	attrs := etnaTestAttributes(b.chain.CurrentBlock(), []byte{0xc0})
	attrs.BaseFeePerGas = big.NewInt(7)
	block, err := w.sealBlockWith(b.chain.CurrentBlock(), 0, attrs)
	if err != nil {
		t.Fatalf("sealBlockWith: %v", err)
	}
	if block.MixDigest() != attrs.Random {
		t.Fatalf("mix digest = %v, want prevRandao %v", block.MixDigest(), attrs.Random)
	}
	if block.BaseFee().Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("base fee = %v, want 7", block.BaseFee())
	}
}

// TestSealBlockWith_EtnaTxListDecoding pins how an Etna block decodes its
// transaction list with the shared execution transaction grammar. Only the
// first RLP value is the list, and bytes after it are ignored. Etna blocks may
// be empty, and a list that does not decode, or that holds one transaction
// outside the grammar, seals an empty block instead of failing.
func TestSealBlockWith_EtnaTxListDecoding(t *testing.T) {
	config := newEtnaTestChainConfig()
	transfer := bankTransfer(t, config, 0)
	transferList := encodeTestTxList(t, transfer)

	type decodeCase struct {
		name   string
		txList []byte
		want   []*types.Transaction
	}
	cases := []decodeCase{
		{"empty list", []byte{0xc0}, nil},
		{"no bytes", []byte{}, nil},
		{"truncated list", []byte{0xc1}, nil},
		{"undecodable list", []byte{0x01}, nil},
		{"trailing zero byte", append(bytes.Clone(transferList), 0x00), []*types.Transaction{transfer}},
		{"trailing empty list", append(bytes.Clone(transferList), 0xc0), []*types.Transaction{transfer}},
		{"transfer and deposit type", encodeWithDepositTx(t, transfer), nil},
	}
	for _, v := range etnaGrammarViolations(config.ChainID) {
		cases = append(cases, decodeCase{"transfer and " + v.name, encodeTestTxList(t, transfer, v.tx), nil})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))
			block := sealAndImportEtna(t, w, b, etnaTestAttributes(b.chain.CurrentBlock(), tc.txList))
			txs := block.Transactions()
			if len(txs) != len(tc.want) {
				t.Fatalf("sealed %d transactions, want %d", len(txs), len(tc.want))
			}
			for i := range txs {
				if txs[i].Hash() != tc.want[i].Hash() {
					t.Fatalf("transaction %d = %v, want %v", i, txs[i].Hash(), tc.want[i].Hash())
				}
			}
			if len(txs) == 0 && block.Difficulty().Sign() != 0 {
				t.Fatalf("difficulty = %v, want 0", block.Difficulty())
			}
		})
	}
}

// TestSealBlockWith_PreEtnaTxListErrors pins that before Etna the whole input
// must decode as one RLP list, and that the list must not be empty, since it
// starts with the anchor transaction.
func TestSealBlockWith_PreEtnaTxListErrors(t *testing.T) {
	config := newPreEtnaTestChainConfig()
	transferList := encodeTestTxList(t, bankTransfer(t, config, 0))

	for _, tt := range []struct {
		name   string
		txList []byte
		want   string
	}{
		{"undecodable list", []byte{0x01}, "failed to decode txList"},
		{"trailing bytes", append(transferList, 0x00), "failed to decode txList"},
		{"empty list", []byte{0xc0}, "too less transactions in the block"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))
			attrs := preEtnaTestAttributes(b.chain.CurrentBlock(), tt.txList)
			if _, err := w.sealBlockWith(b.chain.CurrentBlock(), 0, attrs); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("sealBlockWith error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// TestSealBlockWith_GasLimitOpcode pins what GASLIMIT reads while sealing. From
// Etna on it is the metadata gas limit, as on import, rather than the gas limit
// the miner targets on its own. Before Etna the sealing EVM keeps the miner's
// target, a known build/import mismatch left unchanged, so a sealed block whose
// transaction stores GASLIMIT fails import.
func TestSealBlockWith_GasLimitOpcode(t *testing.T) {
	probe := common.HexToAddress("0x0000000000000000000000000000000000000033")
	newProbeWorker := func(t *testing.T, config *params.ChainConfig) (*Miner, *testWorkerBackend) {
		gspec := newEtnaTestGenesis(config)
		// GASLIMIT; PUSH1 0; SSTORE; STOP
		gspec.Alloc[probe] = types.Account{Code: common.FromHex("0x4560005500"), Balance: common.Big0}
		return newUnzenTestWorker(t, gspec)
	}
	probeCall := func(config *params.ChainConfig) *types.Transaction {
		return types.MustSignNewTx(testBankKey, types.LatestSigner(config), &types.DynamicFeeTx{
			ChainID:   config.ChainID,
			Nonce:     0,
			GasTipCap: big.NewInt(1),
			GasFeeCap: big.NewInt(2 * params.InitialBaseFee),
			Gas:       100_000,
			To:        &probe,
		})
	}

	t.Run("etna", func(t *testing.T) {
		config := newEtnaTestChainConfig()
		w, b := newProbeWorker(t, config)
		parent := b.chain.CurrentBlock()
		tx := probeCall(config)
		attrs := etnaTestAttributes(parent, encodeTestTxList(t, tx))
		gasLimit := attrs.BlockMetadata.GasLimit
		if target := core.CalcGasLimit(parent.GasLimit, testConfig.GasCeil); target == gasLimit {
			t.Fatalf("the metadata gas limit must differ from the miner's own target %d", target)
		}

		block := sealAndImportEtna(t, w, b, attrs)
		if txs := block.Transactions(); len(txs) != 1 || txs[0].Hash() != tx.Hash() {
			t.Fatalf("sealed block must contain exactly the probe call, got %d transactions", len(txs))
		}
		statedb, err := b.chain.StateAt(block.Root())
		if err != nil {
			t.Fatalf("StateAt: %v", err)
		}
		if got, want := statedb.GetState(probe, common.Hash{}), common.BigToHash(new(big.Int).SetUint64(gasLimit)); got != want {
			t.Fatalf("GASLIMIT stored %v, want the metadata gas limit %v", got, want)
		}
	})
	t.Run("before etna", func(t *testing.T) {
		config := newPreEtnaTestChainConfig()
		w, b := newProbeWorker(t, config)
		parent := b.chain.CurrentBlock()
		attrs := preEtnaTestAttributes(parent, nil)
		baseFee := misc.CalcEIP4396BaseFee(config, parent, 0)
		attrs.BlockMetadata.TxList = encodeTestTxList(t, anchorShapedTx(t, config, baseFee), probeCall(config))
		if target := core.CalcGasLimit(parent.GasLimit, testConfig.GasCeil); target == attrs.BlockMetadata.GasLimit {
			t.Fatalf("the metadata gas limit must differ from the miner's own target %d", target)
		}

		block, err := w.sealBlockWith(parent, 0, attrs)
		if err != nil {
			t.Fatalf("sealBlockWith: %v", err)
		}
		if n := len(block.Transactions()); n != 2 {
			t.Fatalf("sealed %d transactions, want the anchor and the probe call", n)
		}
		if _, err := b.chain.InsertChain(types.Blocks{block}); err == nil || !strings.Contains(err.Error(), "invalid merkle root") {
			t.Fatalf("import error = %v, want the known invalid merkle root", err)
		}
	})
}

// TestSealBlockWith_EtnaFirstPositionClassification pins how the Etna sealer
// classifies a transaction at index 0, which is an ordinary position: an
// invalid first transaction is skipped and the valid transfer after it is
// sealed, while one that exhausts the zk gas budget ends the block before
// itself, leaving it empty. Every sealed block is imported.
func TestSealBlockWith_EtnaFirstPositionClassification(t *testing.T) {
	config := newEtnaTestChainConfig()
	signer := types.LatestSigner(config)
	baseFee := new(big.Int).SetUint64(params.ShastaInitialBaseFee)
	feeCap := big.NewInt(2 * params.InitialBaseFee)
	unfundedKey, _ := crypto.HexToECDSA("0202020202020202020202020202020202020202020202020202020202020202")
	contractKey, _ := crypto.HexToECDSA("0303030303030303030303030303030303030303030303030303030303030303")
	dynamicFee := func(key *ecdsa.PrivateKey, edit func(*types.DynamicFeeTx)) *types.Transaction {
		inner := &types.DynamicFeeTx{
			ChainID:   config.ChainID,
			GasTipCap: big.NewInt(1),
			GasFeeCap: feeCap,
			Gas:       params.TxGas,
			To:        &testUserAddress,
			Value:     common.Big1,
		}
		edit(inner)
		return types.MustSignNewTx(key, signer, inner)
	}
	noEdit := func(*types.DynamicFeeTx) {}

	valid := bankTransfer(t, config, 0)
	unsigned, err := valid.WithSignature(signer, make([]byte, crypto.SignatureLength))
	if err != nil {
		t.Fatalf("strip signature: %v", err)
	}
	blob := types.MustSignNewTx(testBankKey, signer, &types.BlobTx{
		ChainID:    uint256.MustFromBig(config.ChainID),
		Nonce:      0,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.MustFromBig(feeCap),
		Gas:        params.TxGas,
		To:         testUserAddress,
		Value:      uint256.NewInt(0),
		BlobFeeCap: uint256.NewInt(1),
		BlobHashes: []common.Hash{{0x01}},
	})
	emptyAuthList := types.MustSignNewTx(testBankKey, signer, &types.SetCodeTx{
		ChainID:   uint256.MustFromBig(config.ChainID),
		Nonce:     0,
		GasTipCap: uint256.NewInt(1),
		GasFeeCap: uint256.MustFromBig(feeCap),
		Gas:       100_000,
		To:        testUserAddress,
		Value:     uint256.NewInt(0),
	})
	otherChain := types.MustSignNewTx(testBankKey, types.LatestSignerForChainID(big.NewInt(1)), &types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		GasTipCap: big.NewInt(1),
		GasFeeCap: feeCap,
		Gas:       params.TxGas,
		To:        &testUserAddress,
		Value:     common.Big1,
	})

	for _, tt := range []struct {
		name      string
		first     *types.Transaction
		committed []*types.Transaction
	}{
		{"valid", valid, []*types.Transaction{valid}},
		{"nonce too high", bankTransfer(t, config, 99), []*types.Transaction{valid}},
		{"unrecoverable signature", unsigned, []*types.Transaction{valid}},
		{"chain ID mismatch", otherChain, []*types.Transaction{valid}},
		{"blob transaction", blob, []*types.Transaction{valid}},
		{"fee cap below the base fee", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			tx.GasFeeCap = new(big.Int).Sub(baseFee, common.Big1)
		}), []*types.Transaction{valid}},
		{"tip above the fee cap", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			tx.GasTipCap = new(big.Int).Add(feeCap, common.Big1)
		}), []*types.Transaction{valid}},
		{"insufficient funds", dynamicFee(unfundedKey, noEdit), []*types.Transaction{valid}},
		{"sender with code", dynamicFee(contractKey, noEdit), []*types.Transaction{valid}},
		{"intrinsic gas too low", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			tx.Gas = params.TxGas - 1
		}), []*types.Transaction{valid}},
		{"gas below the calldata floor", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			// 100 non-zero bytes: intrinsic gas 22,600, floor 25,000.
			tx.Data = bytes.Repeat([]byte{0x01}, 100)
			tx.Gas = 24_000
		}), []*types.Transaction{valid}},
		{"init code above the size limit", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			tx.To = nil
			tx.Data = make([]byte, params.MaxInitCodeSize+1)
			tx.Gas = 1_000_000
		}), []*types.Transaction{valid}},
		{"empty authorization list", emptyAuthList, []*types.Transaction{valid}},
		{"gas above the transaction gas cap", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			tx.Gas = params.MaxTxGas + 1
		}), []*types.Transaction{valid}},
		{"gas above the block gas limit", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			tx.Gas = 10_000_001
		}), []*types.Transaction{valid}},
		{"zk gas exhaustion", dynamicFee(testBankKey, func(tx *types.DynamicFeeTx) {
			tx.To = &etnaTestHasher
			tx.Value = common.Big0
			tx.Gas = 5_000_000
		}), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gspec := newEtnaTestGenesis(config)
			gspec.Alloc[crypto.PubkeyToAddress(contractKey.PublicKey)] = types.Account{Code: []byte{0x00}, Balance: testBankFunds}
			w, b := newUnzenTestWorker(t, gspec)
			attrs := etnaTestAttributes(b.chain.CurrentBlock(), encodeTestTxList(t, tt.first, valid))
			attrs.BlockMetadata.GasLimit = 10_000_000
			block := sealAndImportEtna(t, w, b, attrs)
			txs := block.Transactions()
			if len(txs) != len(tt.committed) {
				t.Fatalf("committed %d transactions, want %d", len(txs), len(tt.committed))
			}
			for i := range txs {
				if txs[i].Hash() != tt.committed[i].Hash() {
					t.Fatalf("transaction %d = %v, want %v", i, txs[i].Hash(), tt.committed[i].Hash())
				}
			}
			if len(txs) == 0 && block.Difficulty().Sign() != 0 {
				t.Fatalf("difficulty = %v, want 0 for an empty block", block.Difficulty())
			}
		})
	}
}

// TestIsEtnaSkippedTxError pins which transaction errors the Etna sealer
// skips. Every other error aborts the build, including zk-gas exhaustion,
// which the sealer handles before classifying.
func TestIsEtnaSkippedTxError(t *testing.T) {
	for _, err := range []error{
		types.ErrInvalidSig,
		types.ErrInvalidChainId,
		core.ErrNonceTooLow,
		core.ErrNonceTooHigh,
		core.ErrNonceMax,
		core.ErrGasLimitReached,
		core.ErrGasLimitTooHigh,
		core.ErrInsufficientFunds,
		core.ErrInsufficientFundsForTransfer,
		core.ErrGasUintOverflow,
		core.ErrIntrinsicGas,
		core.ErrFloorDataGas,
		core.ErrTxTypeNotSupported,
		core.ErrTipAboveFeeCap,
		core.ErrTipVeryHigh,
		core.ErrFeeCapVeryHigh,
		core.ErrFeeCapTooLow,
		core.ErrSenderNoEOA,
		core.ErrBlobFeeCapTooLow,
		core.ErrMissingBlobHashes,
		core.ErrTooManyBlobs,
		core.ErrBlobTxCreate,
		core.ErrEmptyAuthList,
		core.ErrSetCodeTxCreate,
		vm.ErrMaxInitCodeSizeExceeded,
	} {
		if wrapped := fmt.Errorf("%w: address %v", err, testBankAddress); !isEtnaSkippedTxError(wrapped) {
			t.Errorf("%q aborts the build, want it skipped", wrapped)
		}
	}
	for _, err := range []error{
		vm.ErrZkGasLimitExceeded,
		core.ErrGasLimitOverflow,
		errors.New("missing trie node"),
	} {
		if isEtnaSkippedTxError(err) {
			t.Errorf("%q is skipped, want it to abort the build", err)
		}
	}
}

// anchorShapedTx signs a golden-touch transaction shaped like a Shasta anchor
// paying baseFee: the treasury call with the anchorV4 selector and gas limit.
func anchorShapedTx(t *testing.T, config *params.ChainConfig, baseFee *big.Int) *types.Transaction {
	t.Helper()
	treasury := core.TaikoTreasuryAddress(config.ChainID)
	return types.MustSignNewTx(goldenTouchTestKey(t), types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     0,
		GasTipCap: common.Big0,
		GasFeeCap: baseFee,
		Gas:       taiko.AnchorV3V4GasLimit,
		To:        &treasury,
		Data:      taiko.AnchorV4Selector,
	})
}

// TestSealBlockWith_EtnaGoldenTouchIsOrdinary pins that from Etna on a
// golden-touch transaction shaped like a legacy anchor gets no exemption: it
// buys gas and shares its base fee. Before Etna the same first transaction is
// the fee-exempt anchor.
func TestSealBlockWith_EtnaGoldenTouchIsOrdinary(t *testing.T) {
	t.Run("etna", func(t *testing.T) {
		config := newEtnaTestChainConfig()
		w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))
		attrs := etnaTestAttributes(b.chain.CurrentBlock(), nil)
		attrs.BlockMetadata.TxList = encodeTestTxList(t, anchorShapedTx(t, config, attrs.BaseFeePerGas))
		block := sealAndImportEtna(t, w, b, attrs)
		if len(block.Transactions()) != 1 {
			t.Fatalf("sealed %d transactions, want 1", len(block.Transactions()))
		}
		receipts := b.chain.GetReceiptsByHash(block.Hash())
		fee := new(big.Int).Mul(new(big.Int).SetUint64(receipts[0].GasUsed), attrs.BaseFeePerGas)
		statedb, err := b.chain.StateAt(block.Root())
		if err != nil {
			t.Fatalf("StateAt: %v", err)
		}
		if got, want := statedb.GetBalance(taiko.GoldenTouchAccount).ToBig(), new(big.Int).Sub(testBankFunds, fee); got.Cmp(want) != 0 {
			t.Fatalf("golden touch balance = %v, want %v", got, want)
		}
		// extraData[0] = 25: a quarter of the base fee goes to the coinbase.
		share := new(big.Int).Div(new(big.Int).Mul(fee, big.NewInt(25)), big.NewInt(100))
		if got := statedb.GetBalance(testUserAddress).ToBig(); got.Cmp(share) != 0 {
			t.Fatalf("coinbase balance = %v, want %v", got, share)
		}
	})
	t.Run("before etna", func(t *testing.T) {
		config := newPreEtnaTestChainConfig()
		w, b := newUnzenTestWorker(t, newEtnaTestGenesis(config))
		parent := b.chain.CurrentBlock()
		attrs := preEtnaTestAttributes(parent, nil)
		// Before Etna the sealer recomputes the base fee with EIP-4396, and the
		// anchor must pay exactly that base fee.
		baseFee := misc.CalcEIP4396BaseFee(config, parent, 0)
		attrs.BlockMetadata.TxList = encodeTestTxList(t, anchorShapedTx(t, config, baseFee))
		block := sealAndImportEtna(t, w, b, attrs)
		// Before Etna the mix digest is the metadata mix hash, not prevRandao.
		if attrs.BlockMetadata.MixHash == attrs.Random {
			t.Fatal("the metadata mix hash and prevRandao must differ")
		}
		if block.MixDigest() != attrs.BlockMetadata.MixHash {
			t.Fatalf("mix digest = %v, want the metadata mix hash %v", block.MixDigest(), attrs.BlockMetadata.MixHash)
		}
		statedb, err := b.chain.StateAt(block.Root())
		if err != nil {
			t.Fatalf("StateAt: %v", err)
		}
		if got := statedb.GetBalance(taiko.GoldenTouchAccount).ToBig(); got.Cmp(testBankFunds) != 0 {
			t.Fatalf("golden touch balance = %v, want the fee-exempt anchor to leave %v", got, testBankFunds)
		}
	})
}

// TestTaikoParentBeaconRoot pins the parent beacon root of a Taiko Unzen block
// on both sides of the Etna activation, for a nil, zero and non-zero
// caller-supplied root, with and without transaction-pool preselection.
func TestTaikoParentBeaconRoot(t *testing.T) {
	config := newEtnaTestChainConfig()
	etnaTime := uint64(100)
	config.EtnaTime = &etnaTime
	zero, root := common.Hash{}, etnaTestRoot

	for _, tt := range []struct {
		name         string
		time         uint64
		root         *common.Hash
		preselection bool
		want         *common.Hash // nil when an error is expected
	}{
		{"pre-etna nil", 99, nil, false, &zero},
		{"pre-etna nil preselection", 99, nil, true, &zero},
		{"pre-etna zero", 99, &zero, false, &zero},
		{"pre-etna zero preselection", 99, &zero, true, &zero},
		{"pre-etna non-zero", 99, &root, false, nil},
		{"pre-etna non-zero preselection", 99, &root, true, &root},
		{"etna nil", 100, nil, false, nil},
		{"etna nil preselection", 100, nil, true, nil},
		{"etna zero", 100, &zero, false, nil},
		{"etna zero preselection", 100, &zero, true, nil},
		{"etna non-zero", 100, &root, false, &root},
		{"etna non-zero preselection", 100, &root, true, &root},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := taikoParentBeaconRoot(config, tt.time, &generateParams{beaconRoot: tt.root, taikoPreselection: tt.preselection})
			if tt.want == nil {
				if err == nil {
					t.Fatalf("root = %v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("taikoParentBeaconRoot: %v", err)
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("root = %v, want %v", got, *tt.want)
			}
		})
	}
}
