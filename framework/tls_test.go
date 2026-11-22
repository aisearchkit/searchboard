package framework_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework"
)

// selfSigned returns a certificate valid for 127.0.0.1.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "searchboard test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func tlsRoutes(s *framework.Server) {
	s.GET("/info", func(_ context.Context, c *framework.Context) {
		c.JSON(200, map[string]string{"proto": c.Request.Proto, "scheme": c.Scheme(), "path": c.Path()})
	})
	s.POST("/echo", func(_ context.Context, c *framework.Context) { c.Data(200, "application/octet-stream", c.Body()) })
	s.GET("/big", func(_ context.Context, c *framework.Context) {
		c.Data(200, "application/octet-stream", bytes.Repeat([]byte("0123456789abcdef"), 64<<10))
	})
	s.GET("/cookie", func(_ context.Context, c *framework.Context) {
		value, _ := c.Cookie("session")
		_ = c.SetCookie(&http.Cookie{Name: "seen", Value: value})
		c.String(200, "%s", value)
	})
	s.GET("/panic", func(context.Context, *framework.Context) { panic("boom") })
	s.GET("/cancel", func(ctx context.Context, c *framework.Context) {
		<-ctx.Done() // HTTP/2 cancels the request context when the client resets the stream
		canceled <- ctx.Err()
	})
}

var canceled = make(chan error, 4)

// startTLS serves over TLS with the default (standard) transport and returns its address.
func startTLS(t *testing.T, opts ...framework.Option) *running {
	t.Helper()
	cfg := &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}}
	return start(t, nil, tlsRoutes, append(opts, framework.WithTLS(cfg))...)
}

func (r *running) tlsURL(path string) string { return "https://" + r.addr + path }

func tlsClient(http2 bool) *http.Client {
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test certificate
		ForceAttemptHTTP2: http2,
	}
	if !http2 {
		transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{} // disables h2
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

func TestTLSWithHTTP1(t *testing.T) {
	r := startTLS(t)
	resp, err := tlsClient(false).Get(r.tlsURL("/info"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.ProtoMajor != 1 || resp.TLS == nil {
		t.Fatalf("expected HTTP/1.1 over TLS, got proto %d tls=%v", resp.ProtoMajor, resp.TLS != nil)
	}
	if got := readAll(t, resp); got != `{"path":"/info","proto":"HTTP/1.1","scheme":"https"}` {
		t.Fatalf("body = %s", got)
	}
}

func TestHTTP2IsNegotiatedAndServesRequests(t *testing.T) {
	r := startTLS(t)
	client := tlsClient(true)

	resp, err := client.Get(r.tlsURL("/info"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.ProtoMajor != 2 || resp.TLS == nil || resp.TLS.NegotiatedProtocol != "h2" {
		t.Fatalf("expected HTTP/2, got proto %d", resp.ProtoMajor)
	}
	if got := readAll(t, resp); got != `{"path":"/info","proto":"HTTP/2.0","scheme":"https"}` {
		t.Fatalf("body = %s", got)
	}

	payload := bytes.Repeat([]byte("p"), 1<<20)
	resp, err = client.Post(r.tlsURL("/echo"), "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, resp); got != string(payload) {
		t.Errorf("1 MiB echo over h2 returned %d bytes", len(got))
	}

	resp, err = client.Get(r.tlsURL("/big"))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, resp); len(got) != 1<<20 {
		t.Errorf("large response over h2 = %d bytes", len(got))
	}

	for path, want := range map[string]int{"/missing": 404, "/echo": 405, "/panic": 500} {
		resp, err := client.Get(r.tlsURL(path))
		if err != nil {
			t.Fatal(err)
		}
		_ = readAll(t, resp)
		if resp.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}

func TestHTTP2HeadCookiesAndConcurrentStreams(t *testing.T) {
	r := startTLS(t)
	client := tlsClient(true)

	resp, err := client.Head(r.tlsURL("/info"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength <= 0 {
		t.Errorf("HEAD = %d, Content-Length %d", resp.StatusCode, resp.ContentLength)
	}

	req, _ := http.NewRequest("GET", r.tlsURL("/cookie"), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, resp); got != "abc" || len(resp.Cookies()) != 1 || resp.Cookies()[0].Value != "abc" {
		t.Errorf("cookie round trip: body %q cookies %v", got, resp.Cookies())
	}

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Post(r.tlsURL("/echo"), "text/plain", strings.NewReader("stream"))
			if err != nil {
				errs <- err
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(body) != "stream" || resp.ProtoMajor != 2 {
				errs <- errors.New("concurrent stream returned a wrong response")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestHTTP2RequestBodyLimit(t *testing.T) {
	r := startTLS(t, framework.WithMaxBodyBytes(1000))
	resp, err := tlsClient(true).Post(r.tlsURL("/echo"), "text/plain", bytes.NewReader(make([]byte, 5000)))
	if err != nil {
		t.Fatal(err)
	}
	_ = readAll(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestHTTP2ClientCancellationReachesTheHandler(t *testing.T) {
	r := startTLS(t)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", r.tlsURL("/cancel"), nil)

	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	if _, err := tlsClient(true).Do(req); err == nil {
		t.Fatal("the canceled request must fail on the client")
	}
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handler saw %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler's context was never canceled")
	}
}

func TestHTTP2CanBeDisabled(t *testing.T) {
	r := startTLS(t, framework.WithHTTP2(false))
	resp, err := tlsClient(true).Get(r.tlsURL("/info")) // the client offers h2, the server declines
	if err != nil {
		t.Fatal(err)
	}
	_ = readAll(t, resp)
	if resp.ProtoMajor != 1 {
		t.Fatalf("proto = %d, want 1 when HTTP/2 is disabled", resp.ProtoMajor)
	}
}

func TestTLSMinimumVersionDefaultsTo12(t *testing.T) {
	r := startTLS(t)
	conn, err := tls.Dial("tcp", r.addr, &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS11}) //nolint:gosec // test certificate
	if err == nil {
		_ = conn.Close()
		t.Fatal("a TLS 1.1 handshake must be refused")
	}
}

func TestTLSShutdownWithIdleHTTP2Connection(t *testing.T) {
	r := startTLS(t)
	client := tlsClient(true)
	resp, err := client.Get(r.tlsURL("/info"))
	if err != nil {
		t.Fatal(err)
	}
	_ = readAll(t, resp) // leaves an idle HTTP/2 connection open

	began := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("Shutdown took %v with only an idle HTTP/2 connection", took)
	}
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatalf("Serve = %v", r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestPlainTextClientCannotTalkToTLSServer(t *testing.T) {
	r := startTLS(t)
	conn, err := net.Dial("tcp", r.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(conn, "GET /info HTTP/1.1\r\nHost: h\r\n\r\n")

	reply, _ := io.ReadAll(conn)
	if strings.Contains(string(reply), "HTTP/1.1 200") {
		t.Fatal("a plain-text request must never be served by a TLS listener")
	}
}
