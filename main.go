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
	numShards               = 16      // tried 8, 32.. no improvement in rps
	maxCacheEntriesPerShard = 100_000 // tried 50_000, no improvement in rps
	port                    = 7171
	readBufferSize          = 65536
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

// extracts command, key, and value
func parseRESP(data []byte) (cmd, key, value string, ok bool) {
	if len(data) < 4 || data[0] != '*' {
		return
	}
	parts := bytes.Split(data[:len(data)-2], []byte("\r\n"))
	if len(parts) < 4 {
		return
	}
	if string(parts[0]) == "*2" && string(parts[2]) == "GET" {
		cmd = "GET"
		key = string(parts[4])
		ok = true
	} else if string(parts[0]) == "*3" && (string(parts[2]) == "PUT" || string(parts[2]) == "SET") {
		cmd = "PUT"
		key = string(parts[4])
		value = string(parts[6])
		ok = true
	}
	return
}

func handleConnection(conn net.Conn, cache *ShardedCache) {
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, readBufferSize)
	for {
		cmdBytes, err := readRESPCommand(reader)
		if err != nil {
			if err != io.EOF {
				log.Println("Error reading:", err)
			}
			return
		}
		processCommand(conn, cmdBytes, cache)
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
	line = line[:len(line)-2]

	if line[0] == '*' {
		numElements, e := strconv.Atoi(line[1:])
		if e != nil {
			return nil, fmt.Errorf("invalid array length")
		}
		cmd := []byte(line + "\r\n")
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
		return nil, fmt.Errorf("invalid bulk string")
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
	data := make([]byte, length+2)
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return nil, err
	}
	return append([]byte(line+"\r\n"), data...), nil
}

func processCommand(conn net.Conn, rawCmd []byte, cache *ShardedCache) {
	command, key, value, ok := parseRESP(rawCmd)
	if !ok {
		conn.Write([]byte("-ERR invalid command\r\n"))
		return
	}
	switch command {
	case "GET":
		if val, found := cache.Get(key); found {
			conn.Write([]byte("$" + strconv.Itoa(len(val)) + "\r\n" + val + "\r\n"))
		} else {
			conn.Write([]byte("$-1\r\n"))
		}
	case "PUT":
		if len(key) > maxKeyValueSize || len(value) > maxKeyValueSize {
			conn.Write([]byte("-ERR key or value too long\r\n"))
			return
		}
		cache.Put(key, value)
		conn.Write([]byte("+OK\r\n"))
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	os.Setenv("GOGC", "800")
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
			tcpConn.SetNoDelay(true)
			tcpConn.SetReadBuffer(readBufferSize)
			tcpConn.SetWriteBuffer(readBufferSize)
		}
		go handleConnection(conn, sc)
	}
}
