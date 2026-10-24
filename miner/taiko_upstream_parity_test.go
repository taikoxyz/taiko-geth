package miner

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/taiko"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

// The tests in this file are characterization tests. They pin the execution
// results Taiko blocks had on the go-ethereum v1.17.2 base, so that a later
// upstream merge can prove every block still executes bit-identically. Each
// block is built through sealBlockWith and then re-executed and validated by
// the regular import path (InsertChain), and only stable, black-box outputs
// are compared: header hashes and roots, gas, the Unzen zk gas carried in the
// difficulty, transaction counts, receipt statuses and state reads.
//
// The golden values were captured once on the v1.17.2 base with
//
//	TAIKO_PRINT_PARITY=1 go test ./miner -run TestTaikoUpstreamParity -count=1 -v
//
// and must never be regenerated on a later base: a mismatch means a block
// executes differently than it did before, which is the regression these
// tests exist to catch.

// parityGolden is the pinned outcome of one imported block.
type parityGolden struct {
	Hash, Root, ReceiptHash common.Hash
	GasUsed, Difficulty     uint64
	TxCount                 int
	Statuses                []uint64
}

// taikoParityGolden holds the outcome of every block of every scenario, keyed
// by scenario name, as captured on the v1.17.2 base.
var taikoParityGolden = map[string][]parityGolden{
	"G1-legacy": {
		{
			Hash:        common.HexToHash("0x428230a60d1f17997f43d46d17e6644ab17dac6fc6ffaab28af072ffa1b6870e"),
			Root:        common.HexToHash("0xde6f11bedde160b3eb97c164f18841352ed2f55b7ee3a904ff626f1d3915b1d7"),
			ReceiptHash: common.HexToHash("0x4625fba471605d39ff06f5b5fdebdd18ce07f47e7976585842b8e1e40a8e4984"),
			GasUsed:     143231,
			Difficulty:  0,
			TxCount:     4,
			Statuses:    []uint64{1, 1, 1, 1},
		},
	},
	"G1o-ontake": {
		{
			Hash:        common.HexToHash("0x8c0080deb1311b9b27d6caf3faee4c7ceddb58fd38ecb9dc5b4391c9a923bdfa"),
			Root:        common.HexToHash("0x18d812216ac136547e0e1acee2a2488af40afe75b6a0cf1a5936f656cc8e8fe9"),
			ReceiptHash: common.HexToHash("0x4625fba471605d39ff06f5b5fdebdd18ce07f47e7976585842b8e1e40a8e4984"),
			GasUsed:     143231,
			Difficulty:  0,
			TxCount:     4,
			Statuses:    []uint64{1, 1, 1, 1},
		},
	},
	"G1p-pacaya": {
		{
			Hash:        common.HexToHash("0x764a2fcbc26c4721f000d1db19aaa38f504595163183467104c91c85d425ba73"),
			Root:        common.HexToHash("0x18d812216ac136547e0e1acee2a2488af40afe75b6a0cf1a5936f656cc8e8fe9"),
			ReceiptHash: common.HexToHash("0x4625fba471605d39ff06f5b5fdebdd18ce07f47e7976585842b8e1e40a8e4984"),
			GasUsed:     143231,
			Difficulty:  0,
			TxCount:     4,
			Statuses:    []uint64{1, 1, 1, 1},
		},
	},
	"G2-shasta": {
		{
			Hash:        common.HexToHash("0x40e64365425b1b01bebc1308608dd707bd125c8283195ea8797951ab230ec8cb"),
			Root:        common.HexToHash("0x9f8854cfd5902b4ae226e50c75992335416c22d91e70845dedae2777a1e8d801"),
			ReceiptHash: common.HexToHash("0x4625fba471605d39ff06f5b5fdebdd18ce07f47e7976585842b8e1e40a8e4984"),
			GasUsed:     143231,
			Difficulty:  0,
			TxCount:     4,
			Statuses:    []uint64{1, 1, 1, 1},
		},
	},
	"G3-unzen": {
		{
			Hash:        common.HexToHash("0xe45fe92f464eca2ab581fef47855c8fad2d4d067ae27bea9bacb19ecb667e167"),
			Root:        common.HexToHash("0x0f3a88fc49af4409c1695699c81cf20f7fe2b537666d28c47f71b284d30e50b3"),
			ReceiptHash: common.HexToHash("0xdff7233ec3f6a9d78633a69709aad62be391e4857f873d7a2e82af4350ffe80d"),
			GasUsed:     622273,
			Difficulty:  4131482,
			TxCount:     7,
			Statuses:    []uint64{1, 1, 1, 1, 0, 1, 1},
		},
	},
	"G3n-unzen-no-syscontracts": {
		{
			Hash:        common.HexToHash("0x156919adac5273d59a396e028d29a341dc07a09ebec39a2e9cfd7bd1e17a22a5"),
			Root:        common.HexToHash("0x8c90a72b9567d43b243134cab849ebef5d54b089939c0ecddb50829f326dbaff"),
			ReceiptHash: common.HexToHash("0x7f5967ed929e86b813af03afe9c1274ccabf171771890af58f2b4b3e594bd647"),
			GasUsed:     42160,
			Difficulty:  486000,
			TxCount:     2,
			Statuses:    []uint64{1, 1},
		},
		{
			Hash:        common.HexToHash("0x4fe0d2583b88601c8efc6227b36ca2fa1e36af39594eb3ce6833a59aff42109a"),
			Root:        common.HexToHash("0x482761719bc4484087644d1000022f2957e7e5a62036fe16be297f10c4d148ce"),
			ReceiptHash: common.HexToHash("0x7f5967ed929e86b813af03afe9c1274ccabf171771890af58f2b4b3e594bd647"),
			GasUsed:     42160,
			Difficulty:  486000,
			TxCount:     2,
			Statuses:    []uint64{1, 1},
		},
		{
			Hash:        common.HexToHash("0xd37d81fe06c0df84e73bdc92cf7e1aaec2fdb95e157ccfe275e7c60d6a62493e"),
			Root:        common.HexToHash("0xd76d40d579137bfedafe05249ba874212c2481586e3400208ccf0fd105cd2227"),
			ReceiptHash: common.HexToHash("0x7f5967ed929e86b813af03afe9c1274ccabf171771890af58f2b4b3e594bd647"),
			GasUsed:     42160,
			Difficulty:  486000,
			TxCount:     2,
			Statuses:    []uint64{1, 1},
		},
	},
	"G4-unzen-truncate": {
		{
			Hash:        common.HexToHash("0xc864c88f12183e9b7e6d9a55b139544a00da760ad7ab2e5df96ad89e2f6e8186"),
			Root:        common.HexToHash("0x0cc65f441838722e2d636ccfb76ec0885bbb604d33a9705fcc3c8d8a25ec498b"),
			ReceiptHash: common.HexToHash("0x73bb336f5e7edc7725f46ee4ec982620410931bb8291b271794bdd3a469e7c19"),
			GasUsed:     158325,
			Difficulty:  99880304,
			TxCount:     2,
			Statuses:    []uint64{1, 1},
		},
	},
	"G4x-unzen-truncate-midtx": {
		{
			Hash:        common.HexToHash("0xe5458e07f4042b6a17356664a06d3024fa9c993da16ca80f355af96270419752"),
			Root:        common.HexToHash("0x83e0b13c2d541c7c00cc0824682f6e8a21d788a6dec0272561bf79899954facc"),
			ReceiptHash: common.HexToHash("0x7f5967ed929e86b813af03afe9c1274ccabf171771890af58f2b4b3e594bd647"),
			GasUsed:     42160,
			Difficulty:  486000,
			TxCount:     2,
			Statuses:    []uint64{1, 1},
		},
	},
}

