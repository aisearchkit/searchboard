package collector

import (
	"testing"
	"time"
)

func TestGuardBackoffAndRecovery(t *testing.T) {
	g := NewGuard(15 * time.Second)
	if d := g.Observe(Signal{StatusCode: 200, Latency: time.Second, HeapPct: 50, CPUPct: 30}); d != 15*time.Second {
		t.Fatalf("healthy: %v", d)
	}
	if d := g.Observe(Signal{StatusCode: 429}); d != 60*time.Second {
		t.Fatalf("first overload: %v", d)
	}
	if g.AllowHeavy() {
		t.Fatal("heavy must be paused under pressure")
	}
	if d := g.Observe(Signal{Latency: 4 * time.Second}); d != 300*time.Second {
		t.Fatalf("second overload: %v", d)
	}
	if d := g.Observe(Signal{StatusCode: 200, HeapPct: 40, CPUPct: 20}); d != 15*time.Second {
		t.Fatalf("recovery: %v", d)
	}
}
