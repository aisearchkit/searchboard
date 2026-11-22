// Package standard implements network.Transport on top of the Go standard
// library net package: one goroutine per connection and a bufio reader.
//
// It works on every platform, supports TLS, connection hijacking and protocol
// hand-over by ALPN (used for HTTP/2), and is the fallback where netpoll is
// unavailable.
package standard

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/searchboard/searchboard/framework/network"
)

const (
	defaultBufferSize       = 64 << 10
	defaultHandshakeTimeout = 10 * time.Second
	maxAcceptBackoff        = time.Second
)

// ProtocolHandler takes over a connection after a TLS handshake negotiated the
// matching ALPN protocol. It owns the connection until it returns; the
// transport then closes it.
type ProtocolHandler func(ctx context.Context, conn net.Conn)

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

// WithHandshakeTimeout bounds the TLS handshake of a new connection.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(t *Transport) { t.handshakeTimeout = d }
}

// WithProtocolHandler routes TLS connections that negotiated the ALPN protocol
// proto (for example "h2") to handler instead of the HTTP/1.1 ServeFunc.
func WithProtocolHandler(proto string, handler ProtocolHandler) Option {
	return func(t *Transport) { t.protocols[proto] = handler }
}

// Transport is the standard-library transport. The zero value is not usable; call New.
type Transport struct {
	bufferSize       int
	handshakeTimeout time.Duration
	protocols        map[string]ProtocolHandler

	mu      sync.Mutex
	ln      net.Listener
	conns   map[io.Closer]struct{}
	closing bool
}

var _ network.Transport = (*Transport)(nil)

// New returns a transport.
func New(opts ...Option) *Transport {
	t := &Transport{
		bufferSize:       defaultBufferSize,
		handshakeTimeout: defaultHandshakeTimeout,
		protocols:        map[string]ProtocolHandler{},
		conns:            map[io.Closer]struct{}{},
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Listen implements network.Transport.
func (t *Transport) Listen(netw, address string) (net.Listener, error) {
	return net.Listen(netw, address)
}

// Serve implements network.Transport. A listener from tls.NewListener makes it
// serve TLS; handshakes happen in the connection's own goroutine, so a slow
// client cannot block Accept.
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
	defer func() { _ = recover() }() // a panicking handler must not take the process down

	ctx := context.Background()

	if tlsConn, ok := raw.(*tls.Conn); ok {
		if !t.track(tlsConn) {
			_ = raw.Close()
			return
		}
		defer func() {
			t.untrack(tlsConn)
			_ = tlsConn.Close()
		}()

		_ = tlsConn.SetDeadline(time.Now().Add(t.handshakeTimeout))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return
		}
		_ = tlsConn.SetDeadline(time.Time{})

		if handler, ok := t.protocols[tlsConn.ConnectionState().NegotiatedProtocol]; ok {
			handler(ctx, tlsConn)
			return
		}
	}

	c := newConn(raw, t.bufferSize)
	// A hijacked connection is owned by someone else: Close becomes a no-op, so
	// tracking it only costs a map entry until serveConn returns.
	if _, isTLS := raw.(*tls.Conn); !isTLS && !t.track(c) {
		_ = raw.Close()
		return
	}
	defer func() {
		t.untrack(c)
		_ = c.Close()
	}()

	for {
		if err := serve(ctx, c); err != nil {
			return
		}
	}
}

func (t *Transport) track(c io.Closer) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closing {
		return false
	}
	t.conns[c] = struct{}{}
	return true
}

func (t *Transport) untrack(c io.Closer) {
	t.mu.Lock()
	delete(t.conns, c)
	t.mu.Unlock()
}

func (t *Transport) isClosing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closing
}

// Shutdown implements network.Transport. It closes the listener and every
// connection except hijacked ones, which belong to their new owner.
func (t *Transport) Shutdown(context.Context) error {
	t.mu.Lock()
	t.closing = true
	ln := t.ln
	conns := make([]io.Closer, 0, len(t.conns))
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
	isTLS        bool
	readTimeout  time.Duration
	writeTimeout time.Duration

	mu       sync.Mutex
	hijacked bool
}

var (
	_ network.Hijacker = (*conn)(nil)
	_ network.Secure   = (*conn)(nil)
)

func newConn(raw net.Conn, bufferSize int) *conn {
	_, isTLS := raw.(*tls.Conn)
	return &conn{
		raw:   raw,
		r:     bufio.NewReaderSize(raw, bufferSize),
		w:     bufio.NewWriterSize(raw, bufferSize),
		isTLS: isTLS,
	}
}

func (c *conn) IsTLS() bool { return c.isTLS }

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

// Hijack implements network.Hijacker. Deadlines are cleared; the returned
// connection first yields whatever the buffered reader already holds.
func (c *conn) Hijack() (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hijacked {
		return nil, errors.New("standard: connection already hijacked")
	}
	c.hijacked = true
	_ = c.raw.SetDeadline(time.Time{})
	return &hijackedConn{Conn: c.raw, reader: c.r}, nil
}

// Close closes the connection unless it was hijacked.
func (c *conn) Close() error {
	c.mu.Lock()
	hijacked := c.hijacked
	c.mu.Unlock()
	if hijacked {
		return nil
	}
	return c.raw.Close()
}

// hijackedConn reads through the original buffered reader so bytes that
// arrived together with the HTTP request are not lost.
type hijackedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (h *hijackedConn) Read(p []byte) (int, error) { return h.reader.Read(p) }
