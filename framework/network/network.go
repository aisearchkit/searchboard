// Package network defines the transport abstraction of the framework.
//
// The protocol layer only talks to the small Reader and Writer interfaces below,
// so the same HTTP/1.1 implementation runs on top of the standard library
// (package standard) and on the cloudwego/netpoll event loop (package netpoll).
package network

import (
	"context"
	"errors"
	"net"
	"time"
)

// ErrCloseConn is returned by a ServeFunc to ask the transport to close the
// connection normally. It is not reported as a failure.
var ErrCloseConn = errors.New("network: close connection")

// Reader is a zero-copy oriented buffered reader.
//
// Peek and Skip follow the netpoll semantics: Peek(n) blocks until n bytes are
// buffered (or the read timeout fires, or the peer closes) and returns exactly
// n bytes. The returned slice is only valid until the next Skip or Release.
type Reader interface {
	// Peek returns the next n bytes without consuming them.
	Peek(n int) ([]byte, error)
	// Skip consumes n bytes that were already buffered.
	Skip(n int) error
	// Release tells the reader that previously peeked memory is no longer used.
	Release() error
	// Len returns the number of bytes currently buffered.
	Len() int
}

// Writer is a buffered writer. Buffers passed to WriteBinary must stay
// unmodified until Flush returns, because implementations may not copy them.
type Writer interface {
	WriteBinary(p []byte) (int, error)
	Flush() error
}

// Conn is one client connection.
type Conn interface {
	Reader
	Writer
	RemoteAddr() net.Addr
	// SetReadTimeout bounds each blocking read. Zero disables the timeout.
	SetReadTimeout(d time.Duration) error
	// SetWriteTimeout bounds Flush. Zero disables the timeout.
	SetWriteTimeout(d time.Duration) error
	Close() error
}

// ServeFunc handles available data on a connection.
//
// Contract: it reads and answers one or more requests, returns nil when it is
// done for now and the connection should stay open, and returns an error
// (ErrCloseConn for a normal close) when the connection must be closed.
// Transports call it again when more data may be available.
type ServeFunc func(ctx context.Context, conn Conn) error

// Transport accepts connections and feeds them to a ServeFunc.
type Transport interface {
	// Listen opens a listening socket suitable for this transport.
	Listen(network, address string) (net.Listener, error)
	// Serve accepts connections on ln until Shutdown is called. It returns nil
	// after a Shutdown and the accept error otherwise.
	Serve(ln net.Listener, serve ServeFunc) error
	// Shutdown stops accepting connections and closes the existing ones. Callers
	// that want a graceful stop must wait for in-flight requests first.
	Shutdown(ctx context.Context) error
}

// Hijacker is implemented by connections whose ownership can be taken over, for
// example to switch to the WebSocket protocol. After Hijack the transport no
// longer reads from, writes to or closes the connection. Bytes the transport
// had already buffered are returned by the net.Conn's Read before new data.
type Hijacker interface {
	Hijack() (net.Conn, error)
}

// Secure is implemented by connections that know whether they use TLS.
type Secure interface {
	IsTLS() bool
}
