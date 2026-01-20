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
	// GC tuning - errors are non-fatal, log and continue
	if err := os.Setenv("GOGC", "80"); err != nil {
		fmt.Printf("Warning: failed to set GOGC: %v\n", err)
	}
	if err := os.Setenv("GODEBUG", "madvdontneed=1,gctrace=0"); err != nil {
		fmt.Printf("Warning: failed to set GODEBUG: %v\n", err)
	}
	log.SetOutput(io.Discard)

	sc := cache.NewShardedCache(server.MaxCacheEntriesPerShard)

	srv := server.NewServer(sc)
	if err := srv.Start(); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
}
