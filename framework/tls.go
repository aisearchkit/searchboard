package framework

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"

	"golang.org/x/net/http2"

	"github.com/searchboard/searchboard/framework/network/standard"
	"github.com/searchboard/searchboard/framework/protocol"
)

// WithTLS serves HTTPS with cfg. TLS needs the standard transport, so unless
// WithTransport says otherwise the server uses it instead of netpoll. The
// minimum protocol version defaults to TLS 1.2. Unless HTTP/2 is disabled with
// WithHTTP2(false), "h2" is offered next to "http/1.1" through ALPN.
func WithTLS(cfg *tls.Config) Option { return func(o *options) { o.tlsConfig = cfg } }

// WithHTTP2 turns HTTP/2 over TLS on or off. It is on by default whenever WithTLS
// is used with the default transport. Cleartext HTTP/2 (h2c) is not supported.
func WithHTTP2(enabled bool) Option { return func(o *options) { o.disableHTTP2 = !enabled } }

// maxH2Streams is the number of concurrent streams one HTTP/2 connection may open.
const maxH2Streams = 250

// setupTLS prepares the TLS config and, for the default transport, the HTTP/2
// hand-over. It runs once from New.
func (s *Server) setupTLS() {
	cfg := s.opts.tlsConfig
	if cfg == nil {
		return
	}

	cfg = cfg.Clone()
	if cfg.MinVersion == 0 {
		cfg.MinVersion = tls.VersionTLS12
	}
	h2 := !s.opts.disableHTTP2 && s.opts.transport == nil
	if len(cfg.NextProtos) == 0 {
		cfg.NextProtos = []string{"http/1.1"}
		if h2 {
			cfg.NextProtos = []string{"h2", "http/1.1"}
		}
	}
	s.tlsConfig = cfg

	if s.opts.transport != nil {
		return
	}
	var opts []standard.Option
	if h2 {
		s.h2 = &http2.Server{MaxConcurrentStreams: maxH2Streams, IdleTimeout: s.opts.idleTimeout}
		opts = append(opts, standard.WithProtocolHandler("h2", s.serveHTTP2))
	}
	s.opts.transport = standard.New(opts...)
}

// serveHTTP2 runs the HTTP/2 protocol on a TLS connection that negotiated "h2".
func (s *Server) serveHTTP2(ctx context.Context, conn net.Conn) {
	s.h2.ServeConn(conn, &http2.ServeConnOpts{
		Context:    ctx,
		Handler:    http.HandlerFunc(s.serveH2Request),
		BaseConfig: &http.Server{MaxHeaderBytes: s.opts.limits.MaxHeaderBytes},
	})
}

// serveH2Request adapts one HTTP/2 stream to the framework's request pipeline.
// Unlike HTTP/1.1, the request context is canceled when the client resets the
// stream or the connection drops.
func (s *Server) serveH2Request(w http.ResponseWriter, r *http.Request) {
	s.inflight.Add(1)
	defer s.inflight.Add(-1)

	if s.draining.Load() {
		http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.opts.limits.MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "could not read the request body", http.StatusBadRequest)
		}
		return
	}

	req := &protocol.Request{
		Method:     r.Method,
		Proto:      "HTTP/2.0",
		Host:       r.Host,
		RemoteAddr: r.RemoteAddr,
		Scheme:     "https",
	}
	if err := req.SetTarget(r.RequestURI); err != nil {
		http.Error(w, "malformed request target", http.StatusBadRequest)
		return
	}
	for name, values := range r.Header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	req.SetBody(body)

	resp := &protocol.Response{StatusCode: http.StatusOK}
	s.ServeRequest(r.Context(), req, resp)
	writeH2Response(w, r.Method, resp)
}

// hopByHop headers are invalid in HTTP/2 and must not be forwarded.
var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Connection": true,
	"Transfer-Encoding": true, "Upgrade": true,
}

func writeH2Response(w http.ResponseWriter, method string, resp *protocol.Response) {
	header := w.Header()
	resp.Header.VisitAll(func(key, value string) {
		if !hopByHop[key] && key != "Content-Length" {
			header.Add(key, value)
		}
	})

	status := resp.StatusCode
	if status < 200 || status > 999 {
		status = http.StatusInternalServerError
	}
	body := resp.Body()
	bodyAllowed := status != http.StatusNoContent && status != http.StatusNotModified

	if method == http.MethodHead {
		header.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		return
	}
	w.WriteHeader(status)
	if bodyAllowed && len(body) > 0 {
		_, _ = w.Write(body)
	}
}
