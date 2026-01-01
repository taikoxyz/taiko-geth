package core

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/params"
)

// CHANGE(taiko): DecodeBasefeeSharingPctg returns the basefee sharing pctg
// stored in the first byte of extraData.
func DecodeBasefeeSharingPctg(extra []byte) uint8 {
	if len(extra) == 0 {
		return 0
	}
	return extra[params.ExtraDataBasefeeSharingPctgIndex]
}

// CHANGE(taiko): DecodeProposalID decodes the proposalId from bytes 1..6.
func DecodeProposalID(extra []byte) (*big.Int, error) {
	if len(extra) < params.ShastaExtraDataLen {
		return nil, fmt.Errorf("extraData too short for proposalId: %d", len(extra))
	}
	start := params.ExtraDataProposalIDIndex
	end := start + params.ExtraDataProposalIDLength
	return new(big.Int).SetBytes(extra[start:end]), nil
}
