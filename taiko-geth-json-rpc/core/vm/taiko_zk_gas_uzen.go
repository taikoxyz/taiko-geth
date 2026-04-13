package vm

import "math"

// CHANGE(taiko): UzenZkGasSchedule defines the consensus zk gas parameters for the Uzen fork.
var UzenZkGasSchedule = func() ZkGasSchedule {
	s := ZkGasSchedule{
		BlockLimit: 100_000_000,
		SpawnEstimates: SpawnEstimates{
			Call:         12500,
			CallCode:     12500,
			DelegateCall: 3500,
			StaticCall:   3500,
			Create:       37000,
			Create2:      44500,
		},
	}

	// Failsafe default: all 256 opcode slots start at MaxUint16.
	for i := range s.OpcodeMultipliers {
		s.OpcodeMultipliers[i] = math.MaxUint16
	}

	// Known opcode multipliers (from the ZK gas spec).
	s.OpcodeMultipliers[0x00] = 0   // STOP
	s.OpcodeMultipliers[0x01] = 12  // ADD
	s.OpcodeMultipliers[0x02] = 21  // MUL
	s.OpcodeMultipliers[0x03] = 13  // SUB
	s.OpcodeMultipliers[0x04] = 110 // DIV
	s.OpcodeMultipliers[0x05] = 93  // SDIV
	s.OpcodeMultipliers[0x06] = 95  // MOD
	s.OpcodeMultipliers[0x07] = 29  // SMOD
	s.OpcodeMultipliers[0x08] = 71  // ADDMOD
	s.OpcodeMultipliers[0x09] = 152 // MULMOD
	s.OpcodeMultipliers[0x0a] = 33  // EXP
	s.OpcodeMultipliers[0x0b] = 21  // SIGNEXTEND
	s.OpcodeMultipliers[0x10] = 11  // LT
	s.OpcodeMultipliers[0x11] = 10  // GT
	s.OpcodeMultipliers[0x12] = 14  // SLT
	s.OpcodeMultipliers[0x13] = 14  // SGT
	s.OpcodeMultipliers[0x14] = 35  // EQ
	s.OpcodeMultipliers[0x15] = 8   // ISZERO
	s.OpcodeMultipliers[0x16] = 8   // AND
	s.OpcodeMultipliers[0x17] = 9   // OR
	s.OpcodeMultipliers[0x18] = 9   // XOR
	s.OpcodeMultipliers[0x19] = 6   // NOT
	s.OpcodeMultipliers[0x1a] = 9   // BYTE
	s.OpcodeMultipliers[0x1b] = 20  // SHL
	s.OpcodeMultipliers[0x1c] = 19  // SHR
	s.OpcodeMultipliers[0x1d] = 29  // SAR
	s.OpcodeMultipliers[0x20] = 85  // KECCAK256
	s.OpcodeMultipliers[0x30] = 22  // ADDRESS
	s.OpcodeMultipliers[0x31] = 6   // BALANCE
	s.OpcodeMultipliers[0x32] = 21  // ORIGIN
	s.OpcodeMultipliers[0x33] = 21  // CALLER
	s.OpcodeMultipliers[0x34] = 13  // CALLVALUE
	s.OpcodeMultipliers[0x35] = 20  // CALLDATALOAD
	s.OpcodeMultipliers[0x36] = 11  // CALLDATASIZE
	s.OpcodeMultipliers[0x37] = 12  // CALLDATACOPY
	s.OpcodeMultipliers[0x38] = 11  // CODESIZE
	s.OpcodeMultipliers[0x39] = 13  // CODECOPY
	s.OpcodeMultipliers[0x3a] = 14  // GASPRICE
	s.OpcodeMultipliers[0x3b] = 6   // EXTCODESIZE
	s.OpcodeMultipliers[0x3c] = 6   // EXTCODECOPY
	s.OpcodeMultipliers[0x3d] = 10  // RETURNDATASIZE
	s.OpcodeMultipliers[0x3e] = 9   // RETURNDATACOPY
	s.OpcodeMultipliers[0x3f] = 8   // EXTCODEHASH
	s.OpcodeMultipliers[0x40] = 7   // BLOCKHASH
	s.OpcodeMultipliers[0x41] = 21  // COINBASE
	s.OpcodeMultipliers[0x42] = 11  // TIMESTAMP
	s.OpcodeMultipliers[0x43] = 11  // NUMBER
	s.OpcodeMultipliers[0x44] = 28  // PREVRANDAO
	s.OpcodeMultipliers[0x45] = 11  // GASLIMIT
	s.OpcodeMultipliers[0x46] = 11  // CHAINID
	s.OpcodeMultipliers[0x47] = 85  // SELFBALANCE
	s.OpcodeMultipliers[0x48] = 11  // BASEFEE
	s.OpcodeMultipliers[0x49] = 10  // BLOBHASH
	s.OpcodeMultipliers[0x4a] = 15  // BLOBBASEFEE
	s.OpcodeMultipliers[0x50] = 5   // POP
	s.OpcodeMultipliers[0x51] = 20  // MLOAD
	s.OpcodeMultipliers[0x52] = 22  // MSTORE
	s.OpcodeMultipliers[0x53] = 9   // MSTORE8
	s.OpcodeMultipliers[0x54] = 5   // SLOAD
	s.OpcodeMultipliers[0x55] = 13  // SSTORE
	s.OpcodeMultipliers[0x56] = 3   // JUMP
	s.OpcodeMultipliers[0x57] = 5   // JUMPI
	s.OpcodeMultipliers[0x58] = 12  // PC
	s.OpcodeMultipliers[0x59] = 11  // MSIZE
	s.OpcodeMultipliers[0x5a] = 11  // GAS
	s.OpcodeMultipliers[0x5b] = 9   // JUMPDEST
	s.OpcodeMultipliers[0x5c] = 1   // TLOAD
	s.OpcodeMultipliers[0x5d] = 6   // TSTORE
	s.OpcodeMultipliers[0x5e] = 5   // MCOPY
	s.OpcodeMultipliers[0x5f] = 10  // PUSH0
	s.OpcodeMultipliers[0x60] = 5   // PUSH1
	s.OpcodeMultipliers[0x61] = 5   // PUSH2
	s.OpcodeMultipliers[0x62] = 6   // PUSH3
	s.OpcodeMultipliers[0x63] = 8   // PUSH4
	s.OpcodeMultipliers[0x64] = 6   // PUSH5
	s.OpcodeMultipliers[0x65] = 8   // PUSH6
	s.OpcodeMultipliers[0x66] = 7   // PUSH7
	s.OpcodeMultipliers[0x67] = 7   // PUSH8
	s.OpcodeMultipliers[0x68] = 9   // PUSH9
	s.OpcodeMultipliers[0x69] = 10  // PUSH10
	s.OpcodeMultipliers[0x6a] = 8   // PUSH11
	s.OpcodeMultipliers[0x6b] = 9   // PUSH12
	s.OpcodeMultipliers[0x6c] = 7   // PUSH13
	s.OpcodeMultipliers[0x6d] = 12  // PUSH14
	s.OpcodeMultipliers[0x6e] = 10  // PUSH15
	s.OpcodeMultipliers[0x6f] = 11  // PUSH16
	s.OpcodeMultipliers[0x70] = 13  // PUSH17
	s.OpcodeMultipliers[0x71] = 11  // PUSH18
	s.OpcodeMultipliers[0x72] = 11  // PUSH19
	s.OpcodeMultipliers[0x73] = 13  // PUSH20
	s.OpcodeMultipliers[0x74] = 14  // PUSH21
	s.OpcodeMultipliers[0x75] = 16  // PUSH22
	s.OpcodeMultipliers[0x76] = 12  // PUSH23
	s.OpcodeMultipliers[0x77] = 16  // PUSH24
	s.OpcodeMultipliers[0x78] = 13  // PUSH25
	s.OpcodeMultipliers[0x79] = 14  // PUSH26
	s.OpcodeMultipliers[0x7a] = 15  // PUSH27
	s.OpcodeMultipliers[0x7b] = 17  // PUSH28
	s.OpcodeMultipliers[0x7c] = 17  // PUSH29
	s.OpcodeMultipliers[0x7d] = 12  // PUSH30
	s.OpcodeMultipliers[0x7e] = 18  // PUSH31
	s.OpcodeMultipliers[0x7f] = 17  // PUSH32
	s.OpcodeMultipliers[0x80] = 6   // DUP1
	s.OpcodeMultipliers[0x81] = 5   // DUP2
	s.OpcodeMultipliers[0x82] = 6   // DUP3
	s.OpcodeMultipliers[0x83] = 6   // DUP4
	s.OpcodeMultipliers[0x84] = 6   // DUP5
	s.OpcodeMultipliers[0x85] = 6   // DUP6
	s.OpcodeMultipliers[0x86] = 5   // DUP7
	s.OpcodeMultipliers[0x87] = 6   // DUP8
	s.OpcodeMultipliers[0x88] = 7   // DUP9
	s.OpcodeMultipliers[0x89] = 5   // DUP10
	s.OpcodeMultipliers[0x8a] = 6   // DUP11
	s.OpcodeMultipliers[0x8b] = 8   // DUP12
	s.OpcodeMultipliers[0x8c] = 6   // DUP13
	s.OpcodeMultipliers[0x8d] = 7   // DUP14
	s.OpcodeMultipliers[0x8e] = 8   // DUP15
	s.OpcodeMultipliers[0x8f] = 6   // DUP16
	s.OpcodeMultipliers[0x90] = 16  // SWAP1
	s.OpcodeMultipliers[0x91] = 18  // SWAP2
	s.OpcodeMultipliers[0x92] = 18  // SWAP3
	s.OpcodeMultipliers[0x93] = 19  // SWAP4
	s.OpcodeMultipliers[0x94] = 16  // SWAP5
	s.OpcodeMultipliers[0x95] = 17  // SWAP6
	s.OpcodeMultipliers[0x96] = 17  // SWAP7
	s.OpcodeMultipliers[0x97] = 15  // SWAP8
	s.OpcodeMultipliers[0x98] = 18  // SWAP9
	s.OpcodeMultipliers[0x99] = 17  // SWAP10
	s.OpcodeMultipliers[0x9a] = 18  // SWAP11
	s.OpcodeMultipliers[0x9b] = 19  // SWAP12
	s.OpcodeMultipliers[0x9c] = 19  // SWAP13
	s.OpcodeMultipliers[0x9d] = 18  // SWAP14
	s.OpcodeMultipliers[0x9e] = 17  // SWAP15
	s.OpcodeMultipliers[0x9f] = 18  // SWAP16
	s.OpcodeMultipliers[0xa0] = 6   // LOG0
	s.OpcodeMultipliers[0xa1] = 7   // LOG1
	s.OpcodeMultipliers[0xa2] = 4   // LOG2
	s.OpcodeMultipliers[0xa3] = 5   // LOG3
	s.OpcodeMultipliers[0xa4] = 5   // LOG4
	s.OpcodeMultipliers[0xf0] = 1   // CREATE
	s.OpcodeMultipliers[0xf1] = 25  // CALL
	s.OpcodeMultipliers[0xf2] = 24  // CALLCODE
	s.OpcodeMultipliers[0xf3] = 0   // RETURN
	s.OpcodeMultipliers[0xf4] = 21  // DELEGATECALL
	s.OpcodeMultipliers[0xf5] = 1   // CREATE2
	s.OpcodeMultipliers[0xfa] = 24  // STATICCALL
	s.OpcodeMultipliers[0xfd] = 0   // REVERT
	s.OpcodeMultipliers[0xfe] = 0   // INVALID
	s.OpcodeMultipliers[0xff] = 0   // SELFDESTRUCT

	// Failsafe default: all 256 precompile slots start at MaxUint16.
	for i := range s.PrecompileMultipliers {
		s.PrecompileMultipliers[i] = math.MaxUint16
	}

	// Known precompile multipliers (from the ZK gas spec).
	s.PrecompileMultipliers[0x01] = 81   // ecrecover
	s.PrecompileMultipliers[0x02] = 10   // sha256
	s.PrecompileMultipliers[0x03] = 3    // ripemd160
	s.PrecompileMultipliers[0x04] = 2    // identity
	s.PrecompileMultipliers[0x05] = 1363 // modexp
	s.PrecompileMultipliers[0x06] = 38   // bn128_add
	s.PrecompileMultipliers[0x07] = 87   // bn128_mul
	s.PrecompileMultipliers[0x08] = 82   // bn128_pairing
	s.PrecompileMultipliers[0x09] = 243  // blake2f
	s.PrecompileMultipliers[0x0a] = 398  // point_evaluation
	s.PrecompileMultipliers[0x0b] = 112  // bls12_g1add
	s.PrecompileMultipliers[0x0c] = 52   // bls12_g1msm
	s.PrecompileMultipliers[0x0e] = 111  // bls12_g2add
	s.PrecompileMultipliers[0x0f] = 39   // bls12_g2msm
	s.PrecompileMultipliers[0x11] = 134  // bls12_pairing
	s.PrecompileMultipliers[0x12] = 159  // bls12_map_fp_to_g1
	s.PrecompileMultipliers[0x13] = 112  // bls12_map_fp2_to_g2

	return s
}()
