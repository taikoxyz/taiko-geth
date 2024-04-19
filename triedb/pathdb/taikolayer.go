package pathdb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/triestate"
)

type cacheInterface interface {
	getLatestIDByPath(owner common.Hash, path []byte, startID uint64) (uint64, error)
	loadDiffLayer(id uint64) (*taikoMeta, error)
	getTailLayer() *tailLayer
}

type taikoLayer struct {
	cacheInterface
	id uint64
}

func newTaikoLayer(t *taikoCache, root common.Hash) (*taikoLayer, error) {
	id := rawdb.ReadTaikoStateID(t.diskdb, root)
	if id == nil {
		return nil, fmt.Errorf("id not found when create taiko layer, root: %x", root)
	}
	return &taikoLayer{cacheInterface: t, id: *id}, nil
}

func (dl *taikoLayer) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	tLayer := dl.getTailLayer()
	for layerID := dl.id; ; layerID-- {
		tailID := tLayer.getTailID()
		log.Debug("taiko layer node message", "owner", owner.String(), "path", hexutil.Encode(path), "layerID", layerID, "tailID", tailID)
		if layerID <= tailID {
			break
		}

		// use `id < tailID` aims to make sure the latest node is not in the diff layer.
		id, err := dl.getLatestIDByPath(owner, path, layerID)
		if err != nil {
			return nil, err
		}
		if id <= tailID {
			break
		}
		//log.Warn("node message", "owner", owner.String(), "layerID", layerID, "id", id)
		node, err := dl.loadDiffLayer(id)
		if err != nil {
			return nil, err
		}
		// If the trie node is known locally, return it
		subset, ok := node.nodes[owner]
		if ok {
			n, ok := subset[string(path)]
			if ok {
				// If the trie node is not hash matched, or marked as removed,
				// bubble up an error here. It shouldn't happen at all.
				if n.Hash != hash {
					log.Error("taiko layer unexpected trie node in diff layer", "owner", owner, "path", path, "expect", hash, "got", n.Hash)
					return nil, fmt.Errorf("unexpected trie node in diff layer: %x", hash)
				}
				return n.Blob, nil
			}
		}
	}

	if tLayer.getTailID() == 0 {
		return nil, errors.New("unexpected trie node in tail layer")
	}

	return tLayer.Node(owner, path, hash)
}

func (dl *taikoLayer) rootHash() common.Hash {
	panic("unimplemented")
}
func (dl *taikoLayer) stateID() uint64 {
	panic("unimplemented")
}
func (dl *taikoLayer) parentLayer() layer {
	panic("unimplemented")
}
func (dl *taikoLayer) update(root common.Hash, id uint64, block uint64, nodes map[common.Hash]map[string]*trienode.Node, states *triestate.Set) *diffLayer {
	panic("unimplemented")
}
func (dl *taikoLayer) journal(w io.Writer) error {
	panic("unimplemented")
}

type taikoMeta struct {
	root  common.Hash
	block uint64
	id    uint64
	nodes map[common.Hash]map[string]*trienode.Node
}

func encodeLayer(lyer *diffLayer) ([]byte, error) {
	w := new(bytes.Buffer)

	// Write the root hash and block number
	if err := rlp.Encode(w, lyer.root); err != nil {
		return nil, err
	}
	// Write the block number
	if err := rlp.Encode(w, lyer.block); err != nil {
		return nil, err
	}
	// Write the accumulated trie nodes into buffer
	res := make([]journalNodes, 0, len(lyer.nodes))
	for owner, subset := range lyer.nodes {
		entry := journalNodes{Owner: owner}
		for path, node := range subset {
			entry.Nodes = append(entry.Nodes, journalNode{Path: []byte(path), Blob: node.Blob})
		}
		res = append(res, entry)
	}
	if err := rlp.Encode(w, res); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func decodeLayer(data []byte) (*diffLayer, error) {
	r := rlp.NewStream(bytes.NewReader(data), 0)

	var root common.Hash
	if err := r.Decode(&root); err != nil {
		return nil, err
	}
	var block uint64
	if err := r.Decode(&block); err != nil {
		return nil, err
	}
	// Read in-memory trie nodes from journal
	var encoded []journalNodes
	if err := r.Decode(&encoded); err != nil {
		return nil, fmt.Errorf("load diff nodes: %v", err)
	}
	nodes := make(map[common.Hash]map[string]*trienode.Node)
	for _, entry := range encoded {
		subset := make(map[string]*trienode.Node)
		for _, n := range entry.Nodes {
			if len(n.Blob) > 0 {
				subset[string(n.Path)] = trienode.New(crypto.Keccak256Hash(n.Blob), n.Blob)
			} else {
				subset[string(n.Path)] = trienode.NewDeleted()
			}
		}
		nodes[entry.Owner] = subset
	}
	return &diffLayer{
		root:  root,
		block: block,
		nodes: nodes,
	}, nil
}

type tailLayer struct {
	size  uint64
	limit uint64

	diskdb ethdb.Database

	tailID atomic.Uint64

	cleans *fastcache.Cache
	nodes  map[common.Hash]map[string]*trienode.Node
	lock   sync.RWMutex
}

func newTailLayer(diskdb ethdb.Database, dirtySize, cleanSize int) *tailLayer {
	layer := &tailLayer{
		limit:  uint64(dirtySize),
		diskdb: diskdb,
		cleans: fastcache.New(cleanSize),
		nodes:  make(map[common.Hash]map[string]*trienode.Node),
	}
	layer.tailID.Store(rawdb.ReadTaikoTailID(diskdb))

	return layer
}

func (t *tailLayer) getTailID() uint64 {
	return t.tailID.Load()
}

func (t *tailLayer) setTailID(id uint64) {
	t.tailID.Store(id)
	rawdb.WriteTaikoTailID(t.diskdb, id)
}

func (t *tailLayer) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	n, ok := t.nodes[owner]
	if ok {
		if node, ok := n[string(path)]; ok {
			if node.Hash != hash {
				log.Error("tail layer unexpected trie node in tail layer", "owner", owner, "path", path, "expect", hash, "got", node.Hash)
				return nil, fmt.Errorf("unexpected trie node in tail layer: %x", hash)
			}
			return node.Blob, nil
		}
	}
	key := cacheKey(owner, path)
	if blob := t.cleans.Get(nil, key); len(blob) > 0 {
		h := newHasher()
		defer h.release()

		got := h.hash(blob)
		if got == hash {
			return blob, nil
		}
		log.Error("Unexpected trie node in clean cache", "owner", owner, "path", path, "expect", hash, "got", got)
	}
	// Try to retrieve the trie node from the disk.
	var (
		nBlob []byte
		nHash common.Hash
	)
	if owner == (common.Hash{}) {
		nBlob, nHash = rawdb.ReadTailAccountTrieNode(t.diskdb, path)
	} else {
		nBlob, nHash = rawdb.ReadTailStorageTrieNode(t.diskdb, owner, path)
	}
	if nHash != hash {
		log.Error("tail layer unexpected trie node in disk", "owner", owner, "path", path, "expect", hash, "got", nHash)
		return nil, newUnexpectedNodeError("disk", hash, nHash, owner, path, nBlob)
	}
	if len(nBlob) > 0 {
		t.cleans.Set(key, nBlob)
	}
	return nBlob, nil
}

