package vm

const taikoZKGasDefaultMultiplier = ^uint16(0)

var taikoZKGasSpawnEstimates = [256]uint64{
	CALL:         12_500,
	CALLCODE:     12_500,
	DELEGATECALL: 3_500,
	STATICCALL:   3_500,
	CREATE:       37_000,
	CREATE2:      44_500,
}

var taikoZKGasPrecompileMultipliers = newTaikoZKGasPrecompileMultipliers()

func newTaikoZKGasPrecompileMultipliers() [256]uint16 {
	table := [256]uint16{}
	for i := range table {
		table[i] = taikoZKGasDefaultMultiplier
	}
	table[0x05] = 1363 // modexp
	table[0x0a] = 398  // point_evaluation
	table[0x09] = 243  // blake2f
	table[0x12] = 159  // bls12_map_fp_to_g1
	table[0x11] = 134  // bls12_pairing
	table[0x0b] = 112  // bls12_g1add
	table[0x13] = 112  // bls12_map_fp2_to_g2
	table[0x0e] = 111  // bls12_g2add
	table[0x07] = 87   // bn128_mul
	table[0x08] = 82   // bn128_pairing
	table[0x01] = 81   // ecrecover
	table[0x0c] = 52   // bls12_g1msm
	table[0x0f] = 39   // bls12_g2msm
	table[0x06] = 38   // bn128_add
	table[0x02] = 10   // sha256
	table[0x03] = 3    // ripemd160
	table[0x04] = 2    // identity
	return table
}

var taikoZKGasOpcodeMultipliers = newTaikoZKGasOpcodeMultipliers()

