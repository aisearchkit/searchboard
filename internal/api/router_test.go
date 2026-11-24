package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/ut"
)

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>app</title>")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"assets/index-abc.js":  {Data: []byte("console.log('app')")},
		"assets/index-abc.css": {Data: []byte("body{}")},
		"docs/index.html":      {Data: []byte("<h1>docs</h1>")},
	}
}

// uiServer is a minimal server that only has the web UI behind the API 404.
func uiServer() *framework.Server {
	s := framework.New(framework.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	s.NoRoute(notFoundOrWebUI(testAssets()))
	return s
}

func TestWebUIServesFilesAndTheAppShell(t *testing.T) {
	s := uiServer()

	home := ut.PerformRequest(s, "GET", "/", nil)
	if home.Status != 200 || !strings.Contains(home.BodyString(), "<title>app</title>") || home.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("home = %d %q cache=%q", home.Status, home.BodyString(), home.Header.Get("Cache-Control"))
	}

	// Client-side routes get the app shell so a page reload works.
	for _, route := range []string{"/matrix", "/matrix/prod-logs", "/devtools/"} {
		if got := ut.PerformRequest(s, "GET", route, nil); got.Status != 200 || !strings.Contains(got.BodyString(), "<title>app</title>") {
			t.Errorf("GET %s = %d %q, want the app shell", route, got.Status, got.BodyString())
		}
	}

	js := ut.PerformRequest(s, "GET", "/assets/index-abc.js", nil)
	if js.Status != 200 || js.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		!strings.HasPrefix(js.Header.Get("Content-Type"), "text/javascript") {
		t.Errorf("hashed asset = %d cache=%q type=%q", js.Status, js.Header.Get("Cache-Control"), js.Header.Get("Content-Type"))
	}
	if got := ut.PerformRequest(s, "GET", "/favicon.svg", nil); got.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("non-hashed file cache = %q", got.Header.Get("Cache-Control"))
	}
	if got := ut.PerformRequest(s, "GET", "/docs/", nil); got.BodyString() != "<h1>docs</h1>" {
		t.Errorf("directory index = %q", got.BodyString())
	}
	if got := ut.PerformRequest(s, "HEAD", "/", nil); got.Status != 200 {
		t.Errorf("HEAD / = %d", got.Status)
	}
}

func TestWebUIDoesNotInventFilesOrAcceptWrites(t *testing.T) {
	s := uiServer()

	// A missing path that looks like a file is a real 404, not an HTML page.
	for _, missing := range []string{"/assets/old-123.js", "/robots.txt", "/missing.css"} {
		if got := ut.PerformRequest(s, "GET", missing, nil); got.Status != 404 {
			t.Errorf("GET %s = %d, want 404", missing, got.Status)
		}
	}
	if got := ut.PerformRequest(s, "POST", "/matrix", nil); got.Status != 404 {
		t.Errorf("POST to a UI path = %d, want 404", got.Status)
	}
	if got := ut.PerformRequest(s, "GET", "/%zz", nil); got.Status != 400 {
		t.Errorf("bad escape = %d, want 400", got.Status)
	}
}

func TestWebUIPathsCannotEscapeTheAssetRoot(t *testing.T) {
	s := uiServer()
	for _, target := range []string{"/../etc/passwd", "/..%2f..%2fetc%2fpasswd", "/%2e%2e/%2e%2e/etc/passwd"} {
		got := ut.PerformRequest(s, "GET", target, nil)
		if strings.Contains(got.BodyString(), "root:") {
			t.Fatalf("GET %s leaked a host file", target)
		}
	}
}

func TestUnknownAPIPathsAreJSONNotTheAppShell(t *testing.T) {
	env := newTestEnv()

	for _, target := range []string{"/api/nope", "/api/v1/nope", "/api/v1/clusters/abc/unknown"} {
		got := env.do(t, "GET", target, "")
		if got.Status != 404 || got.BodyString() != `{"error":"not found"}` || !strings.HasPrefix(got.Header.Get("Content-Type"), "application/json") {
			t.Errorf("GET %s = %d %q (%s)", target, got.Status, got.BodyString(), got.Header.Get("Content-Type"))
		}
	}
}

func TestWrongMethodIsJSON405WithAllowHeader(t *testing.T) {
	env := newTestEnv()
	got := env.do(t, "PUT", "/api/v1/clusters", "")
	if got.Status != 405 || got.BodyString() != `{"error":"method not allowed"}` || got.Header.Get("Allow") != "GET, POST" {
		t.Fatalf("PUT /clusters = %d %q Allow=%q", got.Status, got.BodyString(), got.Header.Get("Allow"))
	}
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
	env := newTestEnv()
	got := env.do(t, "GET", "/api/v1/healthz", "")
	if len(got.Header.Get("X-Request-Id")) != 16 {
		t.Fatalf("X-Request-Id = %q", got.Header.Get("X-Request-Id"))
	}
	if got := ut.PerformRequest(env.server, "GET", "/api/v1/healthz", nil, ut.Header{Key: "X-Request-Id", Value: "trace-1"}); got.Header.Get("X-Request-Id") != "trace-1" {
		t.Errorf("a client supplied id must be kept, got %q", got.Header.Get("X-Request-Id"))
	}
}

func TestPanicsBecomeJSON500(t *testing.T) {
	env := newTestEnv()
	env.connector.panicOnConnect = true
	id := env.addCluster(t, "demo")

	got := env.do(t, "GET", "/api/v1/clusters/"+id+"/matrix", "")
	if got.Status != 500 || got.BodyString() != `{"error":"internal server error"}` {
		t.Fatalf("panic response = %d %q", got.Status, got.BodyString())
	}
	if env.do(t, "GET", "/api/v1/healthz", "").Status != 200 {
		t.Fatal("the server must keep working after a panic")
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	// The server enforces maxBodyBytes while reading, so this goes over a real connection.
	r := startReal(t, newTestEnv())
	big := strings.NewReader(`{"name":"` + strings.Repeat("x", maxBodyBytes) + `"}`)
	resp, err := http.Post(r+"/api/v1/clusters", "application/json", big)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestOverARealConnection(t *testing.T) {
	base := startReal(t, newTestEnv())

	resp, err := http.Get(base + "/api/v1/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Fatalf("healthz = %d %q", resp.StatusCode, body)
	}

	// The embedded web UI answers every other path with something HTML-ish or 404, never a panic.
	resp, err = http.Get(base + "/some/client/route")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 404 {
		t.Fatalf("UI path = %d", resp.StatusCode)
	}
}

// startReal serves env's server on a random local port and returns its base URL.
func startReal(t *testing.T, env *testEnv) string {
	t.Helper()
	s := New(Deps{
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clusters:  env.repo,
		History:   env.history,
		Connector: env.connector,
	}, framework.WithHostPorts("127.0.0.1:0"))
	ln, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = s.Serve(ln); close(done) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		<-done
	})
	return "http://" + ln.Addr().String()
}
