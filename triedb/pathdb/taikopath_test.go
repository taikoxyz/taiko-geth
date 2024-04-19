package pathdb

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
)

func TestUint64(t *testing.T) {
	var (
		ids     = []uint64{1}
		journal = &journalIndex{IDList: ids}
	)
	w := new(bytes.Buffer)
	assert.NoError(t, rlp.Encode(w, journal))
	data := w.Bytes()

	var journal2 = new(journalIndex)
	assert.NoError(t, rlp.Decode(bytes.NewReader(data), journal2))
	assert.Equal(t, ids, journal2.IDList)
}

func TestOwnerPath(t *testing.T) {
	db, _ := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), "", "", false)
	ownerPath := &pathIndex{
		key:    []byte("key"),
		idList: []uint64{1, 2, 3},
	}
	assert.NoError(t, ownerPath.savePathIndex(db))

	path1, err := loadPathIndex(db, []byte("key"))
	assert.NoError(t, err)
	assert.Equal(t, ownerPath.idList, path1.idList)
}
