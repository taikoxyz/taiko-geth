package pathdb

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"strconv"
)

var (
	batchSize uint64 = 1000
)

var (
	pathLatestIDError = fmt.Errorf("latest id not found")
)

type ownerPath struct {
	key    []byte
	idList []uint64
}

func (p *ownerPath) getLatestID(startID uint64) (uint64, error) {
	ids := p.idList
	if ids == nil {
		return 0, fmt.Errorf("id list is nil")
	}
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] <= startID {
			return ids[i], nil
		}
	}
	return 0, pathLatestIDError
}

func (p *ownerPath) addPath(id uint64) {
	p.idList = append(p.idList, id)
}

type journalUint64 struct {
	IDList []uint64
}

func loadPaths(diskdb ethdb.Database, key []byte) (*ownerPath, error) {
	data := rawdb.ReadOwnerPath(diskdb, key)
	if len(data) == 0 {
		return &ownerPath{
			key:    key,
			idList: make([]uint64, 0),
		}, nil
	}
	var journal = new(journalUint64)
	if err := rlp.Decode(bytes.NewReader(data), journal); err != nil {
		return nil, err
	}
	return &ownerPath{
		key:    key,
		idList: journal.IDList,
	}, nil
}

func (p *ownerPath) savePaths(db ethdb.KeyValueWriter) error {
	w := new(bytes.Buffer)
	if err := rlp.Encode(w, &journalUint64{IDList: p.idList}); err != nil {
		return err
	}
	rawdb.WriteOwnerPath(db, p.key, w.Bytes())
	return nil
}

// cacheKey constructs the unique key of clean cache.
func taikoKey(owner common.Hash, path []byte, id uint64) []byte {
	area := id / batchSize
	key := []byte(strconv.FormatInt(int64(area), 10))
	if owner == (common.Hash{}) {
		key = append(key, path...)
		return key
	}
	key = append(key, append(owner.Bytes(), path...)...)
	return key
}
