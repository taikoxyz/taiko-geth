package core

import (
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"math/big"
)

// HeadL1Origin returns the latest L2 block's corresponding L1 origin.
func (bc *BlockChain) HeadL1Origin() (*rawdb.L1Origin, error) {
	blockID, err := rawdb.ReadHeadL1Origin(bc.db)
	if err != nil {
		return nil, err
	}

	if blockID == nil {
		return nil, ethereum.NotFound
	}

	l1Origin, err := rawdb.ReadL1Origin(bc.db, blockID)
	if err != nil {
		return nil, err
	}

	if l1Origin == nil {
		return nil, ethereum.NotFound
	}

	return l1Origin, nil
}

// L1OriginByID returns the L2 block's corresponding L1 origin.
func (bc *BlockChain) L1OriginByID(blockID *big.Int) (*rawdb.L1Origin, error) {
	l1Origin, err := rawdb.ReadL1Origin(bc.db, blockID)
	if err != nil {
		return nil, err
	}

	if l1Origin == nil {
		return nil, ethereum.NotFound
	}

	return l1Origin, nil
}

// WriteL1Origin stores a L1Origin into the database.
func (bc *BlockChain) WriteL1Origin(l1Origin *rawdb.L1Origin) {
	// Write L1Origin.
	rawdb.WriteL1Origin(bc.db, l1Origin.BlockID, l1Origin)
	// Write the head L1Origin.
	rawdb.WriteHeadL1Origin(bc.db, l1Origin.BlockID)
}
