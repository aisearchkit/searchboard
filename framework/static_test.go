package framework_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/ut"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<h1>home</h1>")},
		"app.js":          {Data: []byte("console.log(1)")},
		"css/site.css":    {Data: []byte("body{}")},
		"docs/index.html": {Data: []byte("<h1>docs</h1>")},
		"blob":            {Data: []byte("\x89PNG\r\n\x1a\nrest")},
		"timed.txt":       {Data: []byte("hello"), ModTime: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
}

func TestStaticFSServesFiles(t *testing.T) {
	s := newServer()
	s.Group("/assets").StaticFS("/", testFS())

	got := ut.PerformRequest(s, "GET", "/assets/app.js", nil)
	if got.Status != 200 || got.BodyString() != "console.log(1)" || !strings.HasPrefix(got.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("js = %d %q %q", got.Status, got.BodyString(), got.Header.Get("Content-Type"))
	}
	if got := ut.PerformRequest(s, "GET", "/assets/css/site.css", nil); !strings.HasPrefix(got.Header.Get("Content-Type"), "text/css") {
		t.Errorf("css content type = %q", got.Header.Get("Content-Type"))
	}
	if got := ut.PerformRequest(s, "GET", "/assets/blob", nil); got.Header.Get("Content-Type") != "image/png" {
		t.Errorf("sniffed content type = %q", got.Header.Get("Content-Type"))
	}
	if got := ut.PerformRequest(s, "HEAD", "/assets/app.js", nil); got.Status != 200 {
		t.Errorf("HEAD = %d", got.Status)
	}
}

func TestStaticFSMissingAndDirectories(t *testing.T) {
	s := newServer()
	s.StaticFS("/static", testFS())

	if got := ut.PerformRequest(s, "GET", "/static/nope.js", nil); got.Status != 404 {
		t.Errorf("missing file = %d", got.Status)
	}
	if got := ut.PerformRequest(s, "GET", "/static/docs/", nil); got.Status != 200 || got.BodyString() != "<h1>docs</h1>" {
		t.Errorf("directory index = %d %q", got.Status, got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/static/docs", nil); got.BodyString() != "<h1>docs</h1>" {
		t.Errorf("directory without slash = %q", got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/static/css/", nil); got.Status != 404 {
		t.Errorf("directory without an index file = %d", got.Status)
	}
}

func TestStaticFSCannotEscapeTheRoot(t *testing.T) {
	root := fstest.MapFS{"public/ok.txt": {Data: []byte("ok")}, "secret.txt": {Data: []byte("TOP SECRET")}}
	sub, err := fs.Sub(root, "public")
	if err != nil {
		t.Fatal(err)
	}
	s := newServer()
	s.StaticFS("/files", sub)

	for _, target := range []string{
		"/files/../secret.txt",
		"/files/..%2fsecret.txt",
		"/files/%2e%2e/secret.txt",
		"/files/%2e%2e%2f%2e%2e%2fsecret.txt",
		"/files/a/../../secret.txt",
	} {
		got := ut.PerformRequest(s, "GET", target, nil)
		if strings.Contains(got.BodyString(), "SECRET") {
			t.Fatalf("GET %s leaked a file outside the root", target)
		}
	}
	if got := ut.PerformRequest(s, "GET", "/files/ok.txt", nil); got.BodyString() != "ok" {
		t.Errorf("legitimate file = %q", got.BodyString())
	}
}

func TestStaticConditionalRequests(t *testing.T) {
	s := newServer()
	s.StaticFS("/s", testFS(), framework.StaticOptions{
		CacheControl: func(name string) string {
			if strings.HasSuffix(name, ".js") {
				return "public, max-age=31536000, immutable"
			}
			return "no-cache"
		},
	})

	first := ut.PerformRequest(s, "GET", "/s/app.js", nil)
	etag := first.Header.Get("ETag")
	if etag == "" || first.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("headers: %+v", first.Header)
	}
	if got := ut.PerformRequest(s, "GET", "/s/css/site.css", nil); got.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("css cache control = %q", got.Header.Get("Cache-Control"))
	}

	for _, header := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		got := ut.PerformRequest(s, "GET", "/s/app.js", nil, ut.Header{Key: "If-None-Match", Value: header})
		if got.Status != 304 || len(got.Body) != 0 {
			t.Errorf("If-None-Match %q = %d with %d body bytes", header, got.Status, len(got.Body))
		}
	}
	if got := ut.PerformRequest(s, "GET", "/s/app.js", nil, ut.Header{Key: "If-None-Match", Value: `"stale"`}); got.Status != 200 {
		t.Errorf("stale validator = %d", got.Status)
	}

	timed := ut.PerformRequest(s, "GET", "/s/timed.txt", nil)
	if timed.Header.Get("Last-Modified") != "Fri, 02 Jan 2026 03:04:05 GMT" {
		t.Errorf("Last-Modified = %q", timed.Header.Get("Last-Modified"))
	}
}

func TestFileHandlerAsNoRouteForSinglePageApps(t *testing.T) {
	assets := testFS()
	fileHandler := framework.FileHandler(assets, framework.StaticOptions{})

	s := newServer()
	s.GET("/api/ping", func(_ context.Context, c *framework.Context) { c.String(200, "pong") })
	s.NoRoute(func(ctx context.Context, c *framework.Context) {
		if strings.HasPrefix(c.Path(), "/api/") {
			c.JSON(404, map[string]string{"error": "not found"})
			return
		}
		if _, err := fs.Stat(assets, strings.TrimPrefix(c.Path(), "/")); err != nil {
			c.Request.Path = "/" // client-side route: serve the app shell
		}
		fileHandler(ctx, c)
	})

	if got := ut.PerformRequest(s, "GET", "/api/ping", nil); got.BodyString() != "pong" {
		t.Errorf("api route = %q", got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/api/unknown", nil); got.Status != 404 || got.BodyString() != `{"error":"not found"}` {
		t.Errorf("unknown api route = %d %q", got.Status, got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/app.js", nil); got.BodyString() != "console.log(1)" {
		t.Errorf("asset = %q", got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/matrix/some/client/route", nil); got.BodyString() != "<h1>home</h1>" {
		t.Errorf("SPA fallback = %q", got.BodyString())
	}
}

func TestFileHandlerRejectsBadPercentEncoding(t *testing.T) {
	s := newServer()
	s.NoRoute(framework.FileHandler(testFS(), framework.StaticOptions{}))
	if got := ut.PerformRequest(s, "GET", "/%zz", nil); got.Status != 400 {
		t.Errorf("bad escape = %d, want 400", got.Status)
	}
	if got := ut.PerformRequest(s, "GET", "/app.js", nil); got.Status != 200 {
		t.Errorf("valid file = %d", got.Status)
	}
}

func TestFileFSReturnsNotExist(t *testing.T) {
	s := newServer()
	var failure error
	s.GET("/x", func(_ context.Context, c *framework.Context) {
		failure = c.FileFS(testFS(), "missing.txt")
		c.String(200, "done")
	})
	ut.PerformRequest(s, "GET", "/x", nil)
	if !errors.Is(failure, fs.ErrNotExist) {
		t.Fatalf("FileFS error = %v, want fs.ErrNotExist", failure)
	}
}