func newTaikoZKGasOpcodeMultipliers() [256]uint16 {
	table := [256]uint16{}
	for i := range table {
		table[i] = taikoZKGasDefaultMultiplier
	}
	table[0x09] = 152 // mulmod
	table[0x04] = 110 // div
	table[0x06] = 95  // mod
	table[0x05] = 93  // sdiv
	table[0x47] = 85  // selfbalance
	table[0x20] = 85  // keccak256
	table[0x08] = 71  // addmod
	table[0x14] = 35  // eq
	table[0x0a] = 33  // exp
	table[0x07] = 29  // smod
	table[0x1d] = 29  // sar
	table[0x44] = 28  // prevrandao
	table[0xf1] = 25  // call
	table[0xf2] = 24  // callcode
	table[0xfa] = 24  // staticcall
	table[0x52] = 22  // mstore
	table[0x30] = 22  // address
	table[0x32] = 21  // origin
	table[0x33] = 21  // caller
	table[0x02] = 21  // mul
	table[0xf4] = 21  // delegatecall
	table[0x41] = 21  // coinbase
	table[0x0b] = 21  // signextend
	table[0x1b] = 20  // shl
	table[0x35] = 20  // calldataload
	table[0x51] = 20  // mload
	table[0x93] = 19  // swap4
	table[0x9c] = 19  // swap13
	table[0x1c] = 19  // shr
	table[0x9b] = 19  // swap12
	table[0x9a] = 18  // swap11
	table[0x92] = 18  // swap3
	table[0x9d] = 18  // swap14
	table[0x98] = 18  // swap9
	table[0x91] = 18  // swap2
	table[0x7e] = 18  // push31
	table[0x9f] = 18  // swap16
	table[0x9e] = 17  // swap15
	table[0x7c] = 17  // push29
	table[0x7b] = 17  // push28
	table[0x96] = 17  // swap7
	table[0x95] = 17  // swap6
	table[0x99] = 17  // swap10
	table[0x7f] = 17  // push32
	table[0x90] = 16  // swap1
	table[0x77] = 16  // push24
	table[0x94] = 16  // swap5
	table[0x75] = 16  // push22
	table[0x97] = 15  // swap8
	table[0x7a] = 15  // push27
	table[0x4a] = 15  // blobbasefee
	table[0x3a] = 14  // gasprice
	table[0x79] = 14  // push26
	table[0x12] = 14  // slt
	table[0x74] = 14  // push21
	table[0x13] = 14  // sgt
	table[0x03] = 13  // sub
	table[0x34] = 13  // callvalue
	table[0x78] = 13  // push25
	table[0x70] = 13  // push17
	table[0x73] = 13  // push20
	table[0x39] = 13  // codecopy
	table[0x55] = 13  // sstore
	table[0x6d] = 12  // push14
	table[0x37] = 12  // calldatacopy
	table[0x7d] = 12  // push30
	table[0x76] = 12  // push23
	table[0x58] = 12  // pc
	table[0x01] = 12  // add
	table[0x72] = 11  // push19
	table[0x5a] = 11  // gas
	table[0x42] = 11  // timestamp
	table[0x48] = 11  // basefee
	table[0x43] = 11  // number
	table[0x71] = 11  // push18
	table[0x36] = 11  // calldatasize
	table[0x6f] = 11  // push16
	table[0x38] = 11  // codesize
	table[0x46] = 11  // chainid
	table[0x10] = 11  // lt
	table[0x45] = 11  // gaslimit
	table[0x59] = 11  // msize
	table[0x3d] = 10  // returndatasize
	table[0x5f] = 10  // push0
	table[0x6e] = 10  // push15
	table[0x11] = 10  // gt
	table[0x69] = 10  // push10
	table[0x49] = 10  // blobhash
	table[0x6b] = 9   // push12
	table[0x68] = 9   // push9
	table[0x17] = 9   // or
	table[0x53] = 9   // mstore8
	table[0x1a] = 9   // byte
	table[0x18] = 9   // xor
	table[0x5b] = 9   // jumpdest
	table[0x3e] = 9   // returndatacopy
	table[0x6a] = 8   // push11
	table[0x16] = 8   // and
	table[0x8b] = 8   // dup12
	table[0x8e] = 8   // dup15
	table[0x65] = 8   // push6
	table[0x63] = 8   // push4
	table[0x15] = 8   // iszero
	table[0x3f] = 8   // extcodehash
	table[0x6c] = 7   // push13
	table[0x66] = 7   // push7
	table[0x40] = 7   // blockhash
	table[0x88] = 7   // dup9
	table[0x67] = 7   // push8
	table[0x8d] = 7   // dup14
	table[0xa1] = 7   // log1
	table[0xa0] = 6   // log0
	table[0x19] = 6   // not
	table[0x8f] = 6   // dup16
	table[0x84] = 6   // dup5
	table[0x62] = 6   // push3
	table[0x85] = 6   // dup6
	table[0x87] = 6   // dup8
	table[0x3b] = 6   // extcodesize
	table[0x31] = 6   // balance
	table[0x80] = 6   // dup1
	table[0x82] = 6   // dup3
	table[0x5d] = 6   // tstore
	table[0x8c] = 6   // dup13
	table[0x8a] = 6   // dup11
	table[0x3c] = 6   // extcodecopy
	table[0x83] = 6   // dup4
	table[0x64] = 6   // push5
	table[0x50] = 5   // pop
	table[0x54] = 5   // sload
	table[0x5e] = 5   // mcopy
	table[0x60] = 5   // push1
	table[0x89] = 5   // dup10
	table[0x61] = 5   // push2
	table[0x86] = 5   // dup7
	table[0xa3] = 5   // log3
	table[0x81] = 5   // dup2
	table[0xa4] = 5   // log4
	table[0x57] = 5   // jumpi
	table[0xa2] = 4   // log2
	table[0x56] = 3   // jump
	table[0x5c] = 1   // tload
	table[0xf0] = 1   // create
	table[0xf5] = 1   // create2
	table[0x00] = 0   // stop
	table[0xf3] = 0   // return
	table[0xfd] = 0   // revert
	table[0xff] = 0   // selfdestruct
	table[0xfe] = 0   // invalid
	return table
}
