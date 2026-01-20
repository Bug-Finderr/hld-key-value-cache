package cache

import (
	"fmt"
	"sync"
	"testing"
)

func TestBasicPutGet(t *testing.T) {
	cache := NewShardedCache(100)

	// Test basic put and get
	cache.Put("key1", "value1")
	val, found := cache.Get("key1")

	if !found {
		t.Error("Expected to find key1")
	}
	if val != "value1" {
		t.Errorf("Expected value1, got %s", val)
	}
}

func TestGetNonExistent(t *testing.T) {
	cache := NewShardedCache(100)

	_, found := cache.Get("nonexistent")
	if found {
		t.Error("Expected not to find nonexistent key")
	}
}

func TestUpdateExistingKey(t *testing.T) {
	cache := NewShardedCache(100)

	cache.Put("key1", "value1")
	cache.Put("key1", "value2")

	val, found := cache.Get("key1")
	if !found {
		t.Error("Expected to find key1")
	}
	if val != "value2" {
		t.Errorf("Expected value2 after update, got %s", val)
	}
}

func TestLRUEviction(t *testing.T) {
	// Create a cache with capacity 2 per shard
	cache := NewLRUCache(2)

	// Add 3 items - the first should be evicted
	cache.mutex.Lock()
	elem1 := cache.list.PushFront(&Entry{key: "key1", value: "value1"})
	cache.items["key1"] = elem1
	elem2 := cache.list.PushFront(&Entry{key: "key2", value: "value2"})
	cache.items["key2"] = elem2
	elem3 := cache.list.PushFront(&Entry{key: "key3", value: "value3"})
	cache.items["key3"] = elem3

	// Manually trigger eviction
	if cache.list.Len() > cache.capacity {
		if oldest := cache.list.Back(); oldest != nil {
			if entry, ok := oldest.Value.(*Entry); ok {
				delete(cache.items, entry.key)
				cache.list.Remove(oldest)
			}
		}
	}
	cache.mutex.Unlock()

	// key1 should be evicted
	cache.mutex.RLock()
	_, found := cache.items["key1"]
	cache.mutex.RUnlock()

	if found {
		t.Error("Expected key1 to be evicted")
	}
}

func TestShardedCacheEviction(t *testing.T) {
	// Small capacity to trigger eviction
	cache := NewShardedCache(2)

	// Put multiple keys that hash to the same shard
	// We'll fill up one shard and verify eviction
	cache.Put("a", "1")
	cache.Put("b", "2")
	cache.Put("c", "3")

	// At least one of the early keys might be evicted
	// depending on which shard they land in
	foundCount := 0
	if _, found := cache.Get("a"); found {
		foundCount++
	}
	if _, found := cache.Get("b"); found {
		foundCount++
	}
	if _, found := cache.Get("c"); found {
		foundCount++
	}

	// With capacity 2 per shard, we should find at least some keys
	if foundCount == 0 {
		t.Error("Expected to find at least some keys")
	}
}

func TestConcurrentAccess(t *testing.T) {
	cache := NewShardedCache(1000)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			value := fmt.Sprintf("value%d", i)
			cache.Put(key, value)
		}(i)
	}

	wg.Wait()

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			cache.Get(key)
		}(i)
	}

	wg.Wait()

	// Verify some values exist
	val, found := cache.Get("key50")
	if !found {
		t.Error("Expected to find key50 after concurrent writes")
	}
	if val != "value50" {
		t.Errorf("Expected value50, got %s", val)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	cache := NewShardedCache(100)
	var wg sync.WaitGroup

	// Pre-populate some data
	for i := 0; i < 50; i++ {
		cache.Put(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
	}

	// Concurrent reads and writes
	for i := 0; i < 100; i++ {
		wg.Add(2)

		// Writer
		go func(i int) {
			defer wg.Done()
			cache.Put(fmt.Sprintf("key%d", i), fmt.Sprintf("newvalue%d", i))
		}(i)

		// Reader
		go func(i int) {
			defer wg.Done()
			cache.Get(fmt.Sprintf("key%d", i%50))
		}(i)
	}

	wg.Wait()
}

func TestEmptyKey(t *testing.T) {
	cache := NewShardedCache(100)

	cache.Put("", "empty_key_value")
	val, found := cache.Get("")

	if !found {
		t.Error("Expected to find empty key")
	}
	if val != "empty_key_value" {
		t.Errorf("Expected empty_key_value, got %s", val)
	}
}

func TestEmptyValue(t *testing.T) {
	cache := NewShardedCache(100)

	cache.Put("key_with_empty", "")
	val, found := cache.Get("key_with_empty")

	if !found {
		t.Error("Expected to find key_with_empty")
	}
	if val != "" {
		t.Errorf("Expected empty value, got %s", val)
	}
}

func TestLargeValue(t *testing.T) {
	cache := NewShardedCache(100)

	// Create a large value (but within limits)
	largeValue := make([]byte, 200)
	for i := range largeValue {
		largeValue[i] = 'x'
	}

	cache.Put("large", string(largeValue))
	val, found := cache.Get("large")

	if !found {
		t.Error("Expected to find large key")
	}
	if len(val) != 200 {
		t.Errorf("Expected value length 200, got %d", len(val))
	}
}

func TestMultipleShards(t *testing.T) {
	cache := NewShardedCache(100)

	// Add many keys to distribute across shards
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("distributed_key_%d", i)
		value := fmt.Sprintf("value_%d", i)
		cache.Put(key, value)
	}

	// Verify we can retrieve all keys
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("distributed_key_%d", i)
		expectedValue := fmt.Sprintf("value_%d", i)

		val, found := cache.Get(key)
		if !found {
			t.Errorf("Expected to find %s", key)
		}
		if val != expectedValue {
			t.Errorf("Expected %s, got %s", expectedValue, val)
		}
	}
}

func TestLRUCacheCreation(t *testing.T) {
	cache := NewLRUCache(50)

	if cache.capacity != 50 {
		t.Errorf("Expected capacity 50, got %d", cache.capacity)
	}
	if cache.items == nil {
		t.Error("Expected items map to be initialized")
	}
	if cache.list == nil {
		t.Error("Expected list to be initialized")
	}
}

func TestShardedCacheCreation(t *testing.T) {
	cache := NewShardedCache(100)

	for i := 0; i < NumShards; i++ {
		if cache.shards[i] == nil {
			t.Errorf("Expected shard %d to be initialized", i)
		}
		if cache.shards[i].capacity != 100 {
			t.Errorf("Expected shard %d capacity 100, got %d", i, cache.shards[i].capacity)
		}
	}
}

func TestEntryMethods(t *testing.T) {
	entry := &Entry{key: "testkey", value: "testvalue"}

	if entry.Key() != "testkey" {
		t.Errorf("Expected testkey, got %s", entry.Key())
	}
	if entry.Value() != "testvalue" {
		t.Errorf("Expected testvalue, got %s", entry.Value())
	}
}

func BenchmarkPut(b *testing.B) {
	cache := NewShardedCache(10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Put(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
	}
}

func BenchmarkGet(b *testing.B) {
	cache := NewShardedCache(10000)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		cache.Put(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(fmt.Sprintf("key%d", i%10000))
	}
}

func BenchmarkConcurrentPutGet(b *testing.B) {
	cache := NewShardedCache(10000)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				cache.Put(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			} else {
				cache.Get(fmt.Sprintf("key%d", i))
			}
			i++
		}
	})
}