var (
	// parityBankKey signs every user transaction. Unlike testBankKey it is
	// fixed, so transaction hashes, and with them every root, are stable.
	parityBankKey     = mustParityKey("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	parityBankAddress = crypto.PubkeyToAddress(parityBankKey.PublicKey)
	parityBankFunds   = new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether))

	// parityAuthorityKey signs the EIP-7702 authorization.
	parityAuthorityKey     = mustParityKey("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	parityAuthorityAddress = crypto.PubkeyToAddress(parityAuthorityKey.PublicKey)

	// parityCoinbase receives the fees. testUserAddress is generated per run,
	// so it cannot be the beneficiary of a pinned block.
	parityCoinbase          = common.HexToAddress("0x0000000000000000000000000000000000c0ffee")
	parityRecipient         = common.HexToAddress("0x000000000000000000000000000000000000beef")
	parityWithdrawalAddress = common.HexToAddress("0x0000000000000000000000000000000000007e57")

	parityGasTipCap = big.NewInt(params.GWei)
	parityGasFeeCap = big.NewInt(2 * params.GWei)

	// parityOntakeExtra carries basefee-sharing pctg 50 in the Ontake/Pacaya
	// layout, which DecodeOntakeExtraData reads as one big-endian integer.
	parityOntakeExtra = common.LeftPadBytes([]byte{50}, 32)
	// parityShastaExtra is the 7-byte Shasta layout: basefee-sharing pctg 50,
	// then proposal id 1 in six bytes.
	parityShastaExtra = []byte{0x32, 0, 0, 0, 0, 0, 1}
)

// Contracts installed in the Unzen genesis.
var (
	parityLoggerAddress           = common.HexToAddress("0x00000000000000000000000000000000000a0001")
	parityFactoryAddress          = common.HexToAddress("0x00000000000000000000000000000000000a0002")
	parityPrecompileCallerAddress = common.HexToAddress("0x00000000000000000000000000000000000a0003")
	parityStaticCallerAddress     = common.HexToAddress("0x00000000000000000000000000000000000a0004")
	parityCreatorAddress          = common.HexToAddress("0x00000000000000000000000000000000000a0005")
	parityZkHeavyAddress          = common.HexToAddress("0x00000000000000000000000000000000000a0006")

	// parityCollisionNonce is the bank nonce of the G3 deploy whose CREATE
	// address is pre-seeded with storage.
	parityCollisionNonce uint64 = 3
	parityCollisionSlot         = common.BigToHash(big.NewInt(1))
)

// parityLoggerRuntime stores 0x2a in slot 0 and emits a LOG1 with topic 1
// over a word holding 0xff, so a call to it writes both state and receipts.
var parityLoggerRuntime = []byte{
	byte(vm.PUSH1), 0x2a, byte(vm.PUSH1), 0x00, byte(vm.SSTORE),
	byte(vm.PUSH1), 0xff, byte(vm.PUSH1), 0x00, byte(vm.MSTORE),
	byte(vm.PUSH1), 0x01, byte(vm.PUSH1), 0x20, byte(vm.PUSH1), 0x00, byte(vm.LOG1),
	byte(vm.STOP),
}

// parityLoggerInitcode deploys parityLoggerRuntime.
var parityLoggerInitcode = append([]byte{
	byte(vm.PUSH1), byte(len(parityLoggerRuntime)), byte(vm.PUSH1), 0x0c, byte(vm.PUSH1), 0x00, byte(vm.CODECOPY),
	byte(vm.PUSH1), byte(len(parityLoggerRuntime)), byte(vm.PUSH1), 0x00, byte(vm.RETURN),
}, parityLoggerRuntime...)

// parityFactoryCode CREATE2s its calldata as initcode with salt 1 and records
// the created address in slot 0.
var parityFactoryCode = []byte{
	byte(vm.CALLDATASIZE), byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.CALLDATACOPY),
	byte(vm.PUSH1), 0x01, byte(vm.CALLDATASIZE), byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.CREATE2),
	byte(vm.PUSH1), 0x00, byte(vm.SSTORE),
	byte(vm.STOP),
}

// parityPrecompileCallerCode calls sha256 twice on the same word (the second
// call is the one a precompile result cache would serve), then p256verify and
// modexp on inputs taken from calldata, and stores the four results in slots
// 0 to 3.
var parityPrecompileCallerCode = concatParityCode(
	[]byte{byte(vm.PUSH1), 0x01, byte(vm.PUSH1), 0x00, byte(vm.MSTORE)},
	[]byte{byte(vm.CALLDATASIZE), byte(vm.PUSH1), 0x00, byte(vm.PUSH2), 0x01, 0x00, byte(vm.CALLDATACOPY)},
	parityStaticCall(common.BytesToAddress([]byte{0x02}), 0xffff, 0x00, 0x20, 0x20, 0x20), []byte{byte(vm.POP)},
	parityStaticCall(common.BytesToAddress([]byte{0x02}), 0xffff, 0x00, 0x20, 0x40, 0x20), []byte{byte(vm.POP)},
	parityStaticCall(common.BytesToAddress([]byte{0x01, 0x00}), 0xffff, 0x100, uint16(len(parityP256Input)), 0x60, 0x20), []byte{byte(vm.POP)},
	parityStaticCall(common.BytesToAddress([]byte{0x05}), 0xffff, 0x100+uint16(len(parityP256Input)), uint16(len(parityModexpInput)), 0x80, 0x01), []byte{byte(vm.POP)},
	[]byte{byte(vm.PUSH1), 0x20, byte(vm.MLOAD), byte(vm.PUSH1), 0x00, byte(vm.SSTORE)},
	[]byte{byte(vm.PUSH1), 0x40, byte(vm.MLOAD), byte(vm.PUSH1), 0x01, byte(vm.SSTORE)},
	[]byte{byte(vm.PUSH1), 0x60, byte(vm.MLOAD), byte(vm.PUSH1), 0x02, byte(vm.SSTORE)},
	[]byte{byte(vm.PUSH1), 0x80, byte(vm.MLOAD), byte(vm.PUSH1), 0x03, byte(vm.SSTORE)},
	[]byte{byte(vm.STOP)},
)

