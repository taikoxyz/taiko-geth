package firehose

import (
	"encoding/hex"
	"math/big"
	"regexp"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessList_marshal(t *testing.T) {
	tests := []struct {
		name    string
		l       AccessList
		wantOut string
	}{
		{"empty", nil, "00"},

		{
			"one address no keys",
			AccessList{
				types.AccessTuple{
					Address: address(t, "0x1234567890123456789012345678901234567890"),
				},
			},
			"01123456789012345678901234567890123456789000",
		},

		{
			"one address one key",
			AccessList{
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "AB")},
				},
			},
			"0112345678901234567890123456789012345678900100000000000000000000000000000000000000000000000000000000000000ab",
		},

		{
			"one address multi keys",
			AccessList{
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "AB"), hash(t, "EF")},
				},
			},
			`
				01
				1234567890123456789012345678901234567890
				  02
				  00000000000000000000000000000000000000000000000000000000000000ab
				  00000000000000000000000000000000000000000000000000000000000000ef
		   `,
		},

		{
			"multi address multi keys",
			AccessList{
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "AB"), hash(t, "EF")},
				},
				types.AccessTuple{
					Address: address(t, "0xabcdefabcdefabcdefabcdefabcdefabcdef0910"),
				},
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "12")},
				},
			},
			`
				03
				1234567890123456789012345678901234567890
					02
					00000000000000000000000000000000000000000000000000000000000000ab
					00000000000000000000000000000000000000000000000000000000000000ef
				abcdefabcdefabcdefabcdefabcdefabcdef0910
					00
				1234567890123456789012345678901234567890
					01
					0000000000000000000000000000000000000000000000000000000000000012
		   `,
		},
	}

	spaceRegex := regexp.MustCompile(`[\s\n\t]+`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, spaceRegex.ReplaceAllString(tt.wantOut, ""), hex.EncodeToString(tt.l.marshal()))
		})
	}
}

func address(t *testing.T, in string) common.Address {
	t.Helper()

	addr, err := common.NewMixedcaseAddressFromString(in)
	require.NoError(t, err)

	return addr.Address()
}

func hash(t *testing.T, in string) common.Hash {
	t.Helper()

	return common.HexToHash(in)
}

func TestEndBlock_DataMarshaling(t *testing.T) {
	header := &types.Header{}
	uncles := []*types.Block{}
	finalizedBlock := types.NewBlockWithHeader(&types.Header{
		Number: big.NewInt(2048),
	})

	endData := map[string]interface{}{
		"header":             header,
		"uncles":             uncles,
		"totalDifficulty":    (*hexutil.Big)(big.NewInt(1024)),
		"finalizedBlockNum":  (*hexutil.Big)(finalizedBlock.Header().Number),
		"finalizedBlockHash": finalizedBlock.Header().Hash(),
	}

	require.Equal(t, `{"finalizedBlockHash":"0x38b1c79e3acb45df1ac1fbd4d70e08c655874fc592c5c86342a29baf4f001769","finalizedBlockNum":"0x800","header":{"parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000","sha3Uncles":"0x0000000000000000000000000000000000000000000000000000000000000000","miner":"0x0000000000000000000000000000000000000000","stateRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","transactionsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","receiptsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","difficulty":null,"number":null,"gasLimit":"0x0","gasUsed":"0x0","timestamp":"0x0","extraData":"0x","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","nonce":"0x0000000000000000","baseFeePerGas":null,"withdrawalsRoot":null,"hash":"0xc3bd2d00745c03048a5616146a96f5ff78e54efb9e5b04af208cdaff6f3830ee"},"totalDifficulty":"0x400","uncles":[]}`, JSON(endData))
}
