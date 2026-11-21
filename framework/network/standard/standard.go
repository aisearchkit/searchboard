// Package standard implements network.Transport on top of the Go standard
// library net package: one goroutine per connection and a bufio reader.
//
// It works on every platform and is the fallback where netpoll is unavailable.
package standard

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/searchboard/searchboard/framework/network"
)

const (
	defaultBufferSize = 64 << 10
	maxAcceptBackoff  = time.Second
)

// Option configures a Transport.
type Option func(*Transport)

// WithBufferSize sets the per-connection read and write buffer size. Peek
// cannot return more than this many bytes, so it also bounds the request header
// size this transport can read.
func WithBufferSize(n int) Option {
	return func(t *Transport) {
		if n > 0 {
			t.bufferSize = n
		}
	}
}

// Transport is the standard-library transport. The zero value is not usable; call New.
type Transport struct {
	bufferSize int

	mu      sync.Mutex
	ln      net.Listener
	conns   map[*conn]struct{}
	closing bool
}

var _ network.Transport = (*Transport)(nil)

// New returns a transport.
func New(opts ...Option) *Transport {
	t := &Transport{bufferSize: defaultBufferSize, conns: map[*conn]struct{}{}}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Listen implements network.Transport.
func (t *Transport) Listen(netw, address string) (net.Listener, error) {
	return net.Listen(netw, address)
}

// Serve implements network.Transport.
func (t *Transport) Serve(ln net.Listener, serve network.ServeFunc) error {
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	t.ln = ln
	t.mu.Unlock()

	var backoff time.Duration
	for {
		raw, err := ln.Accept()
		if err != nil {
			if t.isClosing() {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				backoff = nextBackoff(backoff)
				time.Sleep(backoff)
				continue
			}
			return err
		}
		backoff = 0
		go t.serveConn(raw, serve)
	}
}

func nextBackoff(current time.Duration) time.Duration {
	if current == 0 {
		return 5 * time.Millisecond
	}
	return min(current*2, maxAcceptBackoff)
}

func (t *Transport) serveConn(raw net.Conn, serve network.ServeFunc) {
	c := newConn(raw, t.bufferSize)
	if !t.track(c) {
		_ = raw.Close()
		return
	}
	defer func() {
		_ = recover() // a panicking handler must not take the process down
		t.untrack(c)
		_ = c.Close()
	}()

	ctx := context.Background()
	for {
		if err := serve(ctx, c); err != nil {
			return
		}
	}
}

func (t *Transport) track(c *conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closing {
		return false
	}
	t.conns[c] = struct{}{}
	return true
}

func (t *Transport) untrack(c *conn) {
	t.mu.Lock()
	delete(t.conns, c)
	t.mu.Unlock()
}

func (t *Transport) isClosing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closing
}

// Shutdown implements network.Transport. It closes the listener and every connection.
func (t *Transport) Shutdown(context.Context) error {
	t.mu.Lock()
	t.closing = true
	ln := t.ln
	conns := make([]*conn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}

// conn adapts a net.Conn to network.Conn.
type conn struct {
	raw          net.Conn
	r            *bufio.Reader
	w            *bufio.Writer
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func newConn(raw net.Conn, bufferSize int) *conn {
	return &conn{
		raw: raw,
		r:   bufio.NewReaderSize(raw, bufferSize),
		w:   bufio.NewWriterSize(raw, bufferSize),
	}
}

func (c *conn) Peek(n int) ([]byte, error) {
	// Only arm the deadline when the call can actually block.
	if c.r.Buffered() < n {
		c.armReadDeadline()
	}
	return c.r.Peek(n)
}

func (c *conn) armReadDeadline() {
	if c.readTimeout > 0 {
		_ = c.raw.SetReadDeadline(time.Now().Add(c.readTimeout))
	} else {
		_ = c.raw.SetReadDeadline(time.Time{})
	}
}

func (c *conn) Skip(n int) error {
	_, err := c.r.Discard(n)
	return err
}

func (c *conn) Release() error { return nil }
func (c *conn) Len() int       { return c.r.Buffered() }

func (c *conn) WriteBinary(p []byte) (int, error) { return c.w.Write(p) }

func (c *conn) Flush() error {
	if c.writeTimeout > 0 {
		_ = c.raw.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}
	return c.w.Flush()
}

func (c *conn) RemoteAddr() net.Addr { return c.raw.RemoteAddr() }

func (c *conn) SetReadTimeout(d time.Duration) error {
	c.readTimeout = d
	return nil
}

func (c *conn) SetWriteTimeout(d time.Duration) error {
	c.writeTimeout = d
	return nil
}

func (c *conn) Close() error { return c.raw.Close() }
