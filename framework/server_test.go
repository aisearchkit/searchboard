package framework_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/ut"
)

func newServer(opts ...framework.Option) *framework.Server {
	opts = append([]framework.Option{framework.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}, opts...)
	return framework.New(opts...)
}

func TestRoutingAndParams(t *testing.T) {
	s := newServer()
	s.GET("/users/:id", func(_ context.Context, c *framework.Context) {
		c.String(200, "user=%s route=%s", c.Param("id"), c.FullPath())
	})
	s.GET("/users/me", func(_ context.Context, c *framework.Context) { c.String(200, "me") })
	s.GET("/files/*path", func(_ context.Context, c *framework.Context) { c.String(200, "file=%s", c.Param("path")) })

	tests := []struct{ target, want string }{
		{"/users/42", "user=42 route=/users/:id"},
		{"/users/me", "me"},
		{"/users/a%20b", "user=a b route=/users/:id"}, // parameter values are percent-decoded
		{"/users/a%2Fb", "user=a/b route=/users/:id"}, // an encoded slash never splits segments
		{"/files/x/y.txt", "file=/x/y.txt"},
	}
	for _, tt := range tests {
		got := ut.PerformRequest(s, "GET", tt.target, nil)
		if got.Status != 200 || got.BodyString() != tt.want {
			t.Errorf("GET %s = %d %q, want %q", tt.target, got.Status, got.BodyString(), tt.want)
		}
	}
}

func TestMethodHelpers(t *testing.T) {
	s := newServer()
	handler := func(_ context.Context, c *framework.Context) { c.String(200, "%s", c.Method()) }
	s.GET("/m", handler)
	s.POST("/m", handler)
	s.PUT("/m", handler)
	s.PATCH("/m", handler)
	s.DELETE("/m", handler)
	s.OPTIONS("/m", handler)
	s.Any("/any", handler)

	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if got := ut.PerformRequest(s, method, "/m", nil); got.BodyString() != method {
			t.Errorf("%s /m = %q", method, got.BodyString())
		}
	}
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		if got := ut.PerformRequest(s, method, "/any", nil); got.Status != 200 {
			t.Errorf("%s /any = %d", method, got.Status)
		}
	}
}

func TestMiddlewareOrderAndGroups(t *testing.T) {
	var trace []string
	step := func(name string) framework.HandlerFunc {
		return func(ctx context.Context, c *framework.Context) {
			trace = append(trace, name+">")
			c.Next(ctx)
			trace = append(trace, "<"+name)
		}
	}

	s := newServer()
	s.Use(step("root"))
	api := s.Group("/api", step("api"))
	v1 := api.Group("/v1", step("v1"))
	v1.GET("/ping", step("route"), func(_ context.Context, c *framework.Context) {
		trace = append(trace, "handler")
		c.String(200, "pong")
	})
	s.GET("/plain", func(_ context.Context, c *framework.Context) { c.String(200, "plain") })

	if got := ut.PerformRequest(s, "GET", "/api/v1/ping", nil); got.BodyString() != "pong" {
		t.Fatalf("body = %q", got.BodyString())
	}
	want := "root> api> v1> route> handler <route <v1 <api <root"
	if got := strings.Join(trace, " "); got != want {
		t.Fatalf("trace = %s\nwant    %s", got, want)
	}

	trace = nil
	ut.PerformRequest(s, "GET", "/plain", nil)
	if got := strings.Join(trace, " "); got != "root> <root" {
		t.Fatalf("group middleware leaked into a plain route: %s", got)
	}
	if api.BasePath() != "/api" || v1.BasePath() != "/api/v1" {
		t.Fatalf("base paths: %q %q", api.BasePath(), v1.BasePath())
	}
}