var (
	// parityP256Input is a valid secp256r1 signature, the first vector of
	// core/vm/testdata/precompiles/p256Verify.json, so p256verify returns 1.
	parityP256Input = common.FromHex("4cee90eb86eaa050036147a12d49004b6b9c72bd725d39d4785011fe190f0b4da73bd4903f0ce3b639bbbf6e8e80d16931ff4bcf5993d58468e8fb19086e8cac36dbcd03009df8c59286b162af3bd7fcc0450c9aa81be5d10d312af6c66b1d604aebd3099c618202fcfe16ae7770b0c49ab5eadf74b754204a3bb6060e44eff37618b065f9832de4ca6ca971a7a1adc826d0f7c00181a5fb2ddf79ae00b4e10e")
	// parityModexpInput computes 3^5 mod 7 = 5 with one-byte operands.
	parityModexpInput = concatParityCode(
		common.LeftPadBytes([]byte{1}, 32), common.LeftPadBytes([]byte{1}, 32), common.LeftPadBytes([]byte{1}, 32),
		[]byte{0x03, 0x05, 0x07},
	)
)

// parityStaticCallerCode STATICCALLs parityCreatorAddress, whose CREATE halts
// on write protection, and stores ISZERO(success) in slot 0.
var parityStaticCallerCode = concatParityCode(
	parityStaticCall(parityCreatorAddress, 0xffff, 0, 0, 0, 0),
	[]byte{byte(vm.ISZERO), byte(vm.PUSH1), 0x00, byte(vm.SSTORE), byte(vm.STOP)},
)

// parityCreatorCode runs CREATE with empty initcode.
var parityCreatorCode = []byte{
	byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.CREATE), byte(vm.STOP),
}

// parityZkHeavyCode STATICCALLs the point evaluation precompile, the entry
// with the largest multiplier in vm.UnzenZkGasSchedule (859), k times with an
// empty input, forwarding g gas each time; g is calldata word 0 and k word 1.
// The precompile rejects the input, and a failed precompile call is charged
// zk gas on the whole forwarded gas, so each iteration costs g*859 zk gas.
var parityZkHeavyCode = []byte{
	byte(vm.PUSH1), 0x20, byte(vm.CALLDATALOAD), // 0x00: k
	byte(vm.JUMPDEST),                                                    // 0x03: loop
	byte(vm.DUP1), byte(vm.ISZERO), byte(vm.PUSH1), 0x1f, byte(vm.JUMPI), // 0x04: done when k == 0
	byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, // 0x09: ret and args
	byte(vm.PUSH1), 0x0a, // 0x11: point evaluation
	byte(vm.PUSH1), 0x00, byte(vm.CALLDATALOAD), // 0x13: g
	byte(vm.STATICCALL), byte(vm.POP), // 0x16
	byte(vm.PUSH1), 0x01, byte(vm.SWAP1), byte(vm.SUB), // 0x18: k--
	byte(vm.PUSH1), 0x03, byte(vm.JUMP), // 0x1c
	byte(vm.JUMPDEST), byte(vm.STOP), // 0x1f
}

const (
	// parityZkHeavyGas and parityZkHeavyIterations size the G4 heavy
	// transaction so the block's zk gas lands within vm.TxIntrinsicZkGas of
	// vm.BlockZkGasLimit: the heavy transaction still fits, and the
	// intrinsic zk gas of the trailing transfer no longer does.
	parityZkHeavyGas        = 57_760
	parityZkHeavyIterations = 2

	// parityZkHeavyMidTxIterations makes the G4x heavy transaction loop far
	// past the block's zk gas budget, so it crosses vm.BlockZkGasLimit on a
	// precompile charge while its opcodes are executing.
	parityZkHeavyMidTxIterations = 4
)

func mustParityKey(hex string) *ecdsa.PrivateKey {
	key, err := crypto.HexToECDSA(hex)
	if err != nil {
		panic(err)
	}
	return key
}

func concatParityCode(parts ...[]byte) []byte {
	var code []byte
	for _, part := range parts {
		code = append(code, part...)
	}
	return code
}

// parityStaticCall assembles STATICCALL(gas, to, argsOffset, argsSize,
// retOffset, retSize), leaving the success flag on the stack.
func parityStaticCall(to common.Address, gas, argsOffset, argsSize, retOffset, retSize uint16) []byte {
	var code []byte
	for _, v := range []uint16{retSize, retOffset, argsSize, argsOffset} {
		code = append(code, byte(vm.PUSH2), byte(v>>8), byte(v))
	}
	code = append(code, byte(vm.PUSH20))
	code = append(code, to.Bytes()...)
	return append(code, byte(vm.PUSH2), byte(gas>>8), byte(gas), byte(vm.STATICCALL))
}

// parityTx signs a type-2 transaction from the parity bank.
func parityTx(config *params.ChainConfig, nonce uint64, to *common.Address, value int64, gas uint64, data []byte) *types.Transaction {
	return types.MustSignNewTx(parityBankKey, types.LatestSigner(config), &types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     nonce,
		GasTipCap: parityGasTipCap,
		GasFeeCap: parityGasFeeCap,
		Gas:       gas,
		To:        to,
		Value:     big.NewInt(value),
		Data:      data,
	})
}

func parityWithdrawals() types.Withdrawals {
	return types.Withdrawals{{Index: 0, Validator: 1, Address: parityWithdrawalAddress, Amount: 7}}
}

// Chain configs, one per Taiko era. Fork times are pinned explicitly so that
// upstream additions to the shared configs cannot change what is exercised.

func parityLegacyConfig() *params.ChainConfig {
	config := newUnzenTestChainConfig()
	config.OntakeBlock, config.PacayaBlock, config.ShastaTime = nil, nil, nil
	config.CancunTime, config.PragueTime, config.OsakaTime, config.UnzenTime = nil, nil, nil, nil
	return config
}

