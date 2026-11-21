package framework_test

import (
	"context"
	"testing"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/ut"
)

// The ut helper connects every request from 192.0.2.1.
func clientIPServer(opts ...framework.Option) *framework.Server {
	s := newServer(opts...)
	s.GET("/ip", func(_ context.Context, c *framework.Context) {
		c.String(200, "%s %s", c.ClientIP(), c.Scheme())
	})
	return s
}

func clientIP(s *framework.Server, headers ...ut.Header) string {
	return ut.PerformRequest(s, "GET", "/ip", nil, headers...).BodyString()
}

func TestClientIPWithoutTrustedProxiesIgnoresForwardingHeaders(t *testing.T) {
	s := clientIPServer()
	got := clientIP(s,
		ut.Header{Key: "X-Forwarded-For", Value: "203.0.113.9"},
		ut.Header{Key: "X-Real-Ip", Value: "203.0.113.10"},
		ut.Header{Key: "X-Forwarded-Proto", Value: "https"})
	if got != "192.0.2.1 http" {
		t.Fatalf("got %q: forwarding headers must not be trusted by default", got)
	}
}

func TestClientIPBehindTrustedProxy(t *testing.T) {
	s := clientIPServer(framework.WithTrustedProxies("192.0.2.1", "10.0.0.0/8", "fd00::/8"))

	tests := []struct {
		name    string
		headers []ut.Header
		want    string
	}{
		{"no forwarding headers", nil, "192.0.2.1 http"},
		{"single hop", []ut.Header{{Key: "X-Forwarded-For", Value: "203.0.113.9"}}, "203.0.113.9 http"},
		{"trusted hops are skipped", []ut.Header{{Key: "X-Forwarded-For", Value: "203.0.113.9, 10.1.1.1, 10.2.2.2"}}, "203.0.113.9 http"},
		{"spoofed leftmost entry is ignored", []ut.Header{{Key: "X-Forwarded-For", Value: "1.2.3.4, 203.0.113.9"}}, "203.0.113.9 http"},
		{"several header lines", []ut.Header{{Key: "X-Forwarded-For", Value: "1.2.3.4"}, {Key: "X-Forwarded-For", Value: "203.0.113.9"}}, "203.0.113.9 http"},
		{"ipv6 client", []ut.Header{{Key: "X-Forwarded-For", Value: "2001:db8::1"}}, "2001:db8::1 http"},
		{"all hops trusted gives the leftmost", []ut.Header{{Key: "X-Forwarded-For", Value: "10.9.9.9, 10.1.1.1"}}, "10.9.9.9 http"},
		{"malformed entry falls back to the peer", []ut.Header{{Key: "X-Forwarded-For", Value: "203.0.113.9, not-an-ip"}}, "192.0.2.1 http"},
		{"x-real-ip without x-forwarded-for", []ut.Header{{Key: "X-Real-Ip", Value: "198.51.100.7"}}, "198.51.100.7 http"},
		{"invalid x-real-ip", []ut.Header{{Key: "X-Real-Ip", Value: "junk"}}, "192.0.2.1 http"},
		{"forwarded proto https", []ut.Header{{Key: "X-Forwarded-Proto", Value: "HTTPS"}}, "192.0.2.1 https"},
		{"unknown forwarded proto", []ut.Header{{Key: "X-Forwarded-Proto", Value: "gopher"}}, "192.0.2.1 http"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientIP(s, tt.headers...); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestForwardingHeadersFromUntrustedPeerAreIgnored(t *testing.T) {
	s := clientIPServer(framework.WithTrustedProxies("10.0.0.0/8")) // the peer, 192.0.2.1, is not trusted
	got := clientIP(s,
		ut.Header{Key: "X-Forwarded-For", Value: "203.0.113.9"},
		ut.Header{Key: "X-Forwarded-Proto", Value: "https"})
	if got != "192.0.2.1 http" {
		t.Fatalf("got %q: an untrusted peer must not be able to spoof its address", got)
	}
}

func TestInvalidTrustedProxyPanics(t *testing.T) {
	for _, bad := range []string{"nonsense", "10.0.0.0/33", "300.1.1.1", ""} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("WithTrustedProxies(%q) did not panic", bad)
				}
			}()
			framework.New(framework.WithTrustedProxies(bad))
		}()
	}
}
