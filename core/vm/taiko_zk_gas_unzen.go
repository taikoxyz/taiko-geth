package vm

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// CHANGE(taiko): zk-gas block limit on Devnet, Internal, Hoodi, and Mainnet during Unzen.
const BlockZkGasLimit uint64 = 100_000_000

// CHANGE(taiko): zk-gas block limit on the Taiko Masaya network during Unzen.
// Masaya runs Unzen with a 10× higher block budget than the other Taiko chains.
const MasayaBlockZkGasLimit uint64 = 1_000_000_000

// CHANGE(taiko): TxIntrinsicZkGas is the fixed per-tx intrinsic zk-gas charge
// applied on Devnet, Internal, Hoodi, and Mainnet during Unzen. Sourced from
// the zk-gas spec (taikoxyz/taiko-mono#21669); covers the proving cost of
// per-tx sender recovery.
const TxIntrinsicZkGas uint64 = 243_000

// CHANGE(taiko): MasayaTxIntrinsicZkGas is the per-tx intrinsic charge applied
// on the Taiko Masaya network during Unzen. Pinned at 0 because Masaya
// activated Unzen before the spec change landed; header difficulty on Unzen
// blocks encodes the finalized block zk gas, so changing the per-tx charge
// retroactively would break consensus on already-finalized Masaya blocks.
const MasayaTxIntrinsicZkGas uint64 = 0

// CHANGE(taiko): UnzenZkGasSchedule is the consensus zk-gas schedule used by
// Devnet, Internal, Hoodi, and Mainnet during the Unzen fork, with the
// recalibrated opcode and precompile multipliers.
var UnzenZkGasSchedule = unzenZkGasScheduleWith(
	BlockZkGasLimit,
	TxIntrinsicZkGas,
	unzenOpcodeMultipliers(),
	unzenPrecompileMultipliers(),
)

// CHANGE(taiko): MasayaUnzenZkGasSchedule is the consensus zk-gas schedule used
// by the Taiko Masaya network during the Unzen fork. Its opcode and precompile
// multipliers stay frozen at their pre-recalibration values to preserve
// consensus on Masaya's already-finalized Unzen blocks (header difficulty
// equals the finalized block zk gas); only the block budget and per-tx
// intrinsic charge differ from the default schedule, and spawn estimates are
// identical.
var MasayaUnzenZkGasSchedule = unzenZkGasScheduleWith(
	MasayaBlockZkGasLimit,
	MasayaTxIntrinsicZkGas,
	masayaUnzenOpcodeMultipliers(),
	masayaUnzenPrecompileMultipliers(),
)

// CHANGE(taiko): UnzenZkGasScheduleFor returns the Unzen zk-gas schedule for
// the given chain id. Taiko Masaya (167011) runs the 1B-budget frozen schedule;
// all other chains use the default 100M-budget recalibrated schedule.
func UnzenZkGasScheduleFor(chainID *big.Int) *ZkGasSchedule {
	if chainID != nil && chainID.Cmp(params.MasayaDevnetNetworkID) == 0 {
		return &MasayaUnzenZkGasSchedule
	}
	return &UnzenZkGasSchedule
}

// unzenZkGasScheduleWith builds an Unzen-shaped schedule from the requested
// block limit, per-tx intrinsic charge, and opcode/precompile multiplier
// tables. Spawn estimates are identical across all networks.
func unzenZkGasScheduleWith(blockLimit, txIntrinsicZkGas uint64, opcodeMultipliers [256]uint16, precompileMultipliers map[common.Address]uint16) ZkGasSchedule {
	return ZkGasSchedule{
		BlockLimit:            blockLimit,
		TxIntrinsicZkGas:      txIntrinsicZkGas,
		OpcodeMultipliers:     opcodeMultipliers,
		PrecompileMultipliers: precompileMultipliers,
		SpawnEstimates: SpawnEstimates{
			Call:         12500,
			CallCode:     12500,
			DelegateCall: 3500,
			StaticCall:   3500,
			Create:       37000,
			Create2:      44500,
		},
	}
}

