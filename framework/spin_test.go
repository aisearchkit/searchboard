//go:build !windows

package framework_test

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework"
)

func TestSpinShutsDownGracefullyOnSIGTERM(t *testing.T) {
	// Registering our own handler first guarantees a stray signal can never
	// terminate the test process, whatever the timing.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	s := newServer(framework.WithHostPorts("127.0.0.1:0"), framework.WithShutdownTimeout(3*time.Second))
	s.GET("/ping", func(_ context.Context, c *framework.Context) { c.String(200, "pong") })

	spun := make(chan error, 1)
	go func() { spun <- s.Spin() }()

	deadline := time.Now().Add(5 * time.Second)
	for s.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("server never started listening")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // let Spin install its signal handler

	resp, err := newClient().Get("http://" + s.Addr().String() + "/ping")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("server not serving before the signal: %v", err)
	}
	resp.Body.Close()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-spun:
		if err != nil {
			t.Fatalf("Spin = %v, want nil after a graceful signal shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Spin did not return after SIGTERM")
	}
}
