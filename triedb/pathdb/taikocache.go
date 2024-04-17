package pathdb

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

type taikoCache struct {
	taikoState     uint64 // Number of blocks from head whose taiko histories are reserved.
	cleanCacheSize int    // Maximum memory allowance (in bytes) for caching clean nodes
	dirtyCacheSize int    // Maximum memory allowance (in bytes) for caching dirty nodes

	diskdb ethdb.Database

	tailLayer *tailLayer

	ownerPaths *lru.Cache[string, *ownerPath]
	taikoMetas *lru.Cache[uint64, *taikoMeta]

	tm       time.Time
	wg       sync.WaitGroup
	readonly atomic.Bool
	lock     sync.RWMutex
}

func newTaikoCache(config *Config, diskdb ethdb.Database) *taikoCache {
	return &taikoCache{
		taikoState:     config.TaikoState,
		cleanCacheSize: config.CleanCacheSize,
		dirtyCacheSize: config.DirtyCacheSize,

		diskdb: diskdb,

		tailLayer:  newTailLayer(diskdb, config.DirtyCacheSize, config.CleanCacheSize),
		ownerPaths: lru.NewCache[string, *ownerPath](500),
		taikoMetas: lru.NewCache[uint64, *taikoMeta](10000),

		tm: time.Now(),
	}
}

func (t *taikoCache) recordDiffLayer(lyer *diffLayer) error {
	if t.readonly.Load() {
		return nil
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	var (
		start = time.Now()
		batch = t.diskdb.NewBatch()
	)

	// write nodes to disk
	data, err := encodeNodes(lyer.nodes)
	if err != nil {
		return err
	}
	rawdb.WriteNodeHistoryPrefix(t.diskdb, lyer.id, data)

	for owner, subset := range lyer.nodes {
		for path := range subset {
			paths, err := t.loadOwnerPaths(taikoKey(owner, []byte(path), lyer.id))
			if err != nil {
				return err
			}
			paths.addPath(lyer.id)
			// if tail id is updated then truncate the tail.
			if err = paths.savePaths(batch); err != nil {
				return err
			}
		}
	}

	// try to truncate the tail layer.
	if err = t.truncateFromTail(lyer.id); err != nil {
		return err
	}

	// write data to disk.
	size := batch.ValueSize()
	if err = batch.Write(); err != nil {
		log.Error("Failed to write batch", "err", err)
	}

	// log the record layer
	if time.Since(t.tm) > time.Second*3 {
		t.tm = time.Now()
		log.Info("record layer", "id", lyer.id, "bytes", common.StorageSize(size), "elapsed", common.PrettyDuration(time.Since(start)))
	}

	return nil
}

func (t *taikoCache) Close() error {
	// Set the readonly flag.
	t.readonly.Store(true)

	log.Info("closing taiko cache ...")

	// Truncate the taiko metas
	err := t.tailLayer.flush(true)
	if err != nil {
		log.Error("Failed to truncate taiko metas", "err", err)
	}
	t.wg.Wait()

	return err
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

func (t *taikoCache) getLatestIDByPath(owner common.Hash, path []byte, startID uint64) (uint64, error) {
	tailID := t.tailLayer.getTailID()
	for areaID := startID; areaID > tailID; areaID -= batchSize {
		paths, err := t.loadOwnerPaths(taikoKey(owner, path, areaID))
		if err != nil {
			return 0, err
		}
		if len(paths.idList) == 0 {
			continue
		}
		latestID, err := paths.getLatestID(startID)
		if err == nil {
			return latestID, nil
		}
		if errors.Is(err, pathLatestIDError) {
			continue
		}
		return 0, err
	}

	return tailID, nil
}

func (t *taikoCache) loadOwnerPaths(key []byte) (*ownerPath, error) {
	paths, ok := t.ownerPaths.Get(string(key))
	if !ok {
		// load paths from disk.
		var err error
		paths, err = loadPaths(t.diskdb, key)
		if err != nil {
			return nil, err
		}
		t.ownerPaths.Add(string(key), paths)
	}
	return paths, nil
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

func (t *taikoCache) truncateFromTail(latestID uint64) error {
	if latestID <= t.taikoState {
		return nil
	}
	ntail := latestID - t.taikoState
	// Load the meta objects in range [otail+1, ntail]
	tailID := t.tailLayer.getTailID()
	for otail := tailID + 1; otail < ntail; otail++ {
		nodes, err := t.loadDiffLayer(otail)
		if err != nil {
			return err
		}
		t.tailLayer.commit(nodes.nodes)
		t.tailLayer.setTailID(otail)
		// Delete the tail taiko layer.
		rawdb.DeleteNodeHistoryPrefix(t.diskdb, otail)
	}

	// delete the pre area owner paths.
	if tailID > batchSize {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			areaID := tailID/batchSize - 1
			for owner, subset := range t.tailLayer.nodes {
				for path := range subset {
					key := taikoKey(owner, []byte(path), areaID)
					if !rawdb.HasOwnerPath(t.diskdb, key) {
						break
					}
					rawdb.DeleteOwnerPath(t.diskdb, key)
				}
			}
		}()
	}

	// Truncate the taiko metas
	if err := t.tailLayer.flush(false); err != nil {
		return err
	}

	return nil
}