// masayaUnzenOpcodeMultipliers returns the frozen Masaya opcode multiplier
// table, pinned at the pre-recalibration values. Recalibrating these
// retroactively would break consensus on Masaya's already-finalized Unzen
// blocks, so they stay frozen — same rationale as MasayaTxIntrinsicZkGas.
func masayaUnzenOpcodeMultipliers() [256]uint16 {
	var m [256]uint16
	for i := range m {
		m[i] = math.MaxUint16
	}
	m[0x00] = 0   // STOP
	m[0x01] = 12  // ADD
	m[0x02] = 21  // MUL
	m[0x03] = 13  // SUB
	m[0x04] = 110 // DIV
	m[0x05] = 93  // SDIV
	m[0x06] = 95  // MOD
	m[0x07] = 29  // SMOD
	m[0x08] = 71  // ADDMOD
	m[0x09] = 152 // MULMOD
	m[0x0a] = 33  // EXP
	m[0x0b] = 21  // SIGNEXTEND
	m[0x10] = 11  // LT
	m[0x11] = 10  // GT
	m[0x12] = 14  // SLT
	m[0x13] = 14  // SGT
	m[0x14] = 35  // EQ
	m[0x15] = 8   // ISZERO
	m[0x16] = 8   // AND
	m[0x17] = 9   // OR
	m[0x18] = 9   // XOR
	m[0x19] = 6   // NOT
	m[0x1a] = 9   // BYTE
	m[0x1b] = 20  // SHL
	m[0x1c] = 19  // SHR
	m[0x1d] = 29  // SAR
	m[0x20] = 85  // KECCAK256
	m[0x30] = 22  // ADDRESS
	m[0x31] = 6   // BALANCE
	m[0x32] = 21  // ORIGIN
	m[0x33] = 21  // CALLER
	m[0x34] = 13  // CALLVALUE
	m[0x35] = 20  // CALLDATALOAD
	m[0x36] = 11  // CALLDATASIZE
	m[0x37] = 12  // CALLDATACOPY
	m[0x38] = 11  // CODESIZE
	m[0x39] = 13  // CODECOPY
	m[0x3a] = 14  // GASPRICE
	m[0x3b] = 6   // EXTCODESIZE
	m[0x3c] = 6   // EXTCODECOPY
	m[0x3d] = 10  // RETURNDATASIZE
	m[0x3e] = 9   // RETURNDATACOPY
	m[0x3f] = 8   // EXTCODEHASH
	m[0x40] = 7   // BLOCKHASH
	m[0x41] = 21  // COINBASE
	m[0x42] = 11  // TIMESTAMP
	m[0x43] = 11  // NUMBER
	m[0x44] = 28  // PREVRANDAO
	m[0x45] = 11  // GASLIMIT
	m[0x46] = 11  // CHAINID
	m[0x47] = 85  // SELFBALANCE
	m[0x48] = 11  // BASEFEE
	m[0x49] = 10  // BLOBHASH
	m[0x4a] = 15  // BLOBBASEFEE
	m[0x50] = 5   // POP
	m[0x51] = 20  // MLOAD
	m[0x52] = 22  // MSTORE
	m[0x53] = 9   // MSTORE8
	m[0x54] = 5   // SLOAD
	m[0x55] = 13  // SSTORE
	m[0x56] = 3   // JUMP
	m[0x57] = 5   // JUMPI
	m[0x58] = 12  // PC
	m[0x59] = 11  // MSIZE
	m[0x5a] = 11  // GAS
	m[0x5b] = 9   // JUMPDEST
	m[0x5c] = 1   // TLOAD
	m[0x5d] = 6   // TSTORE
	m[0x5e] = 5   // MCOPY
	m[0x5f] = 10  // PUSH0
	m[0x60] = 5   // PUSH1
	m[0x61] = 5   // PUSH2
	m[0x62] = 6   // PUSH3
	m[0x63] = 8   // PUSH4
	m[0x64] = 6   // PUSH5
	m[0x65] = 8   // PUSH6
	m[0x66] = 7   // PUSH7
	m[0x67] = 7   // PUSH8
	m[0x68] = 9   // PUSH9
	m[0x69] = 10  // PUSH10
	m[0x6a] = 8   // PUSH11
	m[0x6b] = 9   // PUSH12
	m[0x6c] = 7   // PUSH13
	m[0x6d] = 12  // PUSH14
	m[0x6e] = 10  // PUSH15
	m[0x6f] = 11  // PUSH16
	m[0x70] = 13  // PUSH17
	m[0x71] = 11  // PUSH18
	m[0x72] = 11  // PUSH19
	m[0x73] = 13  // PUSH20
	m[0x74] = 14  // PUSH21
	m[0x75] = 16  // PUSH22
	m[0x76] = 12  // PUSH23
	m[0x77] = 16  // PUSH24
	m[0x78] = 13  // PUSH25
	m[0x79] = 14  // PUSH26
	m[0x7a] = 15  // PUSH27
	m[0x7b] = 17  // PUSH28
	m[0x7c] = 17  // PUSH29
	m[0x7d] = 12  // PUSH30
	m[0x7e] = 18  // PUSH31
	m[0x7f] = 17  // PUSH32
	m[0x80] = 6   // DUP1
	m[0x81] = 5   // DUP2
	m[0x82] = 6   // DUP3
	m[0x83] = 6   // DUP4
	m[0x84] = 6   // DUP5
	m[0x85] = 6   // DUP6
	m[0x86] = 5   // DUP7
	m[0x87] = 6   // DUP8
	m[0x88] = 7   // DUP9
	m[0x89] = 5   // DUP10
	m[0x8a] = 6   // DUP11
	m[0x8b] = 8   // DUP12
	m[0x8c] = 6   // DUP13
	m[0x8d] = 7   // DUP14
	m[0x8e] = 8   // DUP15
	m[0x8f] = 6   // DUP16
	m[0x90] = 16  // SWAP1
	m[0x91] = 18  // SWAP2
	m[0x92] = 18  // SWAP3
	m[0x93] = 19  // SWAP4
	m[0x94] = 16  // SWAP5
	m[0x95] = 17  // SWAP6
	m[0x96] = 17  // SWAP7
	m[0x97] = 15  // SWAP8
	m[0x98] = 18  // SWAP9
	m[0x99] = 17  // SWAP10
	m[0x9a] = 18  // SWAP11
	m[0x9b] = 19  // SWAP12
	m[0x9c] = 19  // SWAP13
	m[0x9d] = 18  // SWAP14
	m[0x9e] = 17  // SWAP15
	m[0x9f] = 18  // SWAP16
	m[0xa0] = 6   // LOG0
	m[0xa1] = 7   // LOG1
	m[0xa2] = 4   // LOG2
	m[0xa3] = 5   // LOG3
	m[0xa4] = 5   // LOG4
	m[0xf0] = 1   // CREATE
	m[0xf1] = 25  // CALL
	m[0xf2] = 24  // CALLCODE
	m[0xf3] = 0   // RETURN
	m[0xf4] = 21  // DELEGATECALL
	m[0xf5] = 1   // CREATE2
	m[0xfa] = 24  // STATICCALL
	m[0xfd] = 0   // REVERT
	m[0xfe] = 0   // INVALID
	m[0xff] = 0   // SELFDESTRUCT
	return m
}

