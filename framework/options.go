package framework

import (
	"log/slog"
	"time"

	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/protocol/http1"
)

// Option configures a Server.
type Option func(*options)

type options struct {
	addr            string
	readTimeout     time.Duration
	writeTimeout    time.Duration
	idleTimeout     time.Duration
	shutdownTimeout time.Duration
	limits          http1.Limits

	redirectTrailingSlash  bool
	handleMethodNotAllowed bool
	disableKeepAlive       bool
	trustedProxies         []string

	logger    *slog.Logger
	transport network.Transport
}

func defaultOptions() options {
	return options{
		addr:                   ":8080",
		readTimeout:            30 * time.Second,
		writeTimeout:           30 * time.Second,
		idleTimeout:            120 * time.Second,
		shutdownTimeout:        10 * time.Second,
		limits:                 http1.DefaultLimits(),
		redirectTrailingSlash:  true,
		handleMethodNotAllowed: true,
		logger:                 slog.Default(),
	}
}

// WithHostPorts sets the listen address used by Run and Spin, for example ":8080".
func WithHostPorts(addr string) Option { return func(o *options) { o.addr = addr } }

// WithReadTimeout bounds reading one request once its first byte has arrived.
func WithReadTimeout(d time.Duration) Option { return func(o *options) { o.readTimeout = d } }

// WithWriteTimeout bounds writing one response.
func WithWriteTimeout(d time.Duration) Option { return func(o *options) { o.writeTimeout = d } }

// WithIdleTimeout closes keep-alive connections that stay silent for d.
func WithIdleTimeout(d time.Duration) Option { return func(o *options) { o.idleTimeout = d } }

// WithShutdownTimeout bounds how long Spin waits for in-flight requests after a signal.
func WithShutdownTimeout(d time.Duration) Option { return func(o *options) { o.shutdownTimeout = d } }

// WithMaxHeaderBytes limits the request line plus header fields (default 32 KiB).
func WithMaxHeaderBytes(n int) Option { return func(o *options) { o.limits.MaxHeaderBytes = n } }

// WithMaxHeaderCount limits the number of header fields (default 128).
func WithMaxHeaderCount(n int) Option { return func(o *options) { o.limits.MaxHeaderCount = n } }

// WithMaxBodyBytes limits the request body size (default 4 MiB).
func WithMaxBodyBytes(n int64) Option { return func(o *options) { o.limits.MaxBodyBytes = n } }

// WithRedirectTrailingSlash enables redirecting "/a/" to "/a" (or the reverse)
// when only the other form is registered. Enabled by default.
func WithRedirectTrailingSlash(enabled bool) Option {
	return func(o *options) { o.redirectTrailingSlash = enabled }
}

// WithHandleMethodNotAllowed enables answering 405 with an Allow header when the
// path exists for other methods. Enabled by default.
func WithHandleMethodNotAllowed(enabled bool) Option {
	return func(o *options) { o.handleMethodNotAllowed = enabled }
}

// WithDisableKeepAlive closes every connection after one response.
func WithDisableKeepAlive(disabled bool) Option {
	return func(o *options) { o.disableKeepAlive = disabled }
}

// WithLogger sets the logger used for framework diagnostics such as recovered panics.
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// WithTransport replaces the default transport.
func WithTransport(t network.Transport) Option { return func(o *options) { o.transport = t } }

// WithTrustedProxies lists the IP addresses and CIDR ranges of reverse proxies
// whose X-Forwarded-For, X-Real-IP and X-Forwarded-Proto headers may be
// believed. Without it, ClientIP is always the directly connected peer. New
// panics if an entry is neither an IP address nor a CIDR range.
func WithTrustedProxies(proxies ...string) Option {
	return func(o *options) { o.trustedProxies = append(o.trustedProxies, proxies...) }
}
