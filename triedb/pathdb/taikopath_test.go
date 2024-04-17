package pathdb

import (
	"bytes"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestUint64(t *testing.T) {
	var (
		ids     = []uint64{1}
		journal = &journalUint64{IDList: ids}
	)
	w := new(bytes.Buffer)
	assert.NoError(t, rlp.Encode(w, journal))
	data := w.Bytes()

	var journal2 = new(journalUint64)
	assert.NoError(t, rlp.Decode(bytes.NewReader(data), journal2))
	assert.Equal(t, ids, journal2.IDList)
}