func TestAbortStopsTheChain(t *testing.T) {
	reached := false
	s := newServer()
	s.GET("/x",
		func(_ context.Context, c *framework.Context) { c.AbortWithStatus(http.StatusForbidden) },
		func(_ context.Context, c *framework.Context) { reached = true },
	)
	got := ut.PerformRequest(s, "GET", "/x", nil)
	if got.Status != 403 || reached {
		t.Fatalf("status=%d reached=%v", got.Status, reached)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	s := newServer()
	s.GET("/only-get", func(_ context.Context, c *framework.Context) { c.String(200, "ok") })
	s.POST("/only-get", func(_ context.Context, c *framework.Context) { c.String(200, "ok") })

	if got := ut.PerformRequest(s, "GET", "/missing", nil); got.Status != 404 {
		t.Errorf("missing route = %d", got.Status)
	}
	got := ut.PerformRequest(s, "DELETE", "/only-get", nil)
	if got.Status != 405 || got.Header.Get("Allow") != "GET, POST" {
		t.Errorf("405 response = %d Allow=%q", got.Status, got.Header.Get("Allow"))
	}
}

func TestMethodNotAllowedCanBeDisabled(t *testing.T) {
	s := newServer(framework.WithHandleMethodNotAllowed(false))
	s.GET("/x", func(_ context.Context, c *framework.Context) { c.String(200, "ok") })
	if got := ut.PerformRequest(s, "POST", "/x", nil); got.Status != 404 {
		t.Fatalf("status = %d, want 404 when 405 handling is off", got.Status)
	}
}

func TestCustomNoRouteAndNoMethodRunServerMiddleware(t *testing.T) {
	s := newServer()
	s.Use(func(ctx context.Context, c *framework.Context) {
		c.SetHeader("X-Global", "1")
		c.Next(ctx)
	})
	s.NoRoute(func(_ context.Context, c *framework.Context) {
		c.JSON(404, map[string]string{"error": "no such route"})
	})
	s.NoMethod(func(_ context.Context, c *framework.Context) {
		c.JSON(405, map[string]string{"error": "wrong method"})
	})
	s.GET("/x", func(_ context.Context, c *framework.Context) { c.String(200, "ok") })

	got := ut.PerformRequest(s, "GET", "/nope", nil)
	if got.Status != 404 || got.BodyString() != `{"error":"no such route"}` || got.Header.Get("X-Global") != "1" {
		t.Errorf("custom 404 = %d %q %q", got.Status, got.BodyString(), got.Header.Get("X-Global"))
	}
	got = ut.PerformRequest(s, "POST", "/x", nil)
	if got.Status != 405 || got.BodyString() != `{"error":"wrong method"}` || got.Header.Get("Allow") != "GET" {
		t.Errorf("custom 405 = %d %q", got.Status, got.BodyString())
	}
}

func TestTrailingSlashRedirect(t *testing.T) {
	s := newServer()
	s.GET("/a", func(_ context.Context, c *framework.Context) { c.String(200, "a") })
	s.POST("/b/", func(_ context.Context, c *framework.Context) { c.String(200, "b") })

	got := ut.PerformRequest(s, "GET", "/a/?x=1", nil)
	if got.Status != 301 || got.Header.Get("Location") != "/a?x=1" {
		t.Errorf("GET redirect = %d %q", got.Status, got.Header.Get("Location"))
	}
	got = ut.PerformRequest(s, "POST", "/b", nil)
	if got.Status != 308 || got.Header.Get("Location") != "/b/" {
		t.Errorf("POST redirect = %d %q (308 keeps the method)", got.Status, got.Header.Get("Location"))
	}
}

func TestTrailingSlashRedirectCanBeDisabled(t *testing.T) {
	s := newServer(framework.WithRedirectTrailingSlash(false))
	s.GET("/a", func(_ context.Context, c *framework.Context) { c.String(200, "a") })
	if got := ut.PerformRequest(s, "GET", "/a/", nil); got.Status != 404 {
		t.Fatalf("status = %d, want 404", got.Status)
	}
}

func TestRedirectNeverProducesProtocolRelativeLocation(t *testing.T) {
	s := newServer()
	s.GET("/:a/", func(_ context.Context, c *framework.Context) { c.String(200, "x") })
	// "//evil.example" has an empty first segment and no route; the redirect
	// target must still start with exactly one slash.
	got := ut.PerformRequest(s, "GET", "//evil.example", nil)
	if loc := got.Header.Get("Location"); strings.HasPrefix(loc, "//") {
		t.Fatalf("open redirect: Location = %q", loc)
	}
}

func TestHeadFallsBackToGet(t *testing.T) {
	s := newServer()
	s.GET("/x", func(_ context.Context, c *framework.Context) { c.String(200, "body") })
	if got := ut.PerformRequest(s, "HEAD", "/x", nil); got.Status != 200 {
		t.Fatalf("HEAD = %d", got.Status)
	}
}

func TestQueryAndHeaderHelpers(t *testing.T) {
	s := newServer()
	s.GET("/q", func(_ context.Context, c *framework.Context) {
		c.JSON(200, map[string]any{
			"q":       c.Query("q"),
			"missing": c.Query("missing"),
			"default": c.DefaultQuery("missing", "dflt"),
			"set":     c.DefaultQuery("q", "dflt"),
			"tags":    c.QueryArray("tag"),
			"agent":   c.GetHeader("user-agent"),
			"ip":      c.ClientIP(),
		})
	})
	got := ut.PerformRequest(s, "GET", "/q?q=1&tag=a&tag=b", nil, ut.Header{Key: "User-Agent", Value: "t"})
	want := `{"agent":"t","default":"dflt","ip":"192.0.2.1","missing":"","q":"1","set":"1","tags":["a","b"]}`
	if got.BodyString() != want {
		t.Fatalf("got  %s\nwant %s", got.BodyString(), want)
	}
}

func TestContextKeys(t *testing.T) {
	s := newServer()
	s.GET("/k", func(ctx context.Context, c *framework.Context) {
		c.Set("name", "ada")
		c.Next(ctx)
	}, func(_ context.Context, c *framework.Context) {
		v, ok := c.Get("name")
		_, missing := c.Get("nope")
		c.String(200, "%v %v %v %q", v, ok, missing, c.GetString("name"))
	})
	if got := ut.PerformRequest(s, "GET", "/k", nil); got.BodyString() != "ada true false \"ada\"" {
		t.Fatalf("body = %q", got.BodyString())
	}
	// Keys must not leak into the next request that reuses the pooled Context.
	s.GET("/k2", func(_ context.Context, c *framework.Context) {
		_, ok := c.Get("name")
		c.String(200, "%v", ok)
	})
	if got := ut.PerformRequest(s, "GET", "/k2", nil); got.BodyString() != "false" {
		t.Fatalf("context keys leaked across requests: %q", got.BodyString())
	}
}

func TestResponseHelpers(t *testing.T) {
	s := newServer()
	s.GET("/json", func(_ context.Context, c *framework.Context) { c.JSON(201, map[string]int{"n": 1}) })
	s.GET("/data", func(_ context.Context, c *framework.Context) { c.Data(200, "image/png", []byte{1, 2, 3}) })
	s.GET("/empty", func(_ context.Context, c *framework.Context) { c.NoContent(204) })
	s.GET("/redir", func(_ context.Context, c *framework.Context) { c.Redirect(302, "/json") })
	s.GET("/bad", func(_ context.Context, c *framework.Context) { c.JSON(200, make(chan int)) })

	if got := ut.PerformRequest(s, "GET", "/json", nil); got.Status != 201 ||
		got.Header.Get("Content-Type") != "application/json; charset=utf-8" || got.BodyString() != `{"n":1}` {
		t.Errorf("json = %d %q %q", got.Status, got.Header.Get("Content-Type"), got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/data", nil); got.Header.Get("Content-Type") != "image/png" || len(got.Body) != 3 {
		t.Errorf("data = %+v", got)
	}
	if got := ut.PerformRequest(s, "GET", "/empty", nil); got.Status != 204 || len(got.Body) != 0 {
		t.Errorf("no content = %+v", got)
	}
	if got := ut.PerformRequest(s, "GET", "/redir", nil); got.Status != 302 || got.Header.Get("Location") != "/json" {
		t.Errorf("redirect = %+v", got)
	}
	if got := ut.PerformRequest(s, "GET", "/bad", nil); got.Status != 500 {
		t.Errorf("unencodable value must give 500, got %d", got.Status)
	}
}

type createUser struct {
	ID    int      `path:"id"`
	Page  int      `query:"page"`
	Name  string   `json:"name" form:"name" binding:"required"`
	Token string   `header:"X-Token"`
	Tags  []string `query:"tag"`
}

func TestBinding(t *testing.T) {
	s := newServer()
	s.POST("/users/:id", func(_ context.Context, c *framework.Context) {
		var in createUser
		if err := c.Bind(&in); err != nil {
			c.JSON(400, map[string]string{"error": err.Error()})
			return
		}
		c.JSON(200, in)
	})
	s.GET("/q", func(_ context.Context, c *framework.Context) {
		var in struct {
			Page int `query:"page" binding:"required"`
		}
		if err := c.BindQuery(&in); err != nil {
			c.String(400, "%v", err)
			return
		}
		c.String(200, "page=%d", in.Page)
	})
	s.GET("/h", func(_ context.Context, c *framework.Context) {
		var in struct {
			Token string `header:"x-token" binding:"required"`
		}
		if err := c.BindHeader(&in); err != nil {
			c.String(400, "%v", err)
			return
		}
		c.String(200, "%s", in.Token)
	})
	s.POST("/form", func(_ context.Context, c *framework.Context) {
		var in createUser
		if err := c.BindForm(&in); err != nil {
			c.String(400, "%v", err)
			return
		}
		c.String(200, "%s", in.Name)
	})

	// JSON body + path + query + header all land in one struct.
	got := ut.PerformRequest(s, "POST", "/users/7?page=2&tag=a&tag=b", []byte(`{"name":"ada"}`),
		ut.Header{Key: "Content-Type", Value: "application/json; charset=utf-8"}, ut.Header{Key: "X-Token", Value: "tok"})
	var out createUser
	if err := json.Unmarshal(got.Body, &out); err != nil || got.Status != 200 {
		t.Fatalf("status=%d body=%s", got.Status, got.Body)
	}
	if out.ID != 7 || out.Page != 2 || out.Name != "ada" || len(out.Tags) != 2 {
		t.Errorf("bound struct: %+v", out)
	}

	// Form body.
	got = ut.PerformRequest(s, "POST", "/users/7", []byte("name=grace"),
		ut.Header{Key: "Content-Type", Value: "application/x-www-form-urlencoded"})
	if got.Status != 200 || !strings.Contains(got.BodyString(), `"name":"grace"`) {
		t.Errorf("form bind = %d %s", got.Status, got.Body)
	}

	// Validation, wrong content type and malformed input are client errors.
	for name, tc := range map[string]struct {
		target, body, contentType string
	}{
		"required missing":  {"/users/7", `{}`, "application/json"},
		"bad json":          {"/users/7", `{`, "application/json"},
		"unsupported type":  {"/users/7", `name=x`, "text/plain"},
		"bad path integer":  {"/users/abc", `{"name":"x"}`, "application/json"},
		"bad query integer": {"/users/7?page=x", `{"name":"x"}`, "application/json"},
	} {
		got := ut.PerformRequest(s, "POST", tc.target, []byte(tc.body), ut.Header{Key: "Content-Type", Value: tc.contentType})
		if got.Status != 400 {
			t.Errorf("%s: status = %d (%s)", name, got.Status, got.Body)
		}
	}

	if got := ut.PerformRequest(s, "GET", "/q?page=3", nil); got.BodyString() != "page=3" {
		t.Errorf("BindQuery = %q", got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/q", nil); got.Status != 400 {
		t.Errorf("BindQuery required = %d", got.Status)
	}
	if got := ut.PerformRequest(s, "GET", "/h", nil, ut.Header{Key: "X-Token", Value: "t"}); got.BodyString() != "t" {
		t.Errorf("BindHeader = %q", got.BodyString())
	}
	if got := ut.PerformRequest(s, "POST", "/form", []byte("name=z"), ut.Header{Key: "Content-Type", Value: "application/x-www-form-urlencoded"}); got.BodyString() != "z" {
		t.Errorf("BindForm = %q", got.BodyString())
	}
}

func TestUnrecoveredPanicBecomes500(t *testing.T) {
	s := newServer()
	s.GET("/boom", func(context.Context, *framework.Context) { panic("kaboom") })
	s.GET("/ok", func(_ context.Context, c *framework.Context) { c.String(200, "fine") })

	got := ut.PerformRequest(s, "GET", "/boom", nil)
	if got.Status != 500 || strings.Contains(got.BodyString(), "kaboom") {
		t.Fatalf("panic response = %d %q (must not leak the panic value)", got.Status, got.BodyString())
	}
	if got := ut.PerformRequest(s, "GET", "/ok", nil); got.Status != 200 {
		t.Fatalf("server unusable after a panic: %d", got.Status)
	}
}

func TestRouteRegistrationPanics(t *testing.T) {
	handler := func(context.Context, *framework.Context) {}
	for name, register := range map[string]func(*framework.Server){
		"no handler": func(s *framework.Server) { s.GET("/x") },
		"duplicate":  func(s *framework.Server) { s.GET("/x", handler); s.GET("/x", handler) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			register(newServer())
		})
	}
}

func TestRoutesListing(t *testing.T) {
	s := newServer()
	handler := func(context.Context, *framework.Context) {}
	s.GET("/b", handler)
	s.Group("/api").POST("/a", handler)
	routes := s.Routes()
	if len(routes) != 2 || routes[0].Path != "/api/a" || routes[1].Path != "/b" {
		t.Fatalf("routes = %v", routes)
	}
}

func TestGroupPathJoining(t *testing.T) {
	s := newServer()
	handler := func(_ context.Context, c *framework.Context) { c.String(200, "%s", c.FullPath()) }
	g := s.Group("/api/")
	g.GET("/", handler)
	g.GET("items", handler)
	g.GET("/items/", handler)

	for _, want := range []string{"/api/", "/api/items", "/api/items/"} {
		if got := ut.PerformRequest(s, "GET", want, nil); got.BodyString() != want {
			t.Errorf("GET %s -> %q", want, got.BodyString())
		}
	}
}
