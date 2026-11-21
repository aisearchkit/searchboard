package middleware_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/middleware"
	"github.com/searchboard/searchboard/framework/ut"
)

func quietServer() *framework.Server {
	return framework.New(framework.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
}

func ok(_ context.Context, c *framework.Context) { c.String(200, "ok") }

func TestRecovery(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	s := quietServer()
	s.Use(middleware.RequestID(), middleware.Recovery(logger))
	s.GET("/boom", func(_ context.Context, c *framework.Context) {
		c.SetHeader("X-Half-Written", "1")
		panic("secret internal detail")
	})
	s.GET("/ok", ok)

	got := ut.PerformRequest(s, "GET", "/boom", nil, ut.Header{Key: "X-Request-Id", Value: "req-1"})
	if got.Status != 500 || got.BodyString() != `{"error":"internal server error"}` {
		t.Fatalf("response = %d %q", got.Status, got.BodyString())
	}
	if got.Header.Has("X-Half-Written") {
		t.Error("half-written headers must be dropped")
	}
	if got.Header.Get("X-Request-Id") != "req-1" {
		t.Errorf("request id lost: %q", got.Header.Get("X-Request-Id"))
	}
	if strings.Contains(got.BodyString(), "secret") {
		t.Error("panic value leaked to the client")
	}
	if !strings.Contains(logs.String(), "secret internal detail") || !strings.Contains(logs.String(), "req-1") || !strings.Contains(logs.String(), "stack=") {
		t.Errorf("log lacks panic details: %s", logs.String())
	}

	if got := ut.PerformRequest(s, "GET", "/ok", nil); got.Status != 200 {
		t.Errorf("next request = %d", got.Status)
	}
}

func TestRequestID(t *testing.T) {
	s := quietServer()
	s.Use(middleware.RequestID())
	s.GET("/x", func(_ context.Context, c *framework.Context) { c.String(200, "%s", middleware.GetRequestID(c)) })

	generated := ut.PerformRequest(s, "GET", "/x", nil)
	id := generated.Header.Get("X-Request-Id")
	if len(id) != 16 || generated.BodyString() != id {
		t.Errorf("generated id = %q body = %q", id, generated.BodyString())
	}

	if got := ut.PerformRequest(s, "GET", "/x", nil, ut.Header{Key: "X-Request-Id", Value: "abc-123_x.y"}); got.Header.Get("X-Request-Id") != "abc-123_x.y" {
		t.Errorf("valid client id was replaced: %q", got.Header.Get("X-Request-Id"))
	}

	for _, bad := range []string{"has space", "new\nline", "semi;colon", strings.Repeat("a", 129)} {
		got := ut.PerformRequest(s, "GET", "/x", nil, ut.Header{Key: "X-Request-Id", Value: bad})
		if replaced := got.Header.Get("X-Request-Id"); replaced == bad || len(replaced) != 16 {
			t.Errorf("unsafe id %q was kept: %q", bad, replaced)
		}
	}
}

func TestAccessLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	s := quietServer()
	s.Use(middleware.RequestID(), middleware.AccessLog(logger), middleware.Recovery(logger))
	s.GET("/users/:id", ok)
	s.GET("/boom", func(context.Context, *framework.Context) { panic("x") })

	ut.PerformRequest(s, "GET", "/users/9", nil, ut.Header{Key: "X-Request-Id", Value: "rid-9"})
	line := logs.String()
	for _, want := range []string{"method=GET", "path=/users/9", "route=/users/:id", "status=200", "bytes=2", "client_ip=192.0.2.1", "request_id=rid-9", "duration="} {
		if !strings.Contains(line, want) {
			t.Errorf("access log lacks %q:\n%s", want, line)
		}
	}

	logs.Reset()
	ut.PerformRequest(s, "GET", "/boom", nil)
	if !strings.Contains(logs.String(), "status=500") {
		t.Errorf("access log must see the 500 produced by Recovery:\n%s", logs.String())
	}
}

func corsServer(cfg middleware.CORSConfig) *framework.Server {
	s := quietServer()
	s.Use(middleware.CORS(cfg))
	s.GET("/x", ok)
	s.OPTIONS("/x", ok)
	return s
}

func TestCORSAllowedOrigin(t *testing.T) {
	s := corsServer(middleware.CORSConfig{
		AllowOrigins:     []string{"https://app.example.com"},
		AllowCredentials: true,
		ExposeHeaders:    []string{"X-Request-Id"},
	})

	got := ut.PerformRequest(s, "GET", "/x", nil, ut.Header{Key: "Origin", Value: "https://app.example.com"})
	if got.Header.Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
		got.Header.Get("Access-Control-Allow-Credentials") != "true" ||
		got.Header.Get("Access-Control-Expose-Headers") != "X-Request-Id" ||
		got.Header.Get("Vary") != "Origin" || got.Status != 200 {
		t.Errorf("simple request headers: %+v status=%d", got.Header, got.Status)
	}
}

func TestCORSPreflight(t *testing.T) {
	s := corsServer(middleware.CORSConfig{AllowOrigins: []string{"https://app.example.com"}, MaxAge: 10 * time.Minute})

	got := ut.PerformRequest(s, "OPTIONS", "/x", nil,
		ut.Header{Key: "Origin", Value: "https://app.example.com"},
		ut.Header{Key: "Access-Control-Request-Method", Value: "PUT"},
		ut.Header{Key: "Access-Control-Request-Headers", Value: "content-type, x-token"})
	if got.Status != 204 || len(got.Body) != 0 {
		t.Fatalf("preflight = %d %q", got.Status, got.Body)
	}
	if got.Header.Get("Access-Control-Allow-Headers") != "content-type, x-token" ||
		got.Header.Get("Access-Control-Max-Age") != "600" ||
		!strings.Contains(got.Header.Get("Access-Control-Allow-Methods"), "PUT") {
		t.Errorf("preflight headers: %+v", got.Header)
	}
}

func TestCORSDisallowedOriginAndNoOrigin(t *testing.T) {
	s := corsServer(middleware.CORSConfig{AllowOrigins: []string{"https://app.example.com"}})

	got := ut.PerformRequest(s, "GET", "/x", nil, ut.Header{Key: "Origin", Value: "https://evil.example"})
	if got.Status != 200 || got.Header.Has("Access-Control-Allow-Origin") {
		t.Errorf("disallowed origin got CORS headers: %+v", got.Header)
	}
	if got := ut.PerformRequest(s, "GET", "/x", nil); got.Header.Has("Access-Control-Allow-Origin") || got.Header.Has("Vary") {
		t.Errorf("request without Origin must be untouched: %+v", got.Header)
	}
}

func TestCORSWildcard(t *testing.T) {
	s := corsServer(middleware.CORSConfig{AllowOrigins: []string{"*"}})
	got := ut.PerformRequest(s, "GET", "/x", nil, ut.Header{Key: "Origin", Value: "https://anything.example"})
	if got.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("wildcard = %q", got.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSRejectsWildcardWithCredentials(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for wildcard origin with credentials")
		}
	}()
	middleware.CORS(middleware.CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true})
}
