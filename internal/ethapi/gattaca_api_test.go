package ethapi

import (
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestDecodeTransaction(t *testing.T) {
	// Test cases with hex strings (without 0x prefix)
	testCases := []struct {
		name    string
		hexStr  string
		wantErr bool
	}{
		{
			name:    "valid_legacy_tx",
			hexStr:  "0x02f901d183028c628203d9808386ff518303d09094167010000000000000000000000000000001000180b90164a9edc416000000000000000000000000000000000000000000000000000000000012c459ff08fbafae1212dbeb12ac6e72b5c80d9fa7f32994007b50741de0651806f9987461696b6f20707265636f6e66730000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000015bde0000000000000000000000000000000000000000000000000000000000000008000000000000000000000000000000000000000000000000000000000000004b00000000000000000000000000000000000000000000000000000000004c4b40000000000000000000000000000000000000000000000000000000004fdec7000000000000000000000000000000000000000000000000000000000023c3460000000000000000000000000000000000000000000000000000000000000001400000000000000000000000000000000000000000000000000000000000000000c001a079be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798a03e6f954c3d571fecc29eddf5172c7cb90efbfc8d9ab6646b9b8f5df471fb5048",
			wantErr: false,
		},
		// Add more test cases as needed
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Convert hex string to bytes
			input, err := hexutil.Decode(tc.hexStr)
			if err != nil {
				t.Fatalf("Failed to decode hex string: %v", err)
			}

			// Try to decode the transaction
			tx := new(types.Transaction)
			err = rlp.DecodeBytes(input, tx)
			if err != nil {
				// If RLP decode fails, try UnmarshalBinary
				err = tx.UnmarshalBinary(input)
				if err != nil && !tc.wantErr {
					t.Errorf("Both RLP decode and UnmarshalBinary failed: %v", err)
					return
				}
			}

			if err == nil && tc.wantErr {
				t.Error("Expected error but got none")
				return
			}

			if err == nil {
				// Force output by using t.Error to always see output
				t.Error("Transaction details:")
				t.Error("Type:", tx.Type())
				t.Error("ChainID:", tx.ChainId())
				t.Error("Nonce:", tx.Nonce())
				t.Error("GasPrice:", tx.GasPrice())
				t.Error("Gas:", tx.Gas())
				if tx.To() != nil {
					t.Error("To:", tx.To().Hex())
				}
				t.Error("Value:", tx.Value())
				t.Error("Data:", hexutil.Encode(tx.Data()))
				t.Error("Hash:", tx.Hash().Hex())
			}
		})
	}
}
