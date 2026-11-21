package framework_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/network/standard"
)

// transportCases runs each end-to-end test against the default transport (netpoll
// on Linux and the BSDs) and against the standard-library transport.
func transportCases() []struct {
	name      string
	transport func() network.Transport
} {
	return []struct {
		name      string
		transport func() network.Transport
	}{
		{"default", func() network.Transport { return nil }},
		{"standard", func() network.Transport { return standard.New() }},
	}
}

func forEachTransport(t *testing.T, fn func(t *testing.T, newTransport func() network.Transport)) {
	for _, tc := range transportCases() {
		t.Run(tc.name, func(t *testing.T) { fn(t, tc.transport) })
	}
}

type running struct {
	server *framework.Server
	addr   string
	done   chan struct{} // closed when Serve has returned
	err    error         // Serve's result; valid after done is closed
}

// start serves s on a random local port and registers a cleanup that shuts it down.
func start(t *testing.T, transport network.Transport, register func(*framework.Server), opts ...framework.Option) *running {
	t.Helper()
	if transport != nil {
		opts = append(opts, framework.WithTransport(transport))
	}
	opts = append(opts, framework.WithHostPorts("127.0.0.1:0"))
	s := newServer(opts...)
	register(s)

	ln, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	r := &running{server: s, addr: ln.Addr().String(), done: make(chan struct{})}
	go func() {
		r.err = s.Serve(ln)
		close(r.done)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
			t.Error("Serve did not return after Shutdown")
		}
	})
	return r
}

func (r *running) url(path string) string { return "http://" + r.addr + path }

func (r *running) dial(t *testing.T) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", r.addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableCompression: true}, Timeout: 10 * time.Second}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func echoRoutes(s *framework.Server) {
	s.GET("/hello/:name", func(_ context.Context, c *framework.Context) {
		c.JSON(200, map[string]string{"hello": c.Param("name")})
	})
	s.POST("/echo", func(_ context.Context, c *framework.Context) {
		c.Data(200, "application/octet-stream", c.Body())
	})
	s.GET("/big", func(_ context.Context, c *framework.Context) {
		c.Data(200, "application/octet-stream", bytes.Repeat([]byte("0123456789abcdef"), 64<<10)) // 1 MiB
	})
	s.GET("/panic", func(context.Context, *framework.Context) { panic("boom") })
}

func TestE2EBasics(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		client := newClient()

		resp, err := client.Get(r.url("/hello/world"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json; charset=utf-8" || resp.Header.Get("Date") == "" {
			t.Errorf("response: %d %v", resp.StatusCode, resp.Header)
		}
		if got := readAll(t, resp); got != `{"hello":"world"}` {
			t.Errorf("body = %q", got)
		}

		for path, want := range map[string]int{"/missing": 404, "/echo": 405} {
			resp, err := client.Get(r.url(path))
			if err != nil {
				t.Fatal(err)
			}
			_ = readAll(t, resp)
			if resp.StatusCode != want {
				t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
			}
		}
	})
}

func TestE2EBodies(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		client := newClient()

		for _, size := range []int{0, 1, 1000, 100_000, 3 << 20} {
			payload := bytes.Repeat([]byte("x"), size)
			resp, err := client.Post(r.url("/echo"), "application/octet-stream", bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("size %d: %v", size, err)
			}
			if got := readAll(t, resp); got != string(payload) {
				t.Errorf("size %d: echoed %d bytes", size, len(got))
			}
		}

		// ContentLength -1 makes net/http send Transfer-Encoding: chunked.
		req, _ := http.NewRequest("POST", r.url("/echo"), io.NopCloser(strings.NewReader(strings.Repeat("chunk", 20_000))))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, resp); got != strings.Repeat("chunk", 20_000) {
			t.Errorf("chunked echo returned %d bytes", len(got))
		}

		resp, err = client.Get(r.url("/big"))
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, resp); len(got) != 1<<20 || !strings.HasPrefix(got, "0123456789abcdef") {
			t.Errorf("large response truncated or corrupt: %d bytes", len(got))
		}
	})
}

