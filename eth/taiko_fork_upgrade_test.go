package eth

import (
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

func TestShastaUpgradeUsesUpdatedChainConfigInConsensusEngine(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Simulate an old node database where ShastaTime is not present in chain config.
	oldGenesis := core.TaikoGenesisBlock(params.TaikoHoodiNetworkID.Uint64())
	oldCfg := *oldGenesis.Config
	oldCfg.ShastaTime = nil
	oldGenesis.Config = &oldCfg
	oldGenesis.MustCommit(db, triedb.NewDatabase(db, triedb.HashDefaults))

	// Simulate upgrade to a version that includes ShastaTime in Hoodi genesis config.
	newGenesis := core.TaikoGenesisBlock(params.TaikoHoodiNetworkID.Uint64())
	loadedCfg, _, err := core.LoadChainConfig(db, newGenesis)
	if err != nil {
		t.Fatalf("failed to load chain config: %v", err)
	}
	if loadedCfg.ShastaTime != nil {
		t.Fatalf("expected old loaded config to have nil ShastaTime, got %d", *loadedCfg.ShastaTime)
	}

	engine, err := ethconfig.CreateConsensusEngine(loadedCfg, db)
	if err != nil {
		t.Fatalf("failed to create consensus engine: %v", err)
	}
	chain, err := core.NewBlockChain(db, newGenesis, engine, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	if chain.Config().ShastaTime == nil {
		t.Fatal("expected blockchain config to contain ShastaTime after upgrade")
	}
	headerTime := *chain.Config().ShastaTime + 1

	taikoEngine, ok := engine.(*taiko.Taiko)
	if !ok {
		t.Fatalf("expected taiko engine, got %T", engine)
	}

	to := taikoL2Address(chain.Config().ChainID)
	key, err := crypto.HexToECDSA("92954368afd3caa1f3ce3ead0069c1af414054aefe1ef9aeacc1bf426222ce38")
	if err != nil {
		t.Fatalf("failed to decode test key: %v", err)
	}
	header := &types.Header{
		Number:  common.Big1,
		Time:    headerTime,
		BaseFee: big.NewInt(875_000_000),
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     0,
		GasTipCap: common.Big0,
		GasFeeCap: new(big.Int).Set(header.BaseFee),
		Gas:       taiko.AnchorV3V4GasLimit,
		To:        &to,
		Data:      taiko.AnchorV4Selector,
	})
	signer := types.MakeSigner(chain.Config(), header.Number, header.Time)
	signedTx, err := types.SignTx(tx, signer, key)
	if err != nil {
		t.Fatalf("failed to sign tx: %v", err)
	}

	isAnchor, err := taikoEngine.ValidateAnchorTx(signedTx, header)
	if err != nil {
		t.Fatalf("ValidateAnchorTx returned error: %v", err)
	}
	if !isAnchor {
		t.Fatal("expected AnchorV4 tx to be valid after Shasta activation")
	}
}

func TestUzenUpgradeUsesUpdatedChainConfigInConsensusEngine(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	oldGenesis := core.TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())
	oldCfg := *oldGenesis.Config
	oldCfg.UzenTime = nil
	oldGenesis.Config = &oldCfg
	oldGenesis.MustCommit(db, triedb.NewDatabase(db, triedb.HashDefaults))

	newGenesis := core.TaikoGenesisBlock(params.TaikoMainnetNetworkID.Uint64())
	loadedCfg, _, err := core.LoadChainConfig(db, newGenesis)
	if err != nil {
		t.Fatalf("failed to load chain config: %v", err)
	}
	if loadedCfg.UzenTime != nil {
		t.Fatalf("expected stored config to be missing UzenTime, got %d", *loadedCfg.UzenTime)
	}

	engine, err := ethconfig.CreateConsensusEngine(loadedCfg, db)
	if err != nil {
		t.Fatalf("failed to create consensus engine: %v", err)
	}
	chain, err := core.NewBlockChain(db, newGenesis, engine, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	if chain.Config().UzenTime == nil {
		t.Fatal("expected upgraded blockchain config to contain UzenTime")
	}
	taikoEngine, ok := engine.(*taiko.Taiko)
	if !ok {
		t.Fatalf("expected taiko engine, got %T", engine)
	}
	engineCfg := reflect.ValueOf(taikoEngine).Elem().FieldByName("chainConfig")
	if !engineCfg.IsValid() {
		t.Fatal("expected taiko engine to retain chainConfig field")
	}
	if engineCfg.IsNil() {
		t.Fatal("expected taiko engine chainConfig to be populated after upgrade")
	}
	if engineCfg.Pointer() != reflect.ValueOf(chain.Config()).Pointer() {
		t.Fatal("expected taiko engine to receive the refreshed blockchain chain config")
	}
	if engineCfg.Elem().FieldByName("UzenTime").IsNil() {
		t.Fatal("expected taiko engine chain config to contain UzenTime after upgrade")
	}
}

func taikoL2Address(chainID *big.Int) common.Address {
	prefix := strings.TrimPrefix(chainID.String(), "0")
	return common.HexToAddress(
		"0x" +
			prefix +
			strings.Repeat("0", common.AddressLength*2-len(prefix)-len(taiko.TaikoL2AddressSuffix)) +
			taiko.TaikoL2AddressSuffix,
	)
}