func parityOntakeConfig() *params.ChainConfig {
	config := parityLegacyConfig()
	config.OntakeBlock = common.Big0
	return config
}

func parityPacayaConfig() *params.ChainConfig {
	config := parityOntakeConfig()
	config.PacayaBlock = common.Big0
	return config
}

func parityShastaConfig() *params.ChainConfig {
	zero := uint64(0)
	config := parityPacayaConfig()
	config.ShastaTime = &zero
	return config
}

func parityUnzenConfig() *params.ChainConfig {
	zero := uint64(0)
	config := parityShastaConfig()
	config.CancunTime, config.PragueTime, config.OsakaTime, config.UnzenTime = &zero, &zero, &zero, &zero
	return config
}

// parityGenesis funds the bank and installs a STOP contract at the TaikoL2
// address, which the anchor transaction calls.
func parityGenesis(config *params.ChainConfig) *core.Genesis {
	return &core.Genesis{
		Config: config,
		Alloc: types.GenesisAlloc{
			parityBankAddress:                         {Balance: parityBankFunds},
			core.TaikoTreasuryAddress(config.ChainID): {Code: []byte{byte(vm.STOP)}, Balance: common.Big0},
		},
		Timestamp:  1000,
		GasLimit:   15_000_000,
		Difficulty: common.Big0,
		BaseFee:    big.NewInt(params.InitialBaseFee),
	}
}

// parityUnzenGenesis extends parityGenesis with the contracts the Unzen
// scenarios call and a storage-only account (nonce 0, no code, slot 1 = 1) at
// the CREATE address of the bank's parityCollisionNonce. The EIP-4788 and
// EIP-2935 system contracts are installed only when systemContracts is set;
// the mainnet and Hoodi genesis allocs carry neither.
func parityUnzenGenesis(config *params.ChainConfig, systemContracts bool) *core.Genesis {
	gspec := parityGenesis(config)
	for addr, code := range map[common.Address][]byte{
		parityLoggerAddress:           parityLoggerRuntime,
		parityFactoryAddress:          parityFactoryCode,
		parityPrecompileCallerAddress: parityPrecompileCallerCode,
		parityStaticCallerAddress:     parityStaticCallerCode,
		parityCreatorAddress:          parityCreatorCode,
		parityZkHeavyAddress:          parityZkHeavyCode,
	} {
		gspec.Alloc[addr] = types.Account{Code: code, Balance: common.Big0}
	}
	gspec.Alloc[crypto.CreateAddress(parityBankAddress, parityCollisionNonce)] = types.Account{
		Balance: common.Big0,
		Storage: map[common.Hash]common.Hash{parityCollisionSlot: common.BigToHash(big.NewInt(1))},
	}
	if systemContracts {
		gspec.Alloc[params.BeaconRootsAddress] = types.Account{Code: params.BeaconRootsCode, Balance: common.Big0}
		gspec.Alloc[params.HistoryStorageAddress] = types.Account{Code: params.HistoryStorageCode, Balance: common.Big0}
	}
	return gspec
}

// parityBlock describes one block of a scenario: the transactions that follow
// the anchor in the driver's tx list, and how many of them sealing must drop.
type parityBlock struct {
	extra       []byte
	withdrawals types.Withdrawals
	txs         []*types.Transaction
	dropped     int
}

type parityScenario struct {
	name           string
	config         *params.ChainConfig
	genesis        *core.Genesis
	anchorSelector []byte
	anchorGas      uint64
	blocks         []parityBlock
	check          func(t *testing.T, chain *core.BlockChain, blocks []*types.Block)
}

// parityLegacyTxs is a value transfer, a deploy of parityLoggerRuntime and a
// call into the deployed contract.
func parityLegacyTxs(config *params.ChainConfig) []*types.Transaction {
	deployed := crypto.CreateAddress(parityBankAddress, 1)
	return []*types.Transaction{
		parityTx(config, 0, &parityRecipient, 1000, params.TxGas, nil),
		parityTx(config, 1, nil, 0, 200_000, parityLoggerInitcode),
		parityTx(config, 2, &deployed, 0, 100_000, nil),
	}
}

// parityG3CollisionIndex is the block index of the G3 collision deploy, the
// anchor being index 0.
const parityG3CollisionIndex = 4

// parityUnzenTxs returns the G3 transactions, in block order after the anchor.
func parityUnzenTxs(config *params.ChainConfig) []*types.Transaction {
	auth, err := types.SignSetCode(parityAuthorityKey, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(config.ChainID),
		Address: parityLoggerAddress,
		Nonce:   0,
	})
	if err != nil {
		panic(err)
	}
	setCode := types.MustSignNewTx(parityBankKey, types.LatestSigner(config), &types.SetCodeTx{
		ChainID:   uint256.MustFromBig(config.ChainID),
		Nonce:     4,
		GasTipCap: uint256.MustFromBig(parityGasTipCap),
		GasFeeCap: uint256.MustFromBig(parityGasFeeCap),
		Gas:       200_000,
		To:        parityAuthorityAddress,
		Value:     new(uint256.Int),
		AuthList:  []types.SetCodeAuthorization{auth},
	})
	return []*types.Transaction{
		parityTx(config, 0, &parityRecipient, 1000, params.TxGas, nil),
		parityTx(config, 1, &parityFactoryAddress, 0, 300_000, parityLoggerInitcode),
		parityTx(config, 2, &parityPrecompileCallerAddress, 0, 300_000, concatParityCode(parityP256Input, parityModexpInput)),
		parityTx(config, parityCollisionNonce, nil, 0, 200_000, parityLoggerInitcode),
		setCode,
		parityTx(config, 5, &parityStaticCallerAddress, 0, 200_000, nil),
	}
}

// parityG4Txs returns the zk-gas-heavy transaction and the trailing transfer
// that no longer fits the block's zk gas budget.
func parityG4Txs(config *params.ChainConfig) []*types.Transaction {
	return []*types.Transaction{
		parityTx(config, 0, &parityZkHeavyAddress, 0, 500_000, parityZkHeavyCalldata(parityZkHeavyIterations)),
		parityTx(config, 1, &parityRecipient, 1000, params.TxGas, nil),
	}
}

// parityG4xTxs returns a transfer, a zk-gas-heavy transaction that crosses the
// block's zk gas budget during execution, and a trailing transfer.
func parityG4xTxs(config *params.ChainConfig) []*types.Transaction {
	return []*types.Transaction{
		parityTx(config, 0, &parityRecipient, 1000, params.TxGas, nil),
		parityTx(config, 1, &parityZkHeavyAddress, 0, 500_000, parityZkHeavyCalldata(parityZkHeavyMidTxIterations)),
		parityTx(config, 2, &parityRecipient, 1000, params.TxGas, nil),
	}
}