func (t *tailLayer) commit(nodes map[common.Hash]map[string]*trienode.Node) {
	t.lock.Lock()
	defer t.lock.Unlock()
	var delta int64
	for owner, subset := range nodes {
		current, exist := t.nodes[owner]
		if !exist {
			// Allocate a new map for the subset instead of claiming it directly
			// from the passed map to avoid potential concurrent map read/write.
			// The nodes belong to original diff layer are still accessible even
			// after merging, thus the ownership of nodes map should still belong
			// to original layer and any mutation on it should be prevented.
			current = make(map[string]*trienode.Node)
			for path, n := range subset {
				current[path] = n
				delta += int64(len(n.Blob) + len(path))
			}
			t.nodes[owner] = current
			continue
		}
		for path, n := range subset {
			if orig, exist := current[path]; !exist {
				delta += int64(len(n.Blob) + len(path))
			} else {
				delta += int64(len(n.Blob) - len(orig.Blob))
			}
			current[path] = n
		}
		t.nodes[owner] = current
	}
	t.updateSize(delta)
}

// updateSize updates the total cache size by the given delta.
func (t *tailLayer) updateSize(delta int64) {
	size := int64(t.size) + delta
	if size >= 0 {
		t.size = uint64(size)
		return
	}
	s := t.size
	t.size = 0
	log.Error("Invalid pathdb buffer size", "prev", common.StorageSize(s), "delta", common.StorageSize(delta))
}

func (t *tailLayer) reset() {
	t.size = 0
	t.nodes = make(map[common.Hash]map[string]*trienode.Node)
}

func (t *tailLayer) flush(force bool) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if t.size <= t.limit && !force {
		return nil
	}
	var (
		start = time.Now()
		batch = t.diskdb.NewBatch()
	)
	for owner, subset := range t.nodes {
		for path, n := range subset {
			if n.IsDeleted() {
				if owner == (common.Hash{}) {
					rawdb.DeleteTailAccountTrieNode(batch, []byte(path))
				} else {
					rawdb.DeleteTailStorageTrieNode(batch, owner, []byte(path))
				}
				t.cleans.Del(cacheKey(owner, []byte(path)))
			} else {
				if owner == (common.Hash{}) {
					rawdb.WriteTailAccountTrieNode(batch, []byte(path), n.Blob)
				} else {
					rawdb.WriteTailStorageTrieNode(batch, owner, []byte(path), n.Blob)
				}
				t.cleans.Set(cacheKey(owner, []byte(path)), n.Blob)
			}
		}
	}

	// Flush all mutations in a single batch
	size := batch.ValueSize()
	if err := batch.Write(); err != nil {
		return err
	}
	log.Info("Persisted pathdb nodes", "tail_id", t.tailID.Load(), "nodes", len(t.nodes), "bytes", common.StorageSize(size), "elapsed", common.PrettyDuration(time.Since(start)))
	t.reset()
	return nil
}

func (t *tailLayer) rootHash() common.Hash {
	panic("unimplemented")
}
func (t *tailLayer) stateID() uint64 {
	panic("unimplemented")
}
func (t *tailLayer) parentLayer() layer {
	panic("unimplemented")
}
func (t *tailLayer) update(root common.Hash, id uint64, block uint64, nodes map[common.Hash]map[string]*trienode.Node, states *triestate.Set) *diffLayer {
	panic("unimplemented")
}
func (t *tailLayer) journal(w io.Writer) error {
	panic("unimplemented")
}
