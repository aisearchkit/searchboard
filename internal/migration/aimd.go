// Package migration is the next-gen reindex engine:
// Reader (PIT/search_after/slice) -> DiskQueue (Pebble/WAL) -> AIMD Writer -> target.
package migration

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// Feedback is a congestion signal derived from a bulk response / side-probe.
type Feedback struct {
	TooManyRequests bool    // HTTP 429 or EsRejectedExecutionException
	WriteQueueFull  bool    // thread_pool.write.queue backlog
	HeapPct         float64 // target node heap, 0-100
	BulkP99         time.Duration
}

func (f Feedback) congested() bool {
	return f.TooManyRequests || f.WriteQueueFull || f.HeapPct > 85
}

func (f Feedback) healthy() bool {
	return !f.congested() && f.HeapPct < 75
}

// Limits bounds the controller.
type Limits struct {
	MinConcurrency, MaxConcurrency int
	MinBatchDocs, MaxBatchDocs     int
	AddConcurrency                 int // additive step
	AddBatchDocs                   int
	MaxBackoff                     time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MinConcurrency: 1, MaxConcurrency: 32,
		MinBatchDocs: 100, MaxBatchDocs: 10000,
		AddConcurrency: 1, AddBatchDocs: 200,
		MaxBackoff: 30 * time.Second,
	}
}

// AIMD adjusts concurrency and batch size: additive increase while healthy,
// multiplicative decrease (x0.5) with jittered exponential backoff on congestion.
type AIMD struct {
	mu          sync.Mutex
	lim         Limits
	concurrency int
	batchDocs   int
	strikes     int
	rng         *rand.Rand
}

func NewAIMD(lim Limits) *AIMD {
	return &AIMD{
		lim:         lim,
		concurrency: lim.MinConcurrency,
		batchDocs:   lim.MinBatchDocs,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (a *AIMD) State() (concurrency, batchDocs int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.concurrency, a.batchDocs
}

// Observe applies feedback and returns how long the writer should pause.
func (a *AIMD) Observe(f Feedback) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case f.congested():
		a.strikes++
		a.concurrency = max(a.lim.MinConcurrency, a.concurrency/2)
		a.batchDocs = max(a.lim.MinBatchDocs, a.batchDocs/2)
		return a.backoffLocked()
	case f.healthy():
		a.strikes = 0
		a.concurrency = min(a.lim.MaxConcurrency, a.concurrency+a.lim.AddConcurrency)
		a.batchDocs = min(a.lim.MaxBatchDocs, a.batchDocs+a.lim.AddBatchDocs)
	}
	return 0
}

func (a *AIMD) backoffLocked() time.Duration {
	d := time.Duration(math.Min(
		float64(a.lim.MaxBackoff),
		float64(500*time.Millisecond)*math.Pow(2, float64(a.strikes-1)),
	))
	jitter := time.Duration(a.rng.Int63n(int64(d/4) + 1))
	return d + jitter
}