// parityZkHeavyCalldata asks parityZkHeavyCode for the given number of point
// evaluation calls, each forwarding parityZkHeavyGas.
func parityZkHeavyCalldata(iterations int64) []byte {
	return concatParityCode(
		common.LeftPadBytes(big.NewInt(parityZkHeavyGas).Bytes(), 32),
		common.LeftPadBytes(big.NewInt(iterations).Bytes(), 32),
	)
}

// parityUnzenScenario is the Unzen setup shared by the G3, G3n and G4
// scenarios and the sealing edge tests, without blocks.
func parityUnzenScenario(name string, systemContracts bool) *parityScenario {
	config := parityUnzenConfig()
	return &parityScenario{
		name:           name,
		config:         config,
		genesis:        parityUnzenGenesis(config, systemContracts),
		anchorSelector: taiko.AnchorV4Selector,
		anchorGas:      taiko.AnchorV3V4GasLimit,
		check:          func(*testing.T, *core.BlockChain, []*types.Block) {},
	}
}

func parityG3Scenario() *parityScenario {
	sc := parityUnzenScenario("G3-unzen", true)
	sc.blocks = []parityBlock{{extra: parityShastaExtra, withdrawals: parityWithdrawals(), txs: parityUnzenTxs(sc.config)}}
	sc.check = checkParityG3
	return sc
}

func parityG3nScenario() *parityScenario {
	sc := parityUnzenScenario("G3n-unzen-no-syscontracts", false)
	for i := range 3 {
		sc.blocks = append(sc.blocks, parityBlock{
			extra: parityShastaExtra,
			txs:   []*types.Transaction{parityTx(sc.config, uint64(i), &parityRecipient, 1000, params.TxGas, nil)},
		})
	}
	sc.check = func(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
		statedb := parityState(t, chain, blocks[len(blocks)-1])
		for _, addr := range []common.Address{params.BeaconRootsAddress, params.HistoryStorageAddress} {
			if statedb.Exist(addr) {
				t.Fatalf("system call into absent contract %v left an account behind", addr)
			}
		}
		if got := statedb.GetBalance(parityRecipient).Uint64(); got != 3000 {
			t.Fatalf("recipient balance = %d, want 3000 after three transfers", got)
		}
	}
	return sc
}

func parityG4Scenario() *parityScenario {
	sc := parityUnzenScenario("G4-unzen-truncate", true)
	sc.blocks = []parityBlock{{extra: parityShastaExtra, txs: parityG4Txs(sc.config), dropped: 1}}
	sc.check = func(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
		block := blocks[0]
		if got := len(block.Transactions()); got != 2 {
			t.Fatalf("G4 sealed %d transactions, want the anchor and the heavy transaction", got)
		}
		zkGas := block.Difficulty().Uint64()
		if zkGas > vm.BlockZkGasLimit || zkGas+vm.TxIntrinsicZkGas <= vm.BlockZkGasLimit {
			t.Fatalf("G4 block zk gas %d does not leave the trailing transfer out of the %d budget", zkGas, vm.BlockZkGasLimit)
		}
		if got := parityState(t, chain, block).GetBalance(parityRecipient); !got.IsZero() {
			t.Fatalf("truncated transfer was executed: recipient balance %v", got)
		}
	}
	return sc
}

// parityG4xScenario truncates inside the interpreter instead of at an
// intrinsic charge: the heavy transaction's intrinsic zk gas still fits, but
// its point evaluation loop crosses vm.BlockZkGasLimit while executing. Sealing
// drops it and the trailing transfer, and it must leave no state behind.
func parityG4xScenario() *parityScenario {
	sc := parityUnzenScenario("G4x-unzen-truncate-midtx", true)
	sc.blocks = []parityBlock{{extra: parityShastaExtra, txs: parityG4xTxs(sc.config), dropped: 2}}
	sc.check = func(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
		block, transfer := blocks[0], sc.blocks[0].txs[0]

		pointEvaluation := common.BytesToAddress([]byte{0x0a})
		perCall := uint64(parityZkHeavyGas) * uint64(vm.UnzenZkGasSchedule.PrecompileMultiplier(pointEvaluation))
		if loop := parityZkHeavyMidTxIterations * perCall; loop <= vm.BlockZkGasLimit {
			t.Fatalf("test is vacuous: the heavy loop charges %d zk gas, within the %d budget", loop, vm.BlockZkGasLimit)
		}
		if txs := block.Transactions(); len(txs) != 2 || txs[1].Hash() != transfer.Hash() {
			t.Fatalf("G4x sealed %d transactions, want exactly [anchor, transfer]", len(txs))
		}
		// The anchor calls a STOP contract and the transfer pays an EOA, so
		// each is charged only its intrinsic zk gas: none of the heavy
		// transaction's zk gas is in the header.
		zkGas := block.Difficulty().Uint64()
		if want := 2 * vm.TxIntrinsicZkGas; zkGas != want {
			t.Fatalf("G4x block zk gas = %d, want %d: the truncated heavy transaction was counted", zkGas, want)
		}
		if zkGas+vm.TxIntrinsicZkGas > vm.BlockZkGasLimit {
			t.Fatalf("the heavy transaction's intrinsic zk gas did not fit, so it was not truncated during execution")
		}

		// The heavy transaction's nonce increment and gas payment were rolled
		// back with it; only the leading transfer touched the bank.
		statedb := parityState(t, chain, block)
		if got := statedb.GetNonce(parityBankAddress); got != 1 {
			t.Fatalf("bank nonce = %d, want 1: the truncated heavy transaction left its nonce increment", got)
		}
		gasPrice := new(big.Int).Add(block.BaseFee(), transfer.EffectiveGasTipValue(block.BaseFee()))
		spent := new(big.Int).Add(big.NewInt(1000), new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(params.TxGas)))
		if got, want := statedb.GetBalance(parityBankAddress).ToBig(), new(big.Int).Sub(parityBankFunds, spent); got.Cmp(want) != 0 {
			t.Fatalf("bank balance = %v, want %v: the truncated heavy transaction was charged", got, want)
		}
		if got := statedb.GetBalance(parityRecipient).Uint64(); got != 1000 {
			t.Fatalf("recipient balance = %d, want 1000: the trailing transfer was executed", got)
		}
	}
	return sc
}

