package cache

import (
	"container/list"
	"sync"
)

// single shard of the cache
type LRUCache struct {
	capacity int
	items    map[string]*list.Element
	list     *list.List
	mutex    sync.RWMutex
}

// inits a new LRU cache with the given capacity
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		list:     list.New(),
	}
}
