package pathdb

import (
	"bytes"
	"fmt"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	pathLatestIDError = fmt.Errorf("latest id not found")
)

type ownerPath struct {
	key    []byte
	IDList []uint64
	raws   atomic.Value
}

func (p *ownerPath) getLatestID(startID uint64) (uint64, error) {
	ids := p.IDList
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

func (p *ownerPath) savePaths(db ethdb.KeyValueWriter) error {
	if p.raws.Load() == nil {
		w := new(bytes.Buffer)
		if err := rlp.Encode(w, p.IDList); err != nil {
			return err
		}
		p.raws.Store(w.Bytes())
	}

	rawdb.WriteOwnerPath(db, p.key, p.raws.Load().([]byte))
	return nil
}

func (p *ownerPath) addPath(id uint64) {
	p.IDList = append(p.IDList, id)
}

func (p *ownerPath) truncateTail(tailID uint64) {
	ids := p.IDList
	for len(ids) > 0 && ids[0] <= tailID {
		ids = ids[1:]
	}
	p.IDList = ids
}

func loadPaths(diskdb ethdb.Database, key []byte) (*ownerPath, error) {
	data := rawdb.ReadOwnerPath(diskdb, key)
	var idList []uint64
	if err := rlp.Decode(bytes.NewReader(data), &idList); err != nil {
		return nil, err
	}
	return &ownerPath{
		key:    key,
		IDList: idList,
	}, nil
}