func parityScenarios() []*parityScenario {
	legacy, ontake, pacaya, shasta := parityLegacyConfig(), parityOntakeConfig(), parityPacayaConfig(), parityShastaConfig()
	return []*parityScenario{
		{
			name:           "G1-legacy",
			config:         legacy,
			genesis:        parityGenesis(legacy),
			anchorSelector: taiko.AnchorSelector,
			anchorGas:      taiko.AnchorGasLimit,
			blocks:         []parityBlock{{extra: []byte{}, withdrawals: parityWithdrawals(), txs: parityLegacyTxs(legacy)}},
			check: func(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
				statedb := parityState(t, chain, blocks[0])
				checkParityLegacyTxs(t, statedb)
				checkParityWithdrawal(t, statedb)
			},
		},
		{
			name:           "G1o-ontake",
			config:         ontake,
			genesis:        parityGenesis(ontake),
			anchorSelector: taiko.AnchorV2Selector,
			anchorGas:      taiko.AnchorGasLimit,
			blocks:         []parityBlock{{extra: parityOntakeExtra, txs: parityLegacyTxs(ontake)}},
			check: func(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
				checkParityLegacyTxs(t, parityState(t, chain, blocks[0]))
			},
		},
		{
			name:           "G1p-pacaya",
			config:         pacaya,
			genesis:        parityGenesis(pacaya),
			anchorSelector: taiko.AnchorV3Selector,
			anchorGas:      taiko.AnchorV3V4GasLimit,
			blocks:         []parityBlock{{extra: parityOntakeExtra, txs: parityLegacyTxs(pacaya)}},
			check: func(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
				checkParityLegacyTxs(t, parityState(t, chain, blocks[0]))
			},
		},
		{
			name:           "G2-shasta",
			config:         shasta,
			genesis:        parityGenesis(shasta),
			anchorSelector: taiko.AnchorV4Selector,
			anchorGas:      taiko.AnchorV3V4GasLimit,
			blocks:         []parityBlock{{extra: parityShastaExtra, txs: parityLegacyTxs(shasta)}},
			check: func(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
				checkParityLegacyTxs(t, parityState(t, chain, blocks[0]))
			},
		},
		parityG3Scenario(),
		parityG3nScenario(),
		parityG4Scenario(),
		parityG4xScenario(),
	}
}

// paritySeal holds what is needed to seal the block after the current head.
type paritySeal struct {
	parent          *types.Header
	timestamp       uint64
	parentBlockTime uint64
	baseFee         *big.Int
	anchor          *types.Transaction
}

// nextParitySeal prepares the block after the chain head the way the driver
// does: timestamp parent+1, EIP-4396 base fee from Shasta on (parentBlockTime
// is the parent's own block time, as header verification computes it), and an
// anchor signed by the golden touch key at fee cap == base fee.
func nextParitySeal(t *testing.T, chain *core.BlockChain, sc *parityScenario) paritySeal {
	t.Helper()
	s := paritySeal{parent: chain.CurrentBlock()}
	s.timestamp = s.parent.Time + 1
	if s.parent.Number.Sign() > 0 {
		grandparent := chain.GetHeader(s.parent.ParentHash, s.parent.Number.Uint64()-1)
		s.parentBlockTime = s.parent.Time - grandparent.Time
	}
	s.baseFee = big.NewInt(params.InitialBaseFee)
	if sc.config.IsShasta(s.timestamp) {
		s.baseFee = misc.CalcEIP4396BaseFee(sc.config, s.parent, s.parentBlockTime)
	}
	taikoL2Address := core.TaikoTreasuryAddress(sc.config.ChainID)
	s.anchor = types.MustSignNewTx(goldenTouchTestKey(t), types.LatestSigner(sc.config), &types.DynamicFeeTx{
		ChainID:   sc.config.ChainID,
		Nonce:     s.parent.Number.Uint64(),
		GasTipCap: common.Big0,
		GasFeeCap: s.baseFee,
		Gas:       sc.anchorGas,
		To:        &taikoL2Address,
		Data:      sc.anchorSelector,
	})
	return s
}

// seal hands txs to sealBlockWith as the driver's RLP tx list.
func (s paritySeal) seal(t *testing.T, w *Miner, txs types.Transactions, extra []byte, withdrawals types.Withdrawals) (*types.Block, error) {
	t.Helper()
	txList, err := rlp.EncodeToBytes(txs)
	if err != nil {
		t.Fatalf("encode tx list: %v", err)
	}
	return w.sealBlockWith(s.parent, s.timestamp, s.parentBlockTime, &engine.BlockMetadata{
		Beneficiary: parityCoinbase,
		GasLimit:    s.parent.GasLimit,
		Timestamp:   s.timestamp,
		TxList:      txList,
		ExtraData:   extra,
	}, s.baseFee, withdrawals)
}

func parityState(t *testing.T, chain *core.BlockChain, block *types.Block) *state.StateDB {
	t.Helper()
	statedb, err := chain.StateAt(block.Root())
	if err != nil {
		t.Fatalf("StateAt(%v): %v", block.Root(), err)
	}
	return statedb
}

// checkParityLegacyTxs proves the transfer, deploy and call of
// parityLegacyTxs all executed.
func checkParityLegacyTxs(t *testing.T, statedb *state.StateDB) {
	t.Helper()
	if got := statedb.GetBalance(parityRecipient).Uint64(); got != 1000 {
		t.Fatalf("recipient balance = %d, want 1000", got)
	}
	deployed := crypto.CreateAddress(parityBankAddress, 1)
	if got := statedb.GetCode(deployed); !reflect.DeepEqual(got, parityLoggerRuntime) {
		t.Fatalf("deployed code = %x, want %x", got, parityLoggerRuntime)
	}
	if got := statedb.GetState(deployed, common.Hash{}); got != common.BigToHash(big.NewInt(0x2a)) {
		t.Fatalf("call into the deployed contract did not run: slot 0 = %v", got)
	}
}

// checkParityWithdrawal asserts the withdrawal was credited once, in raw wei.
func checkParityWithdrawal(t *testing.T, statedb *state.StateDB) {
	t.Helper()
	if got := statedb.GetBalance(parityWithdrawalAddress); got.Uint64() != 7 {
		t.Fatalf("withdrawal balance = %v, want 7 wei", got)
	}
}

