package pathdb

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

type taikoCache struct {
	config *Config

	diskdb  ethdb.Database
	batchdb ethdb.Batch
	freezer *rawdb.ResettableFreezer

	tailLayer *tailLayer

	ownerPaths *lru.Cache[common.Hash, *ownerPath]
	taikoMetas *lru.Cache[uint64, *taikoMeta]

	lock sync.RWMutex
}

func newTaikoCache(config *Config, diskdb ethdb.Database, freezer *rawdb.ResettableFreezer) *taikoCache {
	return &taikoCache{
		config:  config,
		diskdb:  diskdb,
		batchdb: diskdb.NewBatch(),
		freezer: freezer,

		tailLayer:  newTailLayer(diskdb, config.DirtyCacheSize, config.CleanCacheSize),
		ownerPaths: lru.NewCache[common.Hash, *ownerPath](100),
		taikoMetas: lru.NewCache[uint64, *taikoMeta](10000),
	}
}

func (t *taikoCache) recordDiffLayer(lyer *diffLayer) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	// try to truncate the tail layer.
	if err := t.truncateFromTail(); err != nil {
		return err
	}
	tailID := t.tailLayer.getTailID()
	for owner, subset := range lyer.nodes {
		paths, ok := t.ownerPaths.Get(owner)
		if !ok {
			paths = newOwnerPath(owner)
			t.ownerPaths.Add(owner, paths)
		}

		paths.addPath(subset, lyer.id)
		if tailID > 0 {
			paths.truncateTail(tailID)
		}
		if err := paths.savePaths(t.batchdb); err != nil {
			return err
		}
	}

	// write nodes to disk
	data, err := encodeNodes(lyer.nodes)
	if err != nil {
		return err
	}
	rawdb.WriteNodeHistoryPrefix(t.batchdb, lyer.id, data)

	// write data to disk.
	if size := t.batchdb.ValueSize(); size >= t.config.DirtyCacheSize {
		if err := t.batchdb.Write(); err != nil {
			log.Error("Failed to write batch", "err", err)
		}
		t.batchdb.Reset()
		log.Debug("Flushed taiko cache", "size", size)
	}

	return nil
}

func (t *taikoCache) Close() error {
	// Truncate the taiko metas
	return t.tailLayer.flush(t.batchdb, true)
}

func (t *taikoCache) Reader(root common.Hash) layer {
	lyer, err := newTaikoLayer(t, root)
	if err != nil {
		log.Error("Failed to recover state", "root", root, "err", err)
		return nil
	}
	return lyer
}

func (t *taikoCache) getTailLayer() *tailLayer {
	return t.tailLayer
}

func (t *taikoCache) getLatestIDByPath(owner common.Hash, path string, startID uint64) (uint64, error) {
	paths, ok := t.ownerPaths.Get(owner)
	if !ok {
		// load paths from disk.
		var err error
		paths, err = loadPaths(t.diskdb, owner)
		if err != nil {
			return 0, err
		}
		t.ownerPaths.Add(owner, paths)
	}

	return paths.getLatestID(path, startID)
}

func (t *taikoCache) loadDiffLayer(id uint64) (*taikoMeta, error) {
	if !t.taikoMetas.Contains(id) {
		var err error
		nodes, err := decodeNodes(rawdb.ReadNodeHistoryPrefix(t.diskdb, id))
		if err != nil {
			return nil, err
		}
		t.taikoMetas.Add(id, &taikoMeta{nodes: nodes})
	}
	node, _ := t.taikoMetas.Get(id)
	return node, nil
}

func (t *taikoCache) truncateFromTail() error {
	// Truncate the taiko metas
	if err := t.tailLayer.flush(t.batchdb, false); err != nil {
		return err
	}

	ohead, err := t.freezer.Ancients()
	if err != nil {
		return err
	}
	ntail := ohead - t.config.StateHistory
	// Load the meta objects in range [otail+1, ntail]
	for otail := t.tailLayer.getTailID(); otail < ntail; otail++ {
		nodes, err := t.loadDiffLayer(otail)
		if err != nil {
			return err
		}
		t.tailLayer.commit(nodes.nodes)
		t.tailLayer.setTailID(otail + 1)
	}

	return nil
}