func TestE2EKeepAliveReusesConnection(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		client := newClient()

		reused := make([]bool, 3)
		for i := range reused {
			trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused[i] = info.Reused }}
			req, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), "GET", r.url("/hello/x"), nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = readAll(t, resp)
		}
		if reused[0] || !reused[1] || !reused[2] {
			t.Fatalf("connection reuse = %v, want [false true true]", reused)
		}
	})
}

func TestE2EPipelining(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		conn := r.dial(t)

		const request = "GET /hello/%s HTTP/1.1\r\nHost: h\r\n\r\n"
		packet := fmt.Sprintf(request, "one") + fmt.Sprintf(request, "two") + fmt.Sprintf(request, "three")
		if _, err := io.WriteString(conn, packet); err != nil {
			t.Fatal(err)
		}

		reader := bufio.NewReader(conn)
		for _, name := range []string{"one", "two", "three"} {
			resp, err := http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatalf("response for %s: %v", name, err)
			}
			if got := readAll(t, resp); got != fmt.Sprintf(`{"hello":"%s"}`, name) {
				t.Errorf("pipelined response for %s = %q", name, got)
			}
		}
	})
}

func TestE2EHead(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		resp, err := newClient().Head(r.url("/hello/x"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 || resp.ContentLength != int64(len(`{"hello":"x"}`)) {
			t.Fatalf("HEAD = %d, Content-Length %d", resp.StatusCode, resp.ContentLength)
		}
	})
}

func TestE2EConnectionClose(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		conn := r.dial(t)
		_, _ = io.WriteString(conn, "GET /hello/x HTTP/1.1\r\nHost: h\r\nConnection: close\r\n\r\n")

		reader := bufio.NewReader(conn)
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = readAll(t, resp)
		if !resp.Close {
			t.Error("response must announce Connection: close")
		}
		if _, err := reader.ReadByte(); err != io.EOF {
			t.Errorf("connection must be closed after the response, got %v", err)
		}
	})
}

func TestE2EMalformedRequests(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes,
			framework.WithMaxHeaderBytes(512), framework.WithMaxBodyBytes(16))

		tests := []struct {
			name, raw string
			status    int
		}{
			{"garbage request line", "NOT AN HTTP REQUEST\r\n\r\n", 400},
			{"unsupported version", "GET / HTTP/2.0\r\nHost: h\r\n\r\n", 505},
			{"missing host", "GET / HTTP/1.1\r\n\r\n", 400},
			{"header too large", "GET / HTTP/1.1\r\nHost: h\r\nX: " + strings.Repeat("a", 2000) + "\r\n\r\n", 431},
			{"body too large", "POST /echo HTTP/1.1\r\nHost: h\r\nContent-Length: 100\r\n\r\n", 413},
			{"smuggling attempt", "POST /echo HTTP/1.1\r\nHost: h\r\nContent-Length: 5\r\nTransfer-Encoding: chunked\r\n\r\n", 400},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				conn := r.dial(t)
				_, _ = io.WriteString(conn, tt.raw)
				reader := bufio.NewReader(conn)
				resp, err := http.ReadResponse(reader, nil)
				if err != nil {
					t.Fatal(err)
				}
				_ = readAll(t, resp)
				if resp.StatusCode != tt.status || !resp.Close {
					t.Errorf("status = %d close=%v, want %d and close", resp.StatusCode, resp.Close, tt.status)
				}
				if _, err := reader.ReadByte(); err != io.EOF {
					t.Errorf("connection must close after a protocol error, got %v", err)
				}
			})
		}
	})
}

func TestE2EExpectContinue(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		conn := r.dial(t)
		reader := bufio.NewReader(conn)

		_, _ = io.WriteString(conn, "POST /echo HTTP/1.1\r\nHost: h\r\nContent-Length: 5\r\nExpect: 100-continue\r\n\r\n")
		line, err := reader.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "HTTP/1.1 100 Continue") {
			t.Fatalf("interim response = %q, %v", line, err)
		}
		_, _ = reader.ReadString('\n') // blank line ending the interim response

		_, _ = io.WriteString(conn, "hello")
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, resp); got != "hello" {
			t.Errorf("echo after 100-continue = %q", got)
		}
	})
}

