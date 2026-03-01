package main

import (
	"context"
	"crypto/rand"
	"os/signal"
	"syscall"
	"time"

	gc "github.com/samborkent/adaptive-gc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gc.AutoAdapt(ctx, 0.66, 0.95)

	for {
		if ctx.Err() != nil {
			return
		}

		stressMemory(ctx)
	}
}

func stressMemory(ctx context.Context) {
	var data [][]byte

	for range 1000 {
		if ctx.Err() != nil {
			return
		}

		chunk := make([]byte, 1024*1024) // allocate 1MB
		rand.Read(chunk)                 // prevent optimizations
		data = append(data, chunk)       // prevent immediate GC collection

		time.Sleep(10 * time.Millisecond) // slow down allocations
	}

	// Release the memory
	data = nil
}
