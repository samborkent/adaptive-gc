package gc

import (
	"context"
	"log/slog"
	"runtime/metrics"

	"github.com/samborkent/adaptive-gc/internal/notify"
)

var samples = []metrics.Sample{
	{ // 0
		// GCAssistTime
		Name: "/cpu/classes/gc/mark/assist:cpu-seconds",
	},
	{ // 1
		// GCDedicatedTime
		Name: "/cpu/classes/gc/mark/dedicated:cpu-seconds",
	},
	{ // 2
		// GCIdleTime
		Name: "/cpu/classes/gc/mark/idle:cpu-seconds",
	},
	{ // 3
		// GCPauseTime
		Name: "/cpu/classes/gc/pause:cpu-seconds",
	},
	{ // 4
		// ScavengeTotalTime
		Name: "/cpu/classes/scavenge/total:cpu-seconds",
	},
	{ // 5
		// UserTime
		Name: "/cpu/classes/user:cpu-seconds",
	},
	{ // 6
		// TotalTime
		Name: "/cpu/classes/total:cpu-seconds",
	},
}

func Adapt(ctx context.Context) {
	notifier := notify.New()

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-notifier.AfterGC():
			slog.InfoContext(ctx, "GC run")

			metrics.Read(samples)

			gcTime := 0.0
			userTime := 0.0
			totalTime := 0.0

			for i, sample := range samples {
				slog.InfoContext(ctx, "metric",
					slog.String("name", sample.Name),
					slog.Float64("value", sample.Value.Float64()),
				)

				if i <= 4 {
					gcTime += sample.Value.Float64()
				} else if i == 5 {
					userTime = sample.Value.Float64()
				} else if i == 6 {
					totalTime = sample.Value.Float64()
				}
			}

			throughput := userTime / (userTime + gcTime)
			overhead := gcTime / totalTime

			slog.InfoContext(ctx, "gc time",
				slog.Float64("throughput", throughput),
				slog.Float64("overhead", overhead),
			)
		}
	}()
}
