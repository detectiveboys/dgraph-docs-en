/*
Copyright 2013 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Modified by Dgraph Labs, Inc.

// Package lru implements an LRU cache.
package posting

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"

	farm "github.com/dgryski/go-farm"
	"github.com/golang/glog"
)

type shard struct {
	sync.Mutex

	MaxEntries int
	ll         *list.List
	cache      map[string]*list.Element
}

// listCache is an LRU cache.
type listCache struct {
	// MaxSize is the maximum size of cache before an item is evicted.
	// MaxSize    uint64
	MaxEntries int

	shards []*shard
	// evicts uint64

	done int32
}

type CacheStats struct {
	Length    int
	Size      uint64
	NumEvicts uint64
}

type entry struct {
	key string
	pl  *List
}

// New creates a new Cache.
func newListCache(maxEntries int) *listCache {
	lc := &listCache{}
	for i := 0; i < 64; i++ {
		lc.shards = append(lc.shards, &shard{
			MaxEntries: maxEntries,
			ll:         list.New(),
			cache:      make(map[string]*list.Element),
		})
	}

	// lc := &listCache{
	// 	MaxEntries: maxEntries,
	// 	ll:         list.New(),
	// 	cache:      make(map[string]*list.Element),
	// }

	go lc.removeOldestLoop()
	return lc
}

// func (c *listCache) UpdateMaxSize(size int) int {
// 	c.Lock()
// 	defer c.Unlock()
// 	if size == -1 {
// 		size = c.MaxEntries
// 	}
// 	c.MaxEntries = size
// 	x.LcacheCapacity.Set(int64(c.MaxEntries))
// 	return c.MaxEntries
// }

// Add adds a value to the cache.
func (lc *listCache) PutIfMissing(key string, pl *List) (res *List) {
	id := farm.Fingerprint32([]byte(key)) % 64
	c := lc.shards[id]

	c.Lock()
	defer c.Unlock()

	if ee, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ee)
		res = ee.Value.(*entry).pl
		return res
	}

	e := &entry{
		key: key,
		pl:  pl,
	}
	ele := c.ll.PushFront(e)
	c.cache[key] = ele

	return e.pl
}

func (c *listCache) removeOldestLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var i int
	for range ticker.C {
		c.removeOldest(i)
		if atomic.LoadInt32(&c.done) > 0 {
			return
		}
		i++
		i %= 64
	}
}

func (lc *listCache) removeOldest(i int) {
	c := lc.shards[i]
	c.Lock()
	defer c.Unlock()
	start := time.Now()
	defer func() {
		glog.Infof("lru.removeOldest for %d blocked for: %s.", i, time.Since(start))
	}()
	if c.MaxEntries == 0 {
		// Allow unlimited LRU cache, or we're done here.
		return
	}
	if c.cache == nil {
		return
	}

	// Allow 10ms out of every second for removal.
	deadline := start.Add(10 * time.Millisecond)
	for c.ll.Len() > c.MaxEntries && time.Now().Before(deadline) {
		ele := c.ll.Back()
		if ele == nil {
			break
		}
		e := ele.Value.(*entry)

		if !e.pl.SetForDeletion() {
			// If the posting list has pending mutations, SetForDeletion would
			// return false, and hence would be skipped.
			ele = ele.Prev()
			continue
		}

		// No mutations found and we have marked the PL for deletion. Now we can
		// safely delete it from the cache.
		delete(c.cache, e.key)

		// ele gets Reset once it's passed to Remove, so store the prev.
		prev := ele.Prev()
		c.ll.Remove(ele)
		// c.evicts++
		ele = prev
	}
}

// Get looks up a key's value from the cache.
func (lc *listCache) Get(key string) (pl *List) {
	i := farm.Fingerprint32([]byte(key)) % 64
	c := lc.shards[i]
	c.Lock()
	defer c.Unlock()

	if ele, hit := c.cache[key]; hit {
		c.ll.MoveToFront(ele)
		e := ele.Value.(*entry)
		return e.pl
	}
	return nil
}

// Len returns the number of items in the cache.
// func (c *listCache) Stats() CacheStats {
// 	c.Lock()
// 	defer c.Unlock()

// 	return CacheStats{
// 		Length:    c.ll.Len(),
// 		NumEvicts: c.evicts,
// 	}
// }

func (c *listCache) Each(f func(key []byte, val *List)) {
	// c.Lock()
	// defer c.Unlock()

	// ele := c.ll.Front()
	// for ele != nil {
	// 	e := ele.Value.(*entry)
	// 	f(e.pl.key, e.pl)
	// 	ele = ele.Next()
	// }
}

func (c *listCache) Reset() {
	// c.Lock()
	// defer c.Unlock()
	// c.ll = list.New()
	// c.cache = make(map[string]*list.Element)
}

func (c *listCache) iterate(cont func(l *List) bool) {
	// c.Lock()
	// defer c.Unlock()
	// for _, e := range c.cache {
	// 	kv := e.Value.(*entry)
	// 	if !cont(kv.pl) {
	// 		return
	// 	}
	// }
}

// Doesn't sync to disk, call this function only when you are deleting the pls.
func (c *listCache) clear(remove func(key []byte) bool) {
	// c.Lock()
	// defer c.Unlock()
	// for k, e := range c.cache {
	// 	kv := e.Value.(*entry)
	// 	if !remove(kv.pl.key) {
	// 		continue
	// 	}

	// 	c.ll.Remove(e)
	// 	delete(c.cache, k)
	// }
}

// delete removes a key from cache
func (lc *listCache) delete(key []byte) {
	i := farm.Fingerprint32(key) % 64
	c := lc.shards[i]

	c.Lock()
	defer c.Unlock()

	if ele, ok := c.cache[string(key)]; ok {
		c.ll.Remove(ele)
		delete(c.cache, string(key))
	}
}
