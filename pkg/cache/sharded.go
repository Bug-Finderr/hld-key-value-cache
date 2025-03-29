package cache

import (
	"server/internal/utils"
)

const (
	NumShards = 16
)

// manages multiple cache shards for better concurrency
type ShardedCache struct {
	shards [NumShards]*LRUCache
}

func NewShardedCache(capacityPerShard int) *ShardedCache {
	sc := &ShardedCache{}
	for i := 0; i < NumShards; i++ {
		sc.shards[i] = NewLRUCache(capacityPerShard)
	}
	return sc
}

func (sc *ShardedCache) getShard(key string) *LRUCache {
	return sc.shards[utils.FNV32(key)&(NumShards-1)]
}

func (sc *ShardedCache) Get(key string) (string, bool) {
	shard := sc.getShard(key)
	shard.mutex.RLock()
	defer shard.mutex.RUnlock()

	if elem, ok := shard.items[key]; ok {
		shard.list.MoveToFront(elem)
		if entry, ok := elem.Value.(*Entry); ok && entry != nil {
			return entry.value, true
		}
	}
	return "", false
}

func (sc *ShardedCache) Put(key, value string) {
	shard := sc.getShard(key)
	shard.mutex.Lock()
	defer shard.mutex.Unlock()

	if elem, ok := shard.items[key]; ok {
		shard.list.MoveToFront(elem)
		if entry, ok := elem.Value.(*Entry); ok && entry != nil {
			entry.value = value
		}
		return
	}

	elem := shard.list.PushFront(&Entry{key: key, value: value})
	shard.items[key] = elem

	if shard.list.Len() > shard.capacity {
		if oldest := shard.list.Back(); oldest != nil {
			if entry, ok := oldest.Value.(*Entry); ok && entry != nil {
				delete(shard.items, entry.key)
				shard.list.Remove(oldest)
			}
		}
	}
}
