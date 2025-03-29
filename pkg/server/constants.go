package server

import "time"

const (
	Port                    = 7171
	ReadBufferSize          = 4096
	MaxKeyValueSize         = 256
	MaxConnections          = 10000
	TCPKeepAliveInterval    = 30 * time.Second
	MaxCacheEntriesPerShard = 20_000
)

var (
	ErrorResponse    = []byte("ERROR\n")
	OkResponse       = []byte("OK\n")
	NotFoundResponse = []byte("NOTFOUND\n")
)
