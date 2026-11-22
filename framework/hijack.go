package framework

import (
	"errors"
	"net"

	"github.com/searchboard/searchboard/framework/network"
)

// ErrHijackUnsupported is returned by Context.Hijack when the connection cannot
// be taken over: the request came in over HTTP/2, the transport is netpoll, or
// the request was created by the ut test helper. Use WithTransport with the
// standard transport (framework/network/standard) to serve WebSocket.
var ErrHijackUnsupported = errors.New("framework: this connection cannot be hijacked; use the standard transport")

// Hijack takes over the underlying connection, for example to speak the
// WebSocket protocol. After a successful call the framework writes no response
// and does not close the connection: the caller owns it and must close it. Bytes
// the framework already buffered are delivered first by the returned net.Conn.
//
// A hijacked connection no longer counts as an in-flight request, so a graceful
// Shutdown does not wait for it. Use RegisterOnShutdown to close such
// connections when the server stops.
func (c *Context) Hijack() (net.Conn, error) {
	hijacker, ok := c.conn.(network.Hijacker)
	if !ok || c.conn == nil {
		return nil, ErrHijackUnsupported
	}
	if c.hijacked {
		return nil, errors.New("framework: connection already hijacked")
	}
	raw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	c.hijacked = true
	c.server.inflight.Add(-1)
	return raw, nil
}

// RegisterOnShutdown registers a function that Shutdown calls, each in its own
// goroutine, as soon as the server starts draining. Use it to close hijacked
// connections such as WebSockets.
func (s *Server) RegisterOnShutdown(fn func()) {
	s.mu.Lock()
	s.onShutdown = append(s.onShutdown, fn)
	s.mu.Unlock()
}
