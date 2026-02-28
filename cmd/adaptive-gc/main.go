package main

import (
	"context"
	"os/signal"
	"syscall"

	gc "github.com/samborkent/adaptive-gc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gc.Adapt(ctx)

	var strA []string

	go func() {
		for {
			str := make([]string, 1024)

			strA = str
		}
	}()

	<-ctx.Done()

	_ = strA
}
