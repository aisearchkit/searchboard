//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

// Package netpoll implements network.Transport on top of the cloudwego/netpoll
// event loop. Connections are multiplexed over epoll or kqueue instead of
// dedicating a goroutine to every idle connection, and reads are zero-copy.
package netpoll

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/cloudwego/netpoll"

	"github.com/searchboard/searchboard/framework/network"
)

// Option configures a Transport.
type Option func(*Transport)

// WithIdleTimeout closes connections that receive no request for d. Zero disables it.
//
// netpoll's own idle timeout is a TCP keepalive period with one-second
// granularity, which never closes a healthy but silent connection, so this
// transport runs a timer per connection instead.
func WithIdleTimeout(d time.Duration) Option {
	return func(t *Transport) { t.idleTimeout = d }
}

// Transport is the netpoll transport. The zero value is not usable; call New.
type Transport struct {
	idleTimeout time.Duration

	mu      sync.Mutex
	loop    netpoll.EventLoop
	closing bool
}

var _ network.Transport = (*Transport)(nil)

// New returns a transport.
func New(opts ...Option) *Transport {
	t := &Transport{}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Listen implements network.Transport.
func (t *Transport) Listen(netw, address string) (net.Listener, error) {
	return netpoll.CreateListener(netw, address)
}

// idleKey is the context key of a connection's idleGuard.
type idleKey struct{}

// idleGuard closes a connection that stays idle. The timer runs while no
// request is being handled and is paused while one is.
type idleGuard struct {
	timer   *time.Timer
	timeout time.Duration
}

func (g *idleGuard) pause()  { g.timer.Stop() }
func (g *idleGuard) resume() { g.timer.Reset(g.timeout) }

// Serve implements network.Transport.
func (t *Transport) Serve(ln net.Listener, serve network.ServeFunc) error {
	handler := func(ctx context.Context, c netpoll.Connection) error {
		if guard, ok := ctx.Value(idleKey{}).(*idleGuard); ok {
			guard.pause()
			defer guard.resume()
		}

		err := serve(ctx, newConn(c))
		switch {
		case err == nil:
			return nil
		case errors.Is(err, network.ErrCloseConn):
			// Close here so netpoll does not report a normal close as an error.
			_ = c.Close()
			return nil
		default:
			return err
		}
	}

	opts := []netpoll.Option{}
	if t.idleTimeout > 0 {
		opts = append(opts, netpoll.WithOnPrepare(t.onPrepare))
	}
	loop, err := netpoll.NewEventLoop(handler, opts...)
	if err != nil {
		return err
	}

	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	t.loop = loop
	t.mu.Unlock()

	if err := loop.Serve(ln); err != nil && !t.isClosing() {
		return err
	}
	return nil
}

// onPrepare arms the idle timer of a new connection.
func (t *Transport) onPrepare(c netpoll.Connection) context.Context {
	guard := &idleGuard{timeout: t.idleTimeout}
	guard.timer = time.AfterFunc(t.idleTimeout, func() { _ = c.Close() })
	_ = c.AddCloseCallback(func(netpoll.Connection) error {
		guard.timer.Stop()
		return nil
	})
	return context.WithValue(context.Background(), idleKey{}, guard)
}

func (t *Transport) isClosing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closing
}

// Shutdown implements network.Transport.
func (t *Transport) Shutdown(ctx context.Context) error {
	t.mu.Lock()
	t.closing = true
	loop := t.loop
	t.mu.Unlock()

	if loop == nil {
		return nil
	}
	return loop.Shutdown(ctx)
}

// conn adapts a netpoll.Connection to network.Conn.
type conn struct {
	netpoll.Connection
	r netpoll.Reader
	w netpoll.Writer
}

func newConn(c netpoll.Connection) *conn {
	return &conn{Connection: c, r: c.Reader(), w: c.Writer()}
}

func (c *conn) Peek(n int) ([]byte, error)        { return c.r.Peek(n) }
func (c *conn) Skip(n int) error                  { return c.r.Skip(n) }
func (c *conn) Release() error                    { return c.r.Release() }
func (c *conn) Len() int                          { return c.r.Len() }
func (c *conn) WriteBinary(p []byte) (int, error) { return c.w.WriteBinary(p) }
func (c *conn) Flush() error                      { return c.w.Flush() }

func (c *conn) SetReadTimeout(d time.Duration) error  { return c.Connection.SetReadTimeout(d) }
func (c *conn) SetWriteTimeout(d time.Duration) error { return c.Connection.SetWriteTimeout(d) }
