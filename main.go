package main

import (
	"bufio"
	"bytes"
	"container/list"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
)

// Constants
const (
	numShards               = 16
	maxCacheEntriesPerShard = 100_000 // ~1.6M total entries, fits ~400MB with 256-char keys/values
	port                    = 7171
	readBufferSize          = 4096
	maxKeyValueSize         = 256
)

// Preallocated responses
// var (
// 	respOK      = []byte("+OK\r\n")
// 	respNil     = []byte("$-1\r\n")
// 	respErr     = []byte("-ERR invalid command\r\n")
// 	respTooLong = []byte("-ERR key or value too long\r\n")
// )

// Entry represents a cache entry
type Entry struct {
	key   string
	value string
}

// LRUCache represents a single shard
type LRUCache struct {
	capacity int
	items    map[string]*list.Element
	list     *list.List
	mutex    sync.RWMutex
}

// NewLRUCache initializes a shard
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		list:     list.New(),
	}
}

// ShardedCache manages multiple shards
type ShardedCache struct {
	shards [numShards]*LRUCache
}

// NewShardedCache initializes the cache
func NewShardedCache(capacityPerShard int) *ShardedCache {
	sc := &ShardedCache{}
	for i := 0; i < numShards; i++ {
		sc.shards[i] = NewLRUCache(capacityPerShard)
	}
	return sc
}

// fnv32 computes FNV-1a hash for sharding
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

// getShard selects a shard
func (sc *ShardedCache) getShard(key string) *LRUCache {
	return sc.shards[fnv32(key)&(numShards-1)]
}

// Get retrieves a value
func (sc *ShardedCache) Get(key string) (string, bool) {
	shard := sc.getShard(key)
	shard.mutex.RLock()
	defer shard.mutex.RUnlock()
	if elem, exists := shard.items[key]; exists {
		shard.list.MoveToFront(elem)
		return elem.Value.(*Entry).value, true
	}
	return "", false
}

// Put inserts or updates a key-value pair
func (sc *ShardedCache) Put(key, value string) {
	shard := sc.getShard(key)
	shard.mutex.Lock()
	defer shard.mutex.Unlock()
	if elem, exists := shard.items[key]; exists {
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

// BufferPool manages reusable buffers
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool initializes the pool
func NewBufferPool(size int) *BufferPool {
	return &BufferPool{
		pool: sync.Pool{New: func() interface{} { return make([]byte, size) }},
	}
}

// Get retrieves a buffer
func (p *BufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

// Put returns a buffer
func (p *BufferPool) Put(b []byte) {
	p.pool.Put(b)
}

var (
	cache      *ShardedCache
	bufferPool *BufferPool
)

// parseRESP efficiently parses RESP commands
func parseRESP(data []byte) (cmd, key, value string, ok bool) {
	if len(data) < 4 || data[0] != '*' {
		return
	}
	parts := bytes.Split(data[:len(data)-2], []byte("\r\n")) // Trim trailing \r\n
	if len(parts) < 4 {
		return
	}
	if string(parts[0]) == "*2" && len(parts) >= 5 && string(parts[2]) == "GET" {
		cmd = "GET"
		key = string(parts[4])
		ok = true
	} else if string(parts[0]) == "*3" && len(parts) >= 7 && (string(parts[2]) == "PUT" || string(parts[2]) == "SET") {
		cmd = "PUT"
		key = string(parts[4])
		value = string(parts[6])
		ok = true
	}
	return
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		cmd, err := readRESPCommand(reader)
		if err != nil {
			if err != io.EOF {
				log.Println("Error reading command:", err)
			}
			return
		}
		processCommand(conn, cmd)
	}
}

func readRESPCommand(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("invalid RESP format")
	}
	line = line[:len(line)-2] // Remove \r\n

	if line[0] == '*' {
		numElements, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, fmt.Errorf("invalid array length")
		}
		var cmd []byte
		cmd = append(cmd, []byte(line+"\r\n")...)
		for i := 0; i < numElements; i++ {
			bulk, err := readBulkString(reader)
			if err != nil {
				return nil, err
			}
			cmd = append(cmd, bulk...)
		}
		return cmd, nil
	}
	return nil, fmt.Errorf("unsupported RESP type")
}

func readBulkString(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("invalid bulk string format")
	}
	line = line[:len(line)-2]
	if line[0] != '$' {
		return nil, fmt.Errorf("expected bulk string")
	}
	length, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, fmt.Errorf("invalid bulk string length")
	}
	if length < 0 {
		return []byte("$-1\r\n"), nil
	}
	data := make([]byte, length+2) // +2 for \r\n
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return nil, err
	}
	return append([]byte(line+"\r\n"), data...), nil
}

func processCommand(conn net.Conn, cmd []byte) {
	command, key, value, ok := parseRESP(cmd)
	if !ok {
		conn.Write([]byte("-ERR invalid command\r\n"))
		return
	}
	switch command {
	case "GET":
		if v, found := cache.Get(key); found {
			resp := []byte("$" + strconv.Itoa(len(v)) + "\r\n" + v + "\r\n")
			conn.Write(resp)
		} else {
			conn.Write([]byte("$-1\r\n"))
		}
	case "PUT":
		if len(key) > maxKeyValueSize || len(value) > maxKeyValueSize {
			conn.Write([]byte("-ERR key or value too long\r\n"))
		} else {
			cache.Put(key, value)
			conn.Write([]byte("+OK\r\n"))
		}
	}
}

func main() {
	runtime.GOMAXPROCS(2)    // Match t3.small’s 2 cores
	os.Setenv("GOGC", "800") // Delay GC to reduce overhead

	cache = NewShardedCache(maxCacheEntriesPerShard)
	bufferPool = NewBufferPool(readBufferSize)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("Failed to bind to port %d: %v\n", port, err)
		os.Exit(1)
	}
	defer ln.Close()
	fmt.Printf("KVCache server listening on :%d\n", port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			tcp.SetNoDelay(true)
			tcp.SetReadBuffer(65536)
			tcp.SetWriteBuffer(65536)
		}
		go handleConnection(conn)
	}
}