// masayaUnzenPrecompileMultipliers returns the frozen Masaya precompile
// multiplier table, keyed by full precompile address and pinned at the
// pre-recalibration values. Frozen for the same consensus reason as
// masayaUnzenOpcodeMultipliers. Canonical precompiles all live at 0x00…00XX, so
// common.Address{19: 0xNN} spells their keys. Absent addresses resolve to
// FailsafeMultiplier. p256verify (RIP-7212, 0x100) is deliberately omitted here:
// it was never in Masaya's finalized schedule, so adding it would break
// consensus on already-finalized Masaya Unzen blocks.
func masayaUnzenPrecompileMultipliers() map[common.Address]uint16 {
	return map[common.Address]uint16{
		{19: 0x01}: 81,   // ecrecover
		{19: 0x02}: 10,   // sha256
		{19: 0x03}: 3,    // ripemd160
		{19: 0x04}: 2,    // identity
		{19: 0x05}: 1363, // modexp
		{19: 0x06}: 38,   // bn128_add
		{19: 0x07}: 87,   // bn128_mul
		{19: 0x08}: 82,   // bn128_pairing
		{19: 0x09}: 243,  // blake2f
		{19: 0x0a}: 398,  // point_evaluation
		{19: 0x0b}: 112,  // bls12_g1add
		{19: 0x0c}: 52,   // bls12_g1msm
		{19: 0x0e}: 111,  // bls12_g2add
		{19: 0x0f}: 39,   // bls12_g2msm
		{19: 0x11}: 134,  // bls12_pairing
		{19: 0x12}: 159,  // bls12_map_fp_to_g1
		{19: 0x13}: 112,  // bls12_map_fp2_to_g2
	}
}

