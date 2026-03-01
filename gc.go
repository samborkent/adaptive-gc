package gc

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sync/atomic"
)

var callOnce atomic.Bool

type obj struct {
	_ *struct{}
}

var minGCThroughput, maxGCThroughput float64

// AutoAdapt starts to automatically adapt the GOGC percentage until the context is cancelled.
// minThroughput and maxThroughput represent the GC throughput limits.
func AutoAdapt(ctx context.Context, minThroughput, maxThroughput float64) {
	if callOnce.Load() {
		panic("AutoAdapt may only be called once")
	}

	// Clamp max throughput to [0%, 100%].
	maxGCThroughput = max(0, min(1, maxThroughput))

	// Clamp min throughput to [0%, max GC throughput].
	minGCThroughput = max(0, min(maxThroughput, minThroughput))

	_ = runtime.AddCleanup(&obj{}, cleanup, ctx)

	callOnce.Store(true)
}

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
	{ // 7
		// GCCount
		Name: "/gc/cycles/total:gc-cycles",
	},
	{ // 8
		// GCPercent
		Name: "/gc/gogc:percent",
	},
}

var (
	prevGCTime    = 0.0
	prevUserTime  = 0.0
	prevTotalTime = 0.0

	prev2Throughput = 1.0
	prevThroughput  = 1.0

	prev2Overhead = 0.0
	prevOverhead  = 0.0
)

// K-value used in sigmoid model.
const sigmoidFactor = 300.0

// Pre-computed exponent used in scaling factor of sigmoid model.
var sigmoidExp = math.Exp(-sigmoidFactor)

const (
	// Margin for GC throughput error.
	errorMargin = 0.01
)

var stepSize = 1

func cleanup(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Collect needed runtime metrics.
	metrics.Read(samples)

	gcTime := 0.0

	// Aggregate runtime metrics for this GC cycle.
	for _, sample := range samples[:5] {
		gcTime += sample.Value.Float64()
	}

	userTime := samples[5].Value.Float64()
	totalTime := samples[6].Value.Float64()
	gcCount := samples[7].Value.Uint64()
	gcPercent := int(samples[8].Value.Uint64())

	// Calculate times of last GC cycle.
	deltaGCTime := gcTime - prevGCTime
	deltaUserTime := userTime - prevUserTime
	deltaTotalTime := totalTime - prevTotalTime

	prevGCTime = gcTime
	prevUserTime = userTime
	prevTotalTime = totalTime

	// Calculate GC CPU throughput and overhead.
	throughput := deltaUserTime / (deltaUserTime + deltaGCTime)
	overhead := deltaGCTime / deltaTotalTime

	// Calculate averages over past three GC cycles.
	averageThroughput := (throughput + prevThroughput + prev2Throughput) / 3
	prev2Throughput = prevThroughput
	prevThroughput = throughput

	averageOverhead := (overhead + prevOverhead + prev2Overhead) / 3
	prev2Overhead = prevOverhead
	prevOverhead = overhead

	// Calculate scaling factor and offset that clamp the sigmoid to [minGCThroughput, maxGCThroughput).
	scalingFactor := (maxGCThroughput - minGCThroughput) * (2 + 2*sigmoidExp) / (maxGCThroughput * (1 - sigmoidExp))
	scalingOffset := minGCThroughput - 0.5*scalingFactor*maxGCThroughput

	// Calculate the target GC throughput based on the real-time GC overhead using the sigmoid model.
	targetThroughput := (scalingFactor*maxGCThroughput)/(1+math.Exp(-sigmoidFactor*averageOverhead)) + scalingOffset

	// Calculate throughput error as the deviation between real-time GC throughput and target GC througput.
	throughputError := averageThroughput - targetThroughput

	// Calculate error magnitude as magnitude of throughput error minus the margin of error, clamped to be >= 0.
	errorMagnitude := max(0, math.Abs(throughputError)-errorMargin)

	newPercent := gcPercent

	// Only adjust GOGC if GC throughput error is larger then margin of error.
	if errorMagnitude > 0 {
		// Adapt step size based on the magnitude of the error.
		stepSize = int(math.Floor(10 * errorMagnitude))

		if stepSize >= 1 {
			if throughputError > 0 && gcPercent-stepSize > 0 {
				// Real-time GC throughput is higher than the target.
				// Decrease GOGC by one step.
				newPercent = gcPercent - stepSize
				debug.SetGCPercent(newPercent)
			} else if throughputError < 0 {
				// Real-time GC throughput is lower than the target.
				// Increase GOGC by one step.
				newPercent = gcPercent + stepSize
				debug.SetGCPercent(newPercent)
			}
		}
	}

	slog.InfoContext(ctx, "gc cycle",
		slog.Uint64("index", gcCount),
		slog.String("percent", fmt.Sprintf("%d%%", gcPercent)),
		slog.String("realtime_throughput", fmt.Sprintf("%.2f%%", 100*averageThroughput)),
		slog.String("realtime_overhead", fmt.Sprintf("%.4f%%", 100*averageOverhead)),
		slog.String("target_throughput", fmt.Sprintf("%.2f%%", 100*targetThroughput)),
		slog.String("throughput_error", fmt.Sprintf("%.2f%%", 100*throughputError)),
		slog.String("step_size", fmt.Sprintf("%d%%", stepSize)),
		slog.String("new_percent", fmt.Sprintf("%d%%", newPercent)),
	)

	// Add callback to next GC cycle.
	_ = runtime.AddCleanup(&obj{}, cleanup, ctx)
}