func checkParityG3(t *testing.T, chain *core.BlockChain, blocks []*types.Block) {
	block := blocks[0]
	statedb := parityState(t, chain, block)
	checkParityWithdrawal(t, statedb)

	receipts := chain.GetReceiptsByHash(block.Hash())
	if receipts[parityG3CollisionIndex].Status != types.ReceiptStatusFailed {
		t.Fatalf("deploy onto the storage-only account succeeded; EIP-7610 collision check did not fire")
	}
	collision := crypto.CreateAddress(parityBankAddress, parityCollisionNonce)
	if code := statedb.GetCode(collision); len(code) != 0 {
		t.Fatalf("collision deploy installed code %x", code)
	}
	if got := statedb.GetState(collision, parityCollisionSlot); got != common.BigToHash(big.NewInt(1)) {
		t.Fatalf("storage-only account lost its slot: %v", got)
	}

	created := crypto.CreateAddress2(parityFactoryAddress, common.BigToHash(big.NewInt(1)), crypto.Keccak256(parityLoggerInitcode))
	if got := statedb.GetState(parityFactoryAddress, common.Hash{}); got != common.BytesToHash(created.Bytes()) {
		t.Fatalf("factory recorded %v, want CREATE2 address %v", got, created)
	}
	if got := statedb.GetCode(created); !reflect.DeepEqual(got, parityLoggerRuntime) {
		t.Fatalf("CREATE2 deployed code %x, want %x", got, parityLoggerRuntime)
	}

	digest := sha256.Sum256(common.LeftPadBytes([]byte{1}, 32))
	for slot, want := range []common.Hash{digest, digest, common.BigToHash(big.NewInt(1)), {0: 0x05}} {
		if got := statedb.GetState(parityPrecompileCallerAddress, common.BigToHash(big.NewInt(int64(slot)))); got != want {
			t.Fatalf("precompile result in slot %d = %v, want %v", slot, got, want)
		}
	}

	if got := statedb.GetCode(parityAuthorityAddress); !reflect.DeepEqual(got, types.AddressToDelegation(parityLoggerAddress)) {
		t.Fatalf("authority code = %x, want a delegation to %v", got, parityLoggerAddress)
	}
	if got := statedb.GetState(parityAuthorityAddress, common.Hash{}); got != common.BigToHash(big.NewInt(0x2a)) {
		t.Fatalf("delegated code did not run in the authority: slot 0 = %v", got)
	}

	if got := statedb.GetState(parityStaticCallerAddress, common.Hash{}); got != common.BigToHash(big.NewInt(1)) {
		t.Fatalf("CREATE under STATICCALL did not halt: recorded %v", got)
	}

	// The EIP-4788 and EIP-2935 system calls both wrote.
	if got := statedb.GetState(params.BeaconRootsAddress, beaconRootsSlot(block.Time())); got != common.BigToHash(new(big.Int).SetUint64(block.Time())) {
		t.Fatalf("EIP-4788 slot = %v, want the block timestamp", got)
	}
	historySlot := common.BigToHash(new(big.Int).SetUint64((block.NumberU64() - 1) % params.HistoryServeWindow))
	if got := statedb.GetState(params.HistoryStorageAddress, historySlot); got != block.ParentHash() {
		t.Fatalf("EIP-2935 slot = %v, want the parent hash %v", got, block.ParentHash())
	}
}

func observeParity(block *types.Block, receipts types.Receipts) parityGolden {
	statuses := make([]uint64, len(receipts))
	for i, receipt := range receipts {
		statuses[i] = receipt.Status
	}
	return parityGolden{
		Hash:        block.Hash(),
		Root:        block.Root(),
		ReceiptHash: block.ReceiptHash(),
		GasUsed:     block.GasUsed(),
		Difficulty:  block.Difficulty().Uint64(),
		TxCount:     len(block.Transactions()),
		Statuses:    statuses,
	}
}

