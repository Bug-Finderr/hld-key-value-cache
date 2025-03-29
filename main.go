package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"

	"server/pkg/cache"
	"server/pkg/server"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	os.Setenv("GOGC", "80")                          // less aggressive GC to reduce CPU usage
	os.Setenv("GODEBUG", "madvdontneed=1,gctrace=0") // reduce memory usage
	log.SetOutput(io.Discard)

	sc := cache.NewShardedCache(server.MaxCacheEntriesPerShard)

	srv := server.NewServer(sc)
	if err := srv.Start(); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
}
