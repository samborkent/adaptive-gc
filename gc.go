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

// AutoAdapt starts to automatically adapt the GOGC percentage until the context is cancelled.
func AutoAdapt(ctx context.Context) {
	if callOnce.Load() {
		panic("AutoAdapt may only be called once")
	}

	_ = runtime.AddCleanup(&obj{}, cleanup, ctx)

	callOnce.Store(true)
}

type obj struct {
	_ *struct{}
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
	errorMargin = 0.02
	// Minimum and maximum GC throughput used for scaling the sigmoid model.
	minThroughput = 0.50
	maxThroughput = 0.95
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

	thisGCTime := 0.0

	// Aggregate runtime metrics for this GC cycle.
	for _, sample := range samples[:5] {
		thisGCTime += sample.Value.Float64()
	}

	thisUserTime := samples[5].Value.Float64()
	thisTotalTime := samples[6].Value.Float64()
	gcCount := samples[7].Value.Uint64()
	gcPercent := int(samples[8].Value.Uint64())

	// Calculate times of last GC cycle.
	deltaGCTime := thisGCTime - prevGCTime
	deltaUserTime := thisUserTime - prevUserTime
	deltaTotalTime := thisTotalTime - prevTotalTime

	prevGCTime = thisGCTime
	prevUserTime = thisUserTime
	prevTotalTime = thisTotalTime

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

	// Calculate scaling factor and offset that clamp the sigmoid to [minGC, maxGC).
	scalingFactor := (maxThroughput - minThroughput) * (2 + 2*sigmoidExp) / (maxThroughput * (1 - sigmoidExp))
	scalingOffset := minThroughput - 0.5*scalingFactor*maxThroughput

	// Calculate the target GC throughput based on the real-time GC overhead using the sigmoid model.
	targetThroughput := (scalingFactor*maxThroughput)/(1+math.Exp(-sigmoidFactor*averageOverhead)) + scalingOffset

	// Calculate throughput error as the deviation between real-time GC throughput and target GC througput.
	throughputError := averageThroughput - targetThroughput
	errorMagnitude := math.Abs(throughputError)

	// Adapt step size based on the magnitude of the error.
	stepSize = int(math.Floor(50 * errorMagnitude))

	newPercent := gcPercent

	// Only adjust GOGC if GC throughput error is larger then margin of error and the step size is at least 1.
	if errorMagnitude > errorMargin && stepSize >= 1 {
		if throughputError > 0 && gcPercent-stepSize >= 0 {
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

	slog.InfoContext(ctx, "gc cycle",
		slog.Uint64("index", gcCount),
		slog.String("percent", fmt.Sprintf("%d%%", gcPercent)),
		slog.String("realtime_throughput", fmt.Sprintf("%.2f%%", 100*averageThroughput)),
		slog.String("realtime_overhead", fmt.Sprintf("%.2f%%", 100*averageOverhead)),
		slog.String("target_throughput", fmt.Sprintf("%.2f%%", 100*targetThroughput)),
		slog.String("throughput_error", fmt.Sprintf("%.2f%%", 100*throughputError)),
		slog.String("step_size", fmt.Sprintf("%d%%", stepSize)),
		slog.String("new_percent", fmt.Sprintf("%d%%", newPercent)),
	)

	// Add callback to next GC cycle.
	_ = runtime.AddCleanup(&obj{}, cleanup, ctx)
}