// unzenOpcodeMultipliers returns the recalibrated default opcode multiplier
// table, with fail-safe defaults for unlisted opcodes.
func unzenOpcodeMultipliers() [256]uint16 {
	var m [256]uint16
	for i := range m {
		m[i] = math.MaxUint16
	}
	m[0x00] = 0   // STOP
	m[0x01] = 19  // ADD
	m[0x02] = 19  // MUL
	m[0x03] = 22  // SUB
	m[0x04] = 76  // DIV
	m[0x05] = 78  // SDIV
	m[0x06] = 66  // MOD
	m[0x07] = 28  // SMOD
	m[0x08] = 52  // ADDMOD
	m[0x09] = 113 // MULMOD
	m[0x0a] = 21  // EXP
	m[0x0b] = 17  // SIGNEXTEND
	m[0x10] = 19  // LT
	m[0x11] = 19  // GT
	m[0x12] = 20  // SLT
	m[0x13] = 19  // SGT
	m[0x14] = 36  // EQ
	m[0x15] = 16  // ISZERO
	m[0x16] = 19  // AND
	m[0x17] = 20  // OR
	m[0x18] = 18  // XOR
	m[0x19] = 15  // NOT
	m[0x1a] = 17  // BYTE
	m[0x1b] = 24  // SHL
	m[0x1c] = 22  // SHR
	m[0x1d] = 21  // SAR
	m[0x20] = 31  // KECCAK256
	m[0x30] = 19  // ADDRESS
	m[0x31] = 4   // BALANCE
	m[0x32] = 21  // ORIGIN
	m[0x33] = 18  // CALLER
	m[0x34] = 11  // CALLVALUE
	m[0x35] = 22  // CALLDATALOAD
	m[0x36] = 13  // CALLDATASIZE
	m[0x37] = 13  // CALLDATACOPY
	m[0x38] = 11  // CODESIZE
	m[0x39] = 12  // CODECOPY
	m[0x3a] = 15  // GASPRICE
	m[0x3b] = 4   // EXTCODESIZE
	m[0x3c] = 4   // EXTCODECOPY
	m[0x3d] = 12  // RETURNDATASIZE
	m[0x3e] = 10  // RETURNDATACOPY
	m[0x3f] = 7   // EXTCODEHASH
	m[0x40] = 6   // BLOCKHASH
	m[0x41] = 18  // COINBASE
	m[0x42] = 10  // TIMESTAMP
	m[0x43] = 12  // NUMBER
	m[0x44] = 42  // PREVRANDAO
	m[0x45] = 13  // GASLIMIT
	m[0x46] = 11  // CHAINID
	m[0x47] = 52  // SELFBALANCE
	m[0x48] = 14  // BASEFEE
	m[0x49] = 13  // BLOBHASH
	m[0x4a] = 15  // BLOBBASEFEE
	m[0x50] = 10  // POP
	m[0x51] = 18  // MLOAD
	m[0x52] = 29  // MSTORE
	m[0x53] = 10  // MSTORE8
	m[0x54] = 3   // SLOAD
	m[0x55] = 5   // SSTORE
	m[0x56] = 4   // JUMP
	m[0x57] = 5   // JUMPI
	m[0x58] = 13  // PC
	m[0x59] = 13  // MSIZE
	m[0x5a] = 11  // GAS
	m[0x5b] = 20  // JUMPDEST
	m[0x5c] = 1   // TLOAD
	m[0x5d] = 5   // TSTORE
	m[0x5e] = 4   // MCOPY
	m[0x5f] = 13  // PUSH0
	m[0x60] = 9   // PUSH1
	m[0x61] = 8   // PUSH2
	m[0x62] = 9   // PUSH3
	m[0x63] = 10  // PUSH4
	m[0x64] = 9   // PUSH5
	m[0x65] = 12  // PUSH6
	m[0x66] = 10  // PUSH7
	m[0x67] = 15  // PUSH8
	m[0x68] = 13  // PUSH9
	m[0x69] = 12  // PUSH10
	m[0x6a] = 12  // PUSH11
	m[0x6b] = 15  // PUSH12
	m[0x6c] = 15  // PUSH13
	m[0x6d] = 17  // PUSH14
	m[0x6e] = 21  // PUSH15
	m[0x6f] = 13  // PUSH16
	m[0x70] = 20  // PUSH17
	m[0x71] = 18  // PUSH18
	m[0x72] = 20  // PUSH19
	m[0x73] = 20  // PUSH20
	m[0x74] = 18  // PUSH21
	m[0x75] = 14  // PUSH22
	m[0x76] = 22  // PUSH23
	m[0x77] = 24  // PUSH24
	m[0x78] = 22  // PUSH25
	m[0x79] = 24  // PUSH26
	m[0x7a] = 16  // PUSH27
	m[0x7b] = 17  // PUSH28
	m[0x7c] = 28  // PUSH29
	m[0x7d] = 29  // PUSH30
	m[0x7e] = 16  // PUSH31
	m[0x7f] = 19  // PUSH32
	m[0x80] = 10  // DUP1
	m[0x81] = 8   // DUP2
	m[0x82] = 9   // DUP3
	m[0x83] = 10  // DUP4
	m[0x84] = 11  // DUP5
	m[0x85] = 8   // DUP6
	m[0x86] = 8   // DUP7
	m[0x87] = 10  // DUP8
	m[0x88] = 9   // DUP9
	m[0x89] = 10  // DUP10
	m[0x8a] = 10  // DUP11
	m[0x8b] = 9   // DUP12
	m[0x8c] = 9   // DUP13
	m[0x8d] = 8   // DUP14
	m[0x8e] = 8   // DUP15
	m[0x8f] = 10  // DUP16
	m[0x90] = 31  // SWAP1
	m[0x91] = 30  // SWAP2
	m[0x92] = 32  // SWAP3
	m[0x93] = 31  // SWAP4
	m[0x94] = 33  // SWAP5
	m[0x95] = 34  // SWAP6
	m[0x96] = 31  // SWAP7
	m[0x97] = 30  // SWAP8
	m[0x98] = 32  // SWAP9
	m[0x99] = 31  // SWAP10
	m[0x9a] = 33  // SWAP11
	m[0x9b] = 32  // SWAP12
	m[0x9c] = 31  // SWAP13
	m[0x9d] = 31  // SWAP14
	m[0x9e] = 36  // SWAP15
	m[0x9f] = 31  // SWAP16
	m[0xa0] = 3   // LOG0
	m[0xa1] = 3   // LOG1
	m[0xa2] = 2   // LOG2
	m[0xa3] = 2   // LOG3
	m[0xa4] = 2   // LOG4
	m[0xf0] = 1   // CREATE
	m[0xf1] = 20  // CALL
	m[0xf2] = 20  // CALLCODE
	m[0xf3] = 0   // RETURN
	m[0xf4] = 17  // DELEGATECALL
	m[0xf5] = 1   // CREATE2
	m[0xfa] = 23  // STATICCALL
	m[0xfd] = 0   // REVERT
	m[0xfe] = 0   // INVALID
	m[0xff] = 0   // SELFDESTRUCT
	return m
}

