package rawdb

import (
	"encoding/binary"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

var (
	taikoTailID       = []byte(":t:t-")
	taikoIDListPrefix = []byte(":t:o-")
	tailAccountPreFix = []byte(":t:a-")
	tailStoragePreFix = []byte(":t:s-")
	nodeHistoryPrefix = []byte(":t:h-")
)

func ReadTailAccountTrieNode(db ethdb.KeyValueReader, path []byte) ([]byte, common.Hash) {
	data, err := db.Get(append(tailAccountPreFix, path[:]...))
	if err != nil {
		return nil, common.Hash{}
	}
	h := newHasher()
	defer h.release()
	return data, h.hash(data)
}

func WriteTailAccountTrieNode(db ethdb.KeyValueWriter, path []byte, data []byte) {
	if err := db.Put(append(tailAccountPreFix, path[:]...), data); err != nil {
		log.Crit("WriteTailAccountTrieNode failed", "err", err)
	}
}

func DeleteTailAccountTrieNode(db ethdb.KeyValueWriter, path []byte) {
	if err := db.Delete(append(tailAccountPreFix, path[:]...)); err != nil {
		log.Crit("DeleteTailAccountTrieNode failed", "err", err)
	}
}

func ReadTailStorageTrieNode(db ethdb.KeyValueReader, accountHash common.Hash, path []byte) ([]byte, common.Hash) {
	data, err := db.Get(append(append(tailStoragePreFix, accountHash[:]...), path[:]...))
	if err != nil {
		return nil, common.Hash{}
	}
	h := newHasher()
	defer h.release()
	return data, h.hash(data)
}

func WriteTailStorageTrieNode(db ethdb.KeyValueWriter, accountHash common.Hash, path []byte, data []byte) {
	if err := db.Put(append(append(tailStoragePreFix, accountHash[:]...), path[:]...), data); err != nil {
		log.Crit("WriteTailStorageTrieNode failed", "err", err)
	}
}

func DeleteTailStorageTrieNode(db ethdb.KeyValueWriter, accountHash common.Hash, path []byte) {
	if err := db.Delete(append(append(tailStoragePreFix, accountHash[:]...), path[:]...)); err != nil {
		log.Crit("DeleteTailStorageTrieNode failed", "err", err)
	}
}

func ReadTaikoTailID(db ethdb.KeyValueReader) uint64 {
	data, _ := db.Get(taikoTailID)
	if len(data) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(data)
}

func WriteTaikoTailID(db ethdb.KeyValueWriter, number uint64) {
	if err := db.Put(taikoTailID, encodeBlockNumber(number)); err != nil {
		log.Crit("WriteTaikoTailID failed", "err", err)
	}
}

// ReadOwnerPath reads the owner path from the database.
// key: id + owner + path
// key: id + path
func ReadOwnerPath(db ethdb.KeyValueReader, key []byte) []byte {
	data, _ := db.Get(append(taikoIDListPrefix, key[:]...))
	return data
}

func WriteOwnerPath(db ethdb.KeyValueWriter, key, data []byte) {
	if err := db.Put(append(taikoIDListPrefix, key[:]...), data); err != nil {
		log.Crit("WriteOwnerPath failed", "err", err)
	}
}

func HasOwnerPath(db ethdb.KeyValueReader, key []byte) bool {
	ok, _ := db.Has(append(taikoIDListPrefix, key[:]...))
	return ok
}

func DeleteOwnerPath(db ethdb.KeyValueWriter, key []byte) {
	if err := db.Delete(append(taikoIDListPrefix, key[:]...)); err != nil {
		log.Crit("DeleteOwnerPath failed", "err", err)
	}
}

func ReadNodeHistoryPrefix(db ethdb.KeyValueReader, id uint64) []byte {
	data, _ := db.Get(append(nodeHistoryPrefix, encodeBlockNumber(id)...))
	return data
}

func WriteNodeHistoryPrefix(db ethdb.KeyValueWriter, id uint64, data []byte) {
	if err := db.Put(append(nodeHistoryPrefix, encodeBlockNumber(id)...), data); err != nil {
		log.Crit("WriteNodeHistoryPrefix failed", "err", err)
	}
}

func DeleteNodeHistoryPrefix(db ethdb.KeyValueWriter, id uint64) {
	if err := db.Delete(append(nodeHistoryPrefix, encodeBlockNumber(id)...)); err != nil {
		log.Crit("DeleteNodeHistoryPrefix failed", "err", err)
	}
}
