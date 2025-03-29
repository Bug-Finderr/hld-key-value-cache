package server

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"server/pkg/cache"
)

// reduce gc pressure
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, ReadBufferSize)
	},
}

type Server struct {
	cache *cache.ShardedCache
}

func NewServer(cache *cache.ShardedCache) *Server {
	return &Server{
		cache: cache,
	}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", Port))
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Printf("Server on :%d\n", Port)

	// connection limiter using semaphore pattern
	sem := make(chan struct{}, MaxConnections)

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
			tcpConn.SetKeepAlivePeriod(TCPKeepAliveInterval)
			tcpConn.SetReadBuffer(ReadBufferSize)
			tcpConn.SetWriteBuffer(ReadBufferSize)
		}

		go func(c net.Conn) {
			s.handleConnection(c)
			<-sem
		}(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, ReadBufferSize)

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
			conn.Write(ErrorResponse)
			continue
		}

		switch ln[:3] {
		case "GET":
			s.handleGet(conn, ln)
		case "PUT":
			s.handlePut(conn, ln)
		default:
			conn.Write(ErrorResponse)
		}
	}
}

func (s *Server) handleGet(conn net.Conn, ln string) {
	space := 3
	for space < len(ln) && ln[space] == ' ' {
		space++
	}

	key := ln[space : len(ln)-1]
	val, found := s.cache.Get(key)

	if found {
		buf := bufferPool.Get().([]byte)
		n := copy(buf, val)
		buf[n] = '\n'
		conn.Write(buf[:n+1])
		bufferPool.Put(buf)
	} else {
		conn.Write(NotFoundResponse)
	}
}

func (s *Server) handlePut(conn net.Conn, ln string) {
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
		conn.Write(ErrorResponse)
		return
	}

	key := rest[:idx]
	value := rest[idx+1:]

	if len(key) > MaxKeyValueSize || len(value) > MaxKeyValueSize {
		conn.Write(ErrorResponse)
		return
	}

	s.cache.Put(key, value)
	conn.Write(OkResponse)
}