func TestE2ERequestSurvivesHandlerPanic(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		client := newClient()

		resp, err := client.Get(r.url("/panic"))
		if err != nil {
			t.Fatal(err)
		}
		_ = readAll(t, resp)
		if resp.StatusCode != 500 {
			t.Errorf("panic status = %d", resp.StatusCode)
		}
		resp, err = client.Get(r.url("/hello/after"))
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("server unusable after a panic: %v", err)
		}
		_ = readAll(t, resp)
	})
}

func TestE2EConcurrentRequests(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		client := newClient()

		var wg sync.WaitGroup
		errs := make(chan error, 64)
		for worker := 0; worker < 32; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 25; i++ {
					name := fmt.Sprintf("w%d-%d", worker, i)
					resp, err := client.Get(r.url("/hello/" + name))
					if err != nil {
						errs <- err
						return
					}
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					if string(body) != fmt.Sprintf(`{"hello":"%s"}`, name) {
						errs <- fmt.Errorf("worker %d got %q for %s", worker, body, name)
						return
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
	})
}

func expectClosedWithin(t *testing.T, conn net.Conn, limit time.Duration) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(limit))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != io.EOF {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("connection was still open after %v", limit)
		}
		if err == nil {
			t.Fatal("server sent unexpected data")
		}
	}
}

func TestE2EIdleTimeoutClosesQuietConnections(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes, framework.WithIdleTimeout(200*time.Millisecond))
		expectClosedWithin(t, r.dial(t), 5*time.Second)
	})
}

func TestE2EReadTimeoutClosesSlowRequests(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes, framework.WithReadTimeout(300*time.Millisecond))
		conn := r.dial(t)
		_, _ = io.WriteString(conn, "GET /hello/x HTTP/1.1\r\nHost:") // then stall forever
		expectClosedWithin(t, conn, 5*time.Second)
	})
}

func TestE2EGracefulShutdownLetsInflightRequestFinish(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		entered := make(chan struct{})
		release := make(chan struct{})
		r := start(t, tr(), func(s *framework.Server) {
			s.GET("/slow", func(_ context.Context, c *framework.Context) {
				close(entered)
				<-release
				c.String(200, "finished")
			})
		})

		type result struct {
			status int
			body   string
			close  bool
			err    error
		}
		got := make(chan result, 1)
		go func() {
			resp, err := newClient().Get(r.url("/slow"))
			if err != nil {
				got <- result{err: err}
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			got <- result{status: resp.StatusCode, body: string(body), close: resp.Close}
		}()
		<-entered

		shutdownDone := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			shutdownDone <- r.server.Shutdown(ctx)
		}()

		select {
		case err := <-shutdownDone:
			t.Fatalf("Shutdown returned (%v) while a request was still running", err)
		case <-time.After(150 * time.Millisecond):
		}

		close(release)
		res := <-got
		if res.err != nil || res.status != 200 || res.body != "finished" || !res.close {
			t.Fatalf("in-flight request: %+v (must finish normally and announce close)", res)
		}
		if err := <-shutdownDone; err != nil {
			t.Fatalf("Shutdown = %v", err)
		}
		<-r.done
		if r.err != nil {
			t.Fatalf("Serve = %v, want nil after a graceful shutdown", r.err)
		}
	})
}

func TestE2EShutdownDoesNotWaitForIdleKeepAliveConnections(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		r := start(t, tr(), echoRoutes)
		client := newClient()
		resp, err := client.Get(r.url("/hello/x"))
		if err != nil {
			t.Fatal(err)
		}
		_ = readAll(t, resp) // leaves an idle keep-alive connection open

		began := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.server.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(began); took > 2*time.Second {
			t.Fatalf("Shutdown took %v with only an idle connection open", took)
		}
		select {
		case <-r.done:
			if r.err != nil {
				t.Fatalf("Serve = %v", r.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Serve did not return")
		}
	})
}

func TestE2EShutdownTimeoutIsReported(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr func() network.Transport) {
		entered := make(chan struct{})
		release := make(chan struct{})
		r := start(t, tr(), func(s *framework.Server) {
			s.GET("/stuck", func(_ context.Context, c *framework.Context) {
				close(entered)
				<-release
				c.String(200, "late")
			})
		})
		defer close(release)

		go func() { _, _ = newClient().Get(r.url("/stuck")) }()
		<-entered

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := r.server.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown = %v, want DeadlineExceeded", err)
		}
	})
}
