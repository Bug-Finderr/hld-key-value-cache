package main

import (
	"bufio"
	"container/list"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
)

// Constants
const (
	numShards               = 32
	maxCacheEntriesPerShard = 100_000
	port                    = 7171
	readBufferSize          = 1024
	maxKeyValueSize         = 256
)

// cache entry
type Entry struct {
	key   string
	value string
}

// single shard
type LRUCache struct {
	capacity int
	items    map[string]*list.Element
	list     *list.List
	mutex    sync.RWMutex
}

// inits a shard
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		list:     list.New(),
	}
}

// manages multiple shards
type ShardedCache struct {
	shards [numShards]*LRUCache
}

// inits cache
func NewShardedCache(capacityPerShard int) *ShardedCache {
	sc := &ShardedCache{}
	for i := 0; i < numShards; i++ {
		sc.shards[i] = NewLRUCache(capacityPerShard)
	}
	return sc
}

// computes a simple FNV-1a hash for sharding
func fnv32(s string) uint32 {
	const (
		prime  uint32 = 16777619
		offset uint32 = 2166136261
	)
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

// selects a shard index
func (sc *ShardedCache) getShard(key string) *LRUCache {
	return sc.shards[fnv32(key)&(numShards-1)]
}

func (sc *ShardedCache) Get(key string) (string, bool) {
	shard := sc.getShard(key)
	shard.mutex.RLock()
	defer shard.mutex.RUnlock()
	if elem, ok := shard.items[key]; ok {
		shard.list.MoveToFront(elem)
		return elem.Value.(*Entry).value, true
	}
	return "", false
}

func (sc *ShardedCache) Put(key, value string) {
	shard := sc.getShard(key)
	shard.mutex.Lock()
	defer shard.mutex.Unlock()
	if elem, ok := shard.items[key]; ok {
		shard.list.MoveToFront(elem)
		elem.Value.(*Entry).value = value
		return
	}
	elem := shard.list.PushFront(&Entry{key: key, value: value})
	shard.items[key] = elem
	if shard.list.Len() > shard.capacity {
		oldest := shard.list.Back()
		shard.list.Remove(oldest)
		delete(shard.items, oldest.Value.(*Entry).key)
	}
}

func handleConnection(conn net.Conn, cache *ShardedCache) {
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, readBufferSize)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Println("Error reading:", err)
			}
			return
		}
		ln := line
		if len(ln) < 4 {
			conn.Write([]byte("ERROR\n"))
			continue
		}
		switch ln[:3] {
		case "GET":
			space := 3
			for space < len(ln) && ln[space] == ' ' {
				space++
			}
			key := ln[space : len(ln)-1]
			val, found := cache.Get(key)
			if found {
				conn.Write([]byte(val + "\n"))
			} else {
				conn.Write([]byte("NOTFOUND\n"))
			}
		case "PUT":
			space := 3
			for space < len(ln) && ln[space] == ' ' {
				space++
			}
			rest := ln[space : len(ln)-1]
			idx := -1
			for i := 0; i < len(rest); i++ {
				if rest[i] == ' ' {
					idx = i
					break
				}
			}
			if idx < 1 {
				conn.Write([]byte("ERROR\n"))
				continue
			}
			key := rest[:idx]
			value := rest[idx+1:]
			if len(key) > maxKeyValueSize || len(value) > maxKeyValueSize {
				conn.Write([]byte("ERROR\n"))
				continue
			}
			cache.Put(key, value)
			conn.Write([]byte("OK\n"))
		default:
			conn.Write([]byte("ERROR\n"))
		}
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	os.Setenv("GOGC", "50")                // more aggressive GC
	os.Setenv("GODEBUG", "madvdontneed=1") // reduce RSS
	log.SetOutput(io.Discard)

	sc := NewShardedCache(maxCacheEntriesPerShard)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("Failed to bind port %d: %v\n", port, err)
		os.Exit(1)
	}
	defer ln.Close()
	fmt.Printf("Server on :%d\n", port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetLinger(0) // Immediately release the port upon close
			tcpConn.SetNoDelay(true)
			tcpConn.SetReadBuffer(readBufferSize)
			tcpConn.SetWriteBuffer(readBufferSize)
		}
		go handleConnection(conn, sc)
	}
}
