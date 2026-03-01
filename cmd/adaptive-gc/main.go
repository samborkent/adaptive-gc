package main

import (
	"context"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	gc "github.com/samborkent/adaptive-gc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gc.AutoAdapt(ctx, 50, 100)

	for range 20 {
		runtime.GC()
		time.Sleep(100 * time.Millisecond)
	}
}