func formatParityGolden(order []string, observed map[string][]parityGolden) string {
	var b strings.Builder
	b.WriteString("var taikoParityGolden = map[string][]parityGolden{\n")
	for _, name := range order {
		fmt.Fprintf(&b, "\t%q: {\n", name)
		for _, g := range observed[name] {
			statuses := make([]string, len(g.Statuses))
			for i, status := range g.Statuses {
				statuses[i] = fmt.Sprint(status)
			}
			fmt.Fprintf(&b, "\t\t{\n")
			fmt.Fprintf(&b, "\t\t\tHash:        common.HexToHash(%q),\n", g.Hash.Hex())
			fmt.Fprintf(&b, "\t\t\tRoot:        common.HexToHash(%q),\n", g.Root.Hex())
			fmt.Fprintf(&b, "\t\t\tReceiptHash: common.HexToHash(%q),\n", g.ReceiptHash.Hex())
			fmt.Fprintf(&b, "\t\t\tGasUsed:     %d,\n", g.GasUsed)
			fmt.Fprintf(&b, "\t\t\tDifficulty:  %d,\n", g.Difficulty)
			fmt.Fprintf(&b, "\t\t\tTxCount:     %d,\n", g.TxCount)
			fmt.Fprintf(&b, "\t\t\tStatuses:    []uint64{%s},\n", strings.Join(statuses, ", "))
			fmt.Fprintf(&b, "\t\t},\n")
		}
		fmt.Fprintf(&b, "\t},\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// TestTaikoUpstreamParity seals every scenario block through sealBlockWith,
// imports it through InsertChain, and compares the outcome with the values
// pinned on the v1.17.2 base. With TAIKO_PRINT_PARITY=1 it prints the
// observed values as a Go literal instead of comparing them.
func TestTaikoUpstreamParity(t *testing.T) {
	printMode := os.Getenv("TAIKO_PRINT_PARITY") == "1"
	var (
		order    []string
		observed = make(map[string][]parityGolden)
	)
	for _, sc := range parityScenarios() {
		order = append(order, sc.name)
		t.Run(sc.name, func(t *testing.T) {
			w, b := newUnzenTestWorker(t, sc.genesis)
			var blocks []*types.Block
			for i, spec := range sc.blocks {
				s := nextParitySeal(t, b.chain, sc)
				block, err := s.seal(t, w, append(types.Transactions{s.anchor}, spec.txs...), spec.extra, spec.withdrawals)
				if err != nil {
					t.Fatalf("block %d: sealBlockWith: %v", i+1, err)
				}
				if got, want := len(block.Transactions()), 1+len(spec.txs)-spec.dropped; got != want {
					t.Fatalf("block %d: sealed %d transactions, want %d: a listed transaction was skipped", i+1, got, want)
				}
				if n, err := b.chain.InsertChain(types.Blocks{block}); err != nil {
					t.Fatalf("block %d: importing the sealed block failed (inserted %d): %v", i+1, n, err)
				}
				got := observeParity(block, b.chain.GetReceiptsByHash(block.Hash()))
				observed[sc.name] = append(observed[sc.name], got)
				blocks = append(blocks, block)

				if printMode {
					continue
				}
				golden := taikoParityGolden[sc.name]
				if len(golden) != len(sc.blocks) {
					t.Fatalf("%d golden blocks pinned for %s, scenario has %d", len(golden), sc.name, len(sc.blocks))
				}
				if want := golden[i]; !reflect.DeepEqual(got, want) {
					t.Fatalf("block %d executes differently than on the v1.17.2 base:\n have %+v\n want %+v", i+1, got, want)
				}
			}
			sc.check(t, b.chain, blocks)
		})
	}
	if printMode {
		fmt.Print(formatParityGolden(order, observed))
	}
}

// TestSealBlockWith_SkipsOversizedSignatureTx pins that a transaction whose
// signature V does not fit 256 bits is dropped at sender recovery while the
// rest of the tx list seals and imports.
func TestSealBlockWith_SkipsOversizedSignatureTx(t *testing.T) {
	sc := parityUnzenScenario("oversized-signature", true)
	w, b := newUnzenTestWorker(t, sc.genesis)
	s := nextParitySeal(t, b.chain, sc)
	oversized := parityOversizedSignatureTx(sc.config)
	transfer := parityTx(sc.config, 0, &parityRecipient, 1000, params.TxGas, nil)

	block, err := s.seal(t, w, types.Transactions{s.anchor, oversized, transfer}, parityShastaExtra, nil)
	if err != nil {
		t.Fatalf("sealBlockWith: %v", err)
	}
	txs := block.Transactions()
	if len(txs) != 2 || txs[0].Hash() != s.anchor.Hash() || txs[1].Hash() != transfer.Hash() {
		t.Fatalf("sealed %d transactions, want exactly [anchor, transfer]", len(txs))
	}
	if n, err := b.chain.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("importing the sealed block failed (inserted %d): %v", n, err)
	}
}

// TestSealBlockWith_OversizedSignatureAtAnchorPosition pins that an oversized
// signature in the first slot takes the anchor's place and is skipped, so the
// block has no anchor and sealing fails.
func TestSealBlockWith_OversizedSignatureAtAnchorPosition(t *testing.T) {
	sc := parityUnzenScenario("oversized-signature-at-anchor", true)
	w, b := newUnzenTestWorker(t, sc.genesis)
	s := nextParitySeal(t, b.chain, sc)
	oversized := parityOversizedSignatureTx(sc.config)
	transfer := parityTx(sc.config, 0, &parityRecipient, 1000, params.TxGas, nil)

	block, err := s.seal(t, w, types.Transactions{oversized, s.anchor, transfer}, parityShastaExtra, nil)
	if !errors.Is(err, taiko.ErrAnchorTxNotFound) {
		t.Fatalf("sealBlockWith = (%v, %v), want %v", block, err, taiko.ErrAnchorTxNotFound)
	}
}

// parityOversizedSignatureTx builds a type-2 transaction with V = 2^300.
// Encoding does not validate signature values.
func parityOversizedSignatureTx(config *params.ChainConfig) *types.Transaction {
	return types.NewTx(&types.DynamicFeeTx{
		ChainID:   config.ChainID,
		Nonce:     0,
		GasTipCap: parityGasTipCap,
		GasFeeCap: parityGasFeeCap,
		Gas:       params.TxGas,
		To:        &parityRecipient,
		Value:     common.Big0,
		V:         new(big.Int).Lsh(big.NewInt(1), 300),
		R:         big.NewInt(1),
		S:         big.NewInt(1),
	})
}

// TestImport_UnzenBodyPastTruncationRejected re-imports a truncated block with
// the first dropped transaction appended back to its body. Import must reject
// it, and must return instead of hanging, whether the re-added transaction
// crosses the zk gas budget at its intrinsic charge (G4's trailing transfer)
// or while executing (G4x's heavy transaction).
func TestImport_UnzenBodyPastTruncationRejected(t *testing.T) {
	for _, tt := range []struct {
		sc    *parityScenario
		readd int // index, among the block's listed transactions, of the one re-added
	}{
		{parityG4Scenario(), 1},
		{parityG4xScenario(), 1},
	} {
		t.Run(tt.sc.name, func(t *testing.T) {
			checkParityBodyPastTruncationRejected(t, tt.sc, tt.sc.blocks[0].txs[tt.readd])
		})
	}
}

// checkParityBodyPastTruncationRejected seals the scenario's single block,
// imports it on a fresh node with readd appended to the body, and requires a
// prompt rejection; the block as sealed must then import on that node.
func checkParityBodyPastTruncationRejected(t *testing.T, sc *parityScenario, readd *types.Transaction) {
	t.Helper()
	w, b := newUnzenTestWorker(t, sc.genesis)
	s := nextParitySeal(t, b.chain, sc)
	spec := sc.blocks[0]
	block, err := s.seal(t, w, append(types.Transactions{s.anchor}, spec.txs...), spec.extra, spec.withdrawals)
	if err != nil {
		t.Fatalf("sealBlockWith: %v", err)
	}
	if got, want := len(block.Transactions()), 1+len(spec.txs)-spec.dropped; got != want || spec.dropped == 0 {
		t.Fatalf("sealed %d transactions, want %d: the block was not truncated", got, want)
	}
	for _, tx := range block.Transactions() {
		if tx.Hash() == readd.Hash() {
			t.Fatalf("transaction %v to re-add was not dropped by sealing", readd.Hash())
		}
	}

	body := append(types.Transactions{}, block.Transactions()...)
	body = append(body, readd)
	header := block.Header()
	header.TxHash = types.DeriveSha(body, trie.NewStackTrie(nil))
	tampered := types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: body, Withdrawals: block.Withdrawals()})

	// Import on a separate node, which is not stopped if the import hangs.
	db := rawdb.NewMemoryDatabase()
	importer, err := core.NewBlockChain(db, sc.genesis, taiko.New(sc.genesis.Config, db), &core.BlockChainConfig{ArchiveMode: true})
	if err != nil {
		t.Fatalf("core.NewBlockChain failed: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := importer.InsertChain(types.Blocks{tampered})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("block whose body extends past the zk gas truncation point was imported")
		}
		if !strings.Contains(err.Error(), "extends past zk gas truncation point") {
			t.Fatalf("import rejected the block for another reason: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("importing a block whose body extends past the zk gas truncation point did not return")
	}
	defer importer.Stop()

	// The block as sealed imports on the same node.
	if n, err := importer.InsertChain(types.Blocks{block}); err != nil {
		t.Fatalf("importing the sealed block failed (inserted %d): %v", n, err)
	}
}
