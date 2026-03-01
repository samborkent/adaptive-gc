package main

import (
	"context"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	gc "github.com/samborkent/adaptive-gc"
)

type garbage struct {
	_   [1024]bool
	ptr *bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gc.AutoAdapt(ctx)

	for {
		if ctx.Err() != nil {
			return
		}

		g := &garbage{ptr: new(bool)}

		runtime.GC()
		time.Sleep(100 * time.Millisecond)

		_ = g.ptr
	}
}
