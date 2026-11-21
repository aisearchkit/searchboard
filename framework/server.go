package framework

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/protocol"
	"github.com/searchboard/searchboard/framework/protocol/http1"
	"github.com/searchboard/searchboard/framework/route"
)

// shutdownPollInterval is how often Shutdown checks whether requests are still in flight.
const shutdownPollInterval = 5 * time.Millisecond

// Server is an HTTP/1.1 server. Create it with New, register routes, then call
// Run, Spin or Serve. Routes must be registered before the server starts.
type Server struct {
	RouterGroup

	opts      options
	trees     *route.Tree[HandlersChain]
	transport network.Transport
	pool      sync.Pool
	trusted   []*net.IPNet

	noRoute  HandlersChain
	noMethod HandlersChain
	chain404 HandlersChain
	chain405 HandlersChain

	inflight atomic.Int64
	draining atomic.Bool

	mu   sync.Mutex
	addr net.Addr
}

// New creates a server.
func New(opts ...Option) *Server {
	s := &Server{opts: defaultOptions(), trees: route.NewTree[HandlersChain]()}
	for _, opt := range opts {
		opt(&s.opts)
	}

	trusted, err := parseTrustedProxies(s.opts.trustedProxies)
	if err != nil {
		panic(err.Error())
	}
	s.trusted = trusted

	s.transport = s.opts.transport
	if s.transport == nil {
		s.transport = newDefaultTransport(s.opts.idleTimeout)
	}
	s.RouterGroup = RouterGroup{basePath: "/", server: s}
	s.pool.New = func() any { return newContext(s) }
	s.rebuildFallbacks()
	return s
}

// Use adds middleware to the server's root group. See RouterGroup.Use.
func (s *Server) Use(middleware ...HandlerFunc) *Server {
	s.RouterGroup.Use(middleware...)
	s.rebuildFallbacks()
	return s
}

// NoRoute sets the handlers that answer requests matching no route. Server-wide
// middleware runs before them. The default answers 404 in plain text.
func (s *Server) NoRoute(handlers ...HandlerFunc) {
	s.noRoute = handlers
	s.rebuildFallbacks()
}

// NoMethod sets the handlers that answer 405 when the path exists for other
// methods. The Allow header is already set. The default answers 405 in plain text.
func (s *Server) NoMethod(handlers ...HandlerFunc) {
	s.noMethod = handlers
	s.rebuildFallbacks()
}

func (s *Server) rebuildFallbacks() {
	notFound, notAllowed := s.noRoute, s.noMethod
	if len(notFound) == 0 {
		notFound = HandlersChain{func(_ context.Context, c *Context) { c.String(http.StatusNotFound, "404 page not found") }}
	}
	if len(notAllowed) == 0 {
		notAllowed = HandlersChain{func(_ context.Context, c *Context) { c.String(http.StatusMethodNotAllowed, "405 method not allowed") }}
	}
	s.chain404 = combine(s.handlers, notFound)
	s.chain405 = combine(s.handlers, notAllowed)
}

// Routes lists the registered routes.
func (s *Server) Routes() []route.Info { return s.trees.Routes() }

// Addr returns the address the server listens on, or nil before Listen.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Listen opens the listening socket for the configured address. Use it with
// Serve when the caller needs the bound address, for example with port 0.
func (s *Server) Listen() (net.Listener, error) {
	ln, err := s.transport.Listen("tcp", s.opts.addr)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.addr = ln.Addr()
	s.mu.Unlock()
	return ln, nil
}

// Serve accepts connections on ln until Shutdown. It returns nil after a Shutdown.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.addr = ln.Addr()
	s.mu.Unlock()
	return s.transport.Serve(ln, s.serveConn)
}

// Run listens on the configured address and serves until Shutdown or a failure.
func (s *Server) Run() error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Spin runs the server until SIGINT or SIGTERM, then shuts down gracefully,
// waiting up to the configured shutdown timeout for in-flight requests.
func (s *Server) Spin() error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ln) }()

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serveErr:
		return err
	case <-signals.Done():
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.opts.shutdownTimeout)
	defer cancel()
	shutdownErr := s.Shutdown(ctx)
	return errors.Join(shutdownErr, <-serveErr)
}

// Shutdown stops the server gracefully. New requests are refused with 503 and
// existing connections close after their current response. It waits until no
// request is in flight or ctx ends, then closes the transport. The transport is
// closed even when ctx ends first, and ctx's error is then returned.
func (s *Server) Shutdown(ctx context.Context) error {
	s.draining.Store(true)

	var waitErr error
	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()
wait:
	for s.inflight.Load() > 0 {
		select {
		case <-ctx.Done():
			waitErr = ctx.Err()
			break wait
		case <-ticker.C:
		}
	}
	return errors.Join(waitErr, s.transport.Shutdown(ctx))
}

// ServeRequest runs req through the router and middleware and fills resp,
// without any network involved. It exists for tests; see package ut.
func (s *Server) ServeRequest(ctx context.Context, req *protocol.Request, resp *protocol.Response) {
	c := s.pool.Get().(*Context)
	c.reset()
	c.Request, c.Response = req, resp
	if resp.StatusCode == 0 {
		resp.StatusCode = http.StatusOK
	}
	s.handle(ctx, c)
	c.reset() // restores the pooled request and response
	s.pool.Put(c)
}

