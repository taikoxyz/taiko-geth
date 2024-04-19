package pathdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTaikoLayer(t *testing.T) {
	lyer := fillLayers(1, 1)[0]

	data, err := encodeLayer(lyer)
	assert.NoError(t, err)

	lyer2, err := decodeLayer(data)
	assert.NoError(t, err)

	assert.Equal(t, lyer.block, lyer2.block)
	assert.Equal(t, lyer.root, lyer2.root)
	assert.Equal(t, lyer.nodes, lyer2.nodes)
}
