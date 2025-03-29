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
	"time"
)

// Constants
const (
	numShards               = 16
	maxCacheEntriesPerShard = 20_000
	port                    = 7171
	readBufferSize          = 4096 // increased to reduce syscalls
	maxKeyValueSize         = 256
	maxConnections          = 10000
	tcpKeepAliveInterval    = 30 * time.Second
)

var (
	errorResponse    = []byte("ERROR\n")
	okResponse       = []byte("OK\n")
	notFoundResponse = []byte("NOTFOUND\n")
)

// buffer pool to reduce gc pressure
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, readBufferSize)
	},
}

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

// manages multiple shards
type ShardedCache struct {
	shards [numShards]*LRUCache
}

// inits a shard
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		list:     list.New(),
	}
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
			conn.Write(errorResponse)
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
				buf := bufferPool.Get().([]byte)
				n := copy(buf, val)
				buf[n] = '\n'
				conn.Write(buf[:n+1])
				bufferPool.Put(buf)
			} else {
				conn.Write(notFoundResponse)
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
				conn.Write(errorResponse)
				continue
			}
			key := rest[:idx]
			value := rest[idx+1:]
			if len(key) > maxKeyValueSize || len(value) > maxKeyValueSize {
				conn.Write(errorResponse)
				continue
			}
			cache.Put(key, value)
			conn.Write(okResponse)
		default:
			conn.Write(errorResponse)
		}
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	os.Setenv("GOGC", "80")                          // Less aggressive GC to reduce CPU usage
	os.Setenv("GODEBUG", "madvdontneed=1,gctrace=0") // Reduce memory usage
	log.SetOutput(io.Discard)

	sc := NewShardedCache(maxCacheEntriesPerShard)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("Failed to bind port %d: %v\n", port, err)
		os.Exit(1)
	}
	defer ln.Close()
	fmt.Printf("Server on :%d\n", port)

	sem := make(chan struct{}, maxConnections)

	for {
		select {
		case sem <- struct{}{}:
		default:
			time.Sleep(1 * time.Millisecond)
			continue
		}

		conn, err := ln.Accept()
		if err != nil {
			<-sem
			continue
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetLinger(0)
			tcpConn.SetNoDelay(true)
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(tcpKeepAliveInterval)
			tcpConn.SetReadBuffer(readBufferSize)
			tcpConn.SetWriteBuffer(readBufferSize)
		}

		go func(c net.Conn) {
			handleConnection(c, sc)
			<-sem
		}(conn)
	}
}
