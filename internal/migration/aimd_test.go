package migration

import (
	"testing"
	"time"
)

func TestAIMD(t *testing.T) {
	a := NewAIMD(DefaultLimits())
	for i := 0; i < 10; i++ {
		a.Observe(Feedback{HeapPct: 50})
	}
	c, b := a.State()
	if c != 11 || b != 2100 {
		t.Fatalf("additive increase: c=%d b=%d", c, b)
	}
	pause := a.Observe(Feedback{TooManyRequests: true})
	c2, b2 := a.State()
	if c2 != c/2 || b2 != b/2 {
		t.Fatalf("multiplicative decrease: c=%d b=%d", c2, b2)
	}
	if pause < 500*time.Millisecond {
		t.Fatalf("expected backoff, got %v", pause)
	}
	// heap between 75 and 85: hold steady
	a.Observe(Feedback{HeapPct: 80})
	if c3, _ := a.State(); c3 != c2 {
		t.Fatalf("expected hold, got %d", c3)
	}
}
