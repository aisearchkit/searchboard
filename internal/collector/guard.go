// Package collector implements Agentless polling and the cluster pressure guard.
package collector

import (
	"sync"
	"time"
)

// Signal is one observation of the target cluster's health after a poll.
type Signal struct {
	StatusCode int
	Latency    time.Duration
	HeapPct    float64 // 0-100
	CPUPct     float64 // 0-100
}

// Guard is the pressure circuit-breaker: on 429, latency > 3s, Heap > 85% or
// CPU > 90% it backs off exponentially (base -> 60s -> 300s) and tells the
// scheduler to skip non-essential (heavy) collection.
type Guard struct {
	mu       sync.Mutex
	base     time.Duration
	level    int // 0 healthy, 1 -> 60s, 2 -> 300s
	maxLevel int
}

func NewGuard(base time.Duration) *Guard {
	return &Guard{base: base, maxLevel: 2}
}

func overloaded(s Signal) bool {
	return s.StatusCode == 429 ||
		s.Latency > 3*time.Second ||
		s.HeapPct > 85 ||
		s.CPUPct > 90
}

// Observe feeds a signal and returns the interval to wait before the next poll.
func (g *Guard) Observe(s Signal) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if overloaded(s) {
		if g.level < g.maxLevel {
			g.level++
		}
	} else {
		g.level = 0
	}
	switch g.level {
	case 1:
		return 60 * time.Second
	case 2:
		return 300 * time.Second
	default:
		return g.base
	}
}

// AllowHeavy reports whether heavy endpoints (_cat/segments, hot_threads) may be polled.
func (g *Guard) AllowHeavy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.level == 0
}
