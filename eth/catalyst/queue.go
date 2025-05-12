// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package catalyst

import (
	"sync"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// CHANGE(taiko): payload prefix for levelsDB
const payloadPrefix = "payload:"


// maxTrackedPayloads is the maximum number of prepared payloads the execution
// engine tracks before evicting old ones. Ideally we should only ever track the
// latest one; but have a slight wiggle room for non-ideal conditions.
const maxTrackedPayloads = 3 * 768 // CHANGE(taiko): change to use 3 * `maxBlocksPerBatch`

// maxTrackedHeaders is the maximum number of executed payloads the execution
// engine tracks before evicting old ones. These are tracked outside the chain
// during initial sync to allow ForkchoiceUpdate to reference past blocks via
// hashes only. For the sync target it would be enough to track only the latest
// header, but snap sync also needs the latest finalized height for the ancient
// limit.
const maxTrackedHeaders = 96

// payloadQueueItem represents an id->payload tuple to store until it's retrieved
// or evicted.
type payloadQueueItem struct {
	id      engine.PayloadID
	payload *miner.Payload
}

// headerQueueItem represents a hash->header tuple to store until it's retrieved or evicted.
type headerQueueItem struct {
	hash   common.Hash
	header *types.Header
}

// persistedPayloadQueue tracks payloads in memory and persists them to LevelDB.
// CHANGE(taiko): change payloadQueue to persisted
type persistedPayloadQueue struct {
	payloads []*payloadQueueItem
	db       *leveldb.DB
	lock     sync.RWMutex
}

// CHANGE(taiko): persisted payload queue using levels DB
// newPersistedPayloadQueue opens (or creates) a LevelDB at dbPath and loads existing entries.
func newPersistedPayloadQueue(dbPath string) (*persistedPayloadQueue, error) {
	var db *leveldb.DB
	var err error

	if dbPath != "" {
		db, err = leveldb.OpenFile(dbPath, nil)
		if err != nil {
			return nil, err
		}
	}
	q := &persistedPayloadQueue{
		payloads: make([]*payloadQueueItem, 0, maxTrackedPayloads),
		db:       db,
	}
	// Load from DB
	if db != nil {
		iter := db.NewIterator(util.BytesPrefix([]byte(payloadPrefix)), nil)
		defer iter.Release()
		for iter.Next() {
			key := iter.Key()
			data := iter.Value()

			id := engine.PayloadID(key[len(payloadPrefix):])

			p := new(miner.Payload)
			if err := rlp.DecodeBytes(data, p); err != nil {
				continue
			}
			q.payloads = append(q.payloads, &payloadQueueItem{id: id, payload: p})
			if len(q.payloads) >= maxTrackedPayloads {
				break
			}
		}
	}

	return q, nil
}

func (q *persistedPayloadQueue) has(id engine.PayloadID) bool {
	q.lock.RLock()
	defer q.lock.RUnlock()

	for _, item := range q.payloads {
		if item == nil {
			break
		}
		if item.id == id {
			return true
		}
	}
	return false
}

// put inserts a new payload into memory and persists it to LevelDB.
func (q *persistedPayloadQueue) put(id engine.PayloadID, payload *miner.Payload) {
	q.lock.Lock()
	defer q.lock.Unlock()

	copy(q.payloads[1:], q.payloads)
	q.payloads[0] = &payloadQueueItem{id: id, payload: payload}

	key := []byte(payloadPrefix + id.String())
	data, err := rlp.EncodeToBytes(payload)
	if err != nil {
		return
	}

	if q.db != nil {
		if err := q.db.Put(key, data, nil); err != nil {
			return
		}
	}
}

// get retrieves a payload from memory or falls back to LevelDB.
func (q *persistedPayloadQueue) get(id engine.PayloadID, full bool) *engine.ExecutionPayloadEnvelope {
	q.lock.RLock()

	defer q.lock.RUnlock()

	for _, item := range q.payloads {
		if item == nil {
			break
		}
		if item.id == id {
			if !full {
				return item.payload.Resolve()
			}
			return item.payload.ResolveFull()
		}
	}

	// Fallback to DB
	if q.db != nil {
		key := []byte(payloadPrefix + id.String())
		data, err := q.db.Get(key, nil)
		if err != nil {
			return nil
		}

		p := new(miner.Payload)
		if err := rlp.DecodeBytes(data, p); err != nil {
			return nil
		}
		if !full {
			return p.Resolve()
		}

		return p.ResolveFull()
	}

	return nil
}

// headerQueue tracks the latest handful of constructed headers to be retrieved
// by the beacon chain if block production is requested.
type headerQueue struct {
	headers []*headerQueueItem
	lock    sync.RWMutex
}

// newHeaderQueue creates a pre-initialized queue with a fixed number of slots
// all containing empty items.
func newHeaderQueue() *headerQueue {
	return &headerQueue{
		headers: make([]*headerQueueItem, maxTrackedHeaders),
	}
}

// put inserts a new header into the queue at the given hash.
func (q *headerQueue) put(hash common.Hash, data *types.Header) {
	q.lock.Lock()
	defer q.lock.Unlock()

	copy(q.headers[1:], q.headers)
	q.headers[0] = &headerQueueItem{
		hash:   hash,
		header: data,
	}
}

// get retrieves a previously stored header item or nil if it does not exist.
func (q *headerQueue) get(hash common.Hash) *types.Header {
	q.lock.RLock()
	defer q.lock.RUnlock()

	for _, item := range q.headers {
		if item == nil {
			return nil // no more items
		}
		if item.hash == hash {
			return item.header
		}
	}
	return nil
}