// serveConn is the network.ServeFunc: it answers every request that is
// available on conn and keeps the connection open for more.
func (s *Server) serveConn(ctx context.Context, conn network.Conn) error {
	for {
		// Wait for the first byte of the next request under the idle timeout,
		// then give the rest of the request the read timeout.
		_ = conn.SetReadTimeout(s.opts.idleTimeout)
		if _, err := conn.Peek(1); err != nil {
			return network.ErrCloseConn
		}
		_ = conn.SetReadTimeout(s.opts.readTimeout)

		keepAlive, err := s.serveRequest(ctx, conn)
		if err != nil || !keepAlive {
			return network.ErrCloseConn
		}
		if conn.Len() == 0 {
			return nil // the transport calls us again when more data arrives
		}
	}
}

// serveRequest reads, handles and answers one request.
func (s *Server) serveRequest(ctx context.Context, conn network.Conn) (keepAlive bool, err error) {
	s.inflight.Add(1)
	defer s.inflight.Add(-1)

	c := s.pool.Get().(*Context)
	c.reset()
	defer func() {
		c.reset()
		s.pool.Put(c)
	}()
	req := c.Request

	if err := http1.ReadHeader(conn, req, s.opts.limits); err != nil {
		replyProtocolError(conn, err)
		return false, err
	}
	req.RemoteAddr = conn.RemoteAddr().String()
	req.Scheme = "http"

	if http1.ExpectsContinue(req) {
		if err := http1.WriteContinue(conn); err != nil {
			return false, err
		}
	}
	if err := http1.ReadBody(conn, req, s.opts.limits); err != nil {
		replyProtocolError(conn, err)
		return false, err
	}

	if s.draining.Load() {
		_ = http1.WriteError(conn, http.StatusServiceUnavailable, "server is shutting down")
		return false, nil
	}

	s.handle(ctx, c)

	keepAlive = http1.KeepAlive(req) && !s.opts.disableKeepAlive && !s.draining.Load()
	_ = conn.SetWriteTimeout(s.opts.writeTimeout)
	err = http1.WriteResponse(conn, c.Response, http1.WriteOptions{
		KeepAlive: keepAlive,
		HeadOnly:  req.Method == http.MethodHead,
	})
	return keepAlive && err == nil, err
}

// replyProtocolError answers a malformed request when the error maps to an HTTP
// status. I/O errors such as timeouts and resets get no response.
func replyProtocolError(conn network.Conn, err error) {
	if status := http1.StatusOf(err); status != 0 {
		_ = http1.WriteError(conn, status, err.Error())
	}
}

// handle runs the request and converts a panic that no middleware recovered
// into a 500, so one bad handler cannot kill the connection loop.
func (s *Server) handle(ctx context.Context, c *Context) {
	defer func() {
		if r := recover(); r != nil {
			s.opts.logger.Error("panic in handler",
				"panic", r, "method", c.Request.Method, "path", c.Request.Path, "stack", string(debug.Stack()))
			c.Response.Reset()
			c.String(http.StatusInternalServerError, "internal server error")
		}
	}()
	s.dispatch(ctx, c)
}

func (s *Server) dispatch(ctx context.Context, c *Context) {
	req := c.Request

	if chain, fullPath, ok := s.lookup(req.Method, req.Path, c); ok {
		c.handlers, c.fullPath = chain, fullPath
		c.Next(ctx)
		return
	}

	if s.opts.redirectTrailingSlash && s.redirectTrailingSlash(c) {
		return
	}

	c.Params = c.Params[:0]
	if s.opts.handleMethodNotAllowed {
		if allowed := s.trees.Allowed(req.Path); len(allowed) > 0 {
			c.Response.Header.Set("Allow", strings.Join(allowed, ", "))
			c.Status(http.StatusMethodNotAllowed)
			c.handlers = s.chain405
			c.Next(ctx)
			return
		}
	}

	c.Status(http.StatusNotFound)
	c.handlers = s.chain404
	c.Next(ctx)
}

// lookup finds the chain for a request. A HEAD request falls back to the GET
// route; the body is dropped when the response is written.
func (s *Server) lookup(method, path string, c *Context) (HandlersChain, string, bool) {
	c.Params = c.Params[:0]
	if chain, fullPath, ok := s.trees.Find(method, path, &c.Params); ok {
		return chain, fullPath, true
	}
	if method == http.MethodHead {
		c.Params = c.Params[:0]
		return s.trees.Find(http.MethodGet, path, &c.Params)
	}
	return nil, "", false
}

// redirectTrailingSlash redirects to the same path with the trailing slash
// added or removed when that form is routable. It reports whether it redirected.
func (s *Server) redirectTrailingSlash(c *Context) bool {
	req := c.Request
	if req.Path == "/" || req.Method == http.MethodConnect {
		return false
	}

	alternative := strings.TrimSuffix(req.Path, "/")
	if alternative == req.Path {
		alternative += "/"
	}
	if _, _, ok := s.lookup(req.Method, alternative, c); !ok {
		return false
	}

	// Collapse leading slashes so the Location can never become a
	// protocol-relative URL such as "//example.com/".
	location := "/" + strings.TrimLeft(alternative, "/")
	if req.RawQuery != "" {
		location += "?" + req.RawQuery
	}
	code := http.StatusPermanentRedirect // keeps the method and body
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		code = http.StatusMovedPermanently
	}
	c.Params = c.Params[:0]
	c.Redirect(code, location)
	return true
}
