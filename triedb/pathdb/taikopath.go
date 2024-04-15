package pathdb

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

var (
	pathLatestIDError = fmt.Errorf("latest id not found")
)

type journalPath struct {
	Path []byte
	Ids  []uint64
}

type ownerPath struct {
	owner common.Hash
	paths map[string][]uint64
}

func newOwnerPath(owner common.Hash) *ownerPath {
	return &ownerPath{owner: owner, paths: map[string][]uint64{}}
}

func (p *ownerPath) getLatestID(path string, startID uint64) (uint64, error) {
	ids := p.paths[path]
	if ids == nil {
		return 0, fmt.Errorf("path not found %s", path)
	}
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] <= startID {
			return ids[i], nil
		}
	}
	return 0, pathLatestIDError
}

func (p *ownerPath) savePaths(db ethdb.KeyValueWriter) error {
	data, err := encodePaths(p.paths)
	if err != nil {
		return err
	}
	rawdb.WriteOwnerPath(db, p.owner, data)
	return nil
}

func (p *ownerPath) addPath(subset map[string]*trienode.Node, id uint64) {
	for path := range subset {
		ids := p.paths[path]
		if ids == nil {
			ids = []uint64{id}
		} else {
			ids = append(ids, id)
		}
		p.paths[path] = ids
	}
}

func (p *ownerPath) truncateTail(tailID uint64) {
	for path := range p.paths {
		ids := p.paths[path]
		var i = len(ids) - 1
		for ; i >= 0; i-- {
			if ids[i] <= tailID {
				break
			}
		}
		p.paths[path] = ids[:i]
	}
	return
}

func loadPaths(diskdb ethdb.Database, owner common.Hash) (*ownerPath, error) {
	return decodePaths(rawdb.ReadOwnerPath(diskdb, owner))
}

func encodePaths(paths map[string][]uint64) ([]byte, error) {
	jpaths := make([]journalPath, 0, len(paths))
	for path, ids := range paths {
		jpaths = append(jpaths, journalPath{Path: []byte(path), Ids: ids})
	}
	w := new(bytes.Buffer)
	if err := rlp.Encode(w, jpaths); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func decodePaths(data []byte) (*ownerPath, error) {
	r := rlp.NewStream(bytes.NewReader(data), 0)
	var paths []journalPath
	if err := r.Decode(&paths); err != nil {
		return nil, err
	}
	ownerPath := &ownerPath{paths: map[string][]uint64{}}
	for _, paths := range paths {
		ownerPath.paths[string(paths.Path)] = paths.Ids
	}
	return ownerPath, nil
}