// unzenPrecompileMultipliers returns the recalibrated default precompile
// multiplier table, keyed by full precompile address. Canonical precompiles
// live at 0x00…00XX, so common.Address{19: 0xNN} spells their keys; p256verify
// (RIP-7212) is the exception at the two-byte address 0x100, keyed as
// common.Address{18: 0x01}. Absent addresses resolve to FailsafeMultiplier.
func unzenPrecompileMultipliers() map[common.Address]uint16 {
	return map[common.Address]uint16{
		{19: 0x01}: 47,  // ecrecover
		{19: 0x02}: 10,  // sha256
		{19: 0x03}: 4,   // ripemd160
		{19: 0x04}: 6,   // identity
		{19: 0x05}: 923, // modexp
		{19: 0x06}: 19,  // bn128_add
		{19: 0x07}: 58,  // bn128_mul
		{19: 0x08}: 54,  // bn128_pairing
		{19: 0x09}: 166, // blake2f
		{19: 0x0a}: 859, // point_evaluation
		{19: 0x0b}: 201, // bls12_g1add
		{19: 0x0c}: 93,  // bls12_g1msm
		{19: 0x0e}: 230, // bls12_g2add
		{19: 0x0f}: 71,  // bls12_g2msm
		{19: 0x11}: 365, // bls12_pairing
		{19: 0x12}: 246, // bls12_map_fp_to_g1
		{19: 0x13}: 208, // bls12_map_fp2_to_g2
		// p256verify (RIP-7212) lives at the two-byte address 0x100, so its key
		// is {18: 0x01}, not the {19: 0xNN} form the canonical precompiles use.
		{18: 0x01}: 163, // p256verify
	}
}
