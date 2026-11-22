package websocket_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/network/standard"
	"github.com/searchboard/searchboard/framework/ut"
	"github.com/searchboard/searchboard/framework/websocket"
)

const clientKey = "dGhlIHNhbXBsZSBub25jZQ=="

func quiet() framework.Option {
	return framework.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// echo is the handler used by most tests: it echoes every message until the
// connection closes, and reports the final error.
func echo(finished chan<- error) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				if finished != nil {
					finished <- err
				}
				return
			}
			if err := conn.WriteMessage(kind, data); err != nil {
				if finished != nil {
					finished <- err
				}
				return
			}
		}
	}
}

type serverUnderTest struct {
	server *framework.Server
	addr   string
}

// startWS serves a /ws endpoint on the standard transport, the only one that supports hijacking.
func startWS(t *testing.T, upgrader *websocket.Upgrader, handler func(*websocket.Conn), opts ...framework.Option) *serverUnderTest {
	t.Helper()
	opts = append(opts, quiet(), framework.WithHostPorts("127.0.0.1:0"), framework.WithTransport(standard.New()))
	s := framework.New(opts...)
	s.GET("/ws", func(_ context.Context, c *framework.Context) {
		_ = upgrader.Upgrade(c, handler)
	})
	s.GET("/ping", func(_ context.Context, c *framework.Context) { c.String(200, "pong") })

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
	return &serverUnderTest{server: s, addr: ln.Addr().String()}
}

type client struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
}

// dial performs the opening handshake by hand and returns the HTTP response.
func dial(t *testing.T, addr string, extra ...string) (*client, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	t.Cleanup(func() { _ = conn.Close() })

	request := "GET /ws HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + clientKey + "\r\nSec-WebSocket-Version: 13\r\n" + strings.Join(extra, "") + "\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, conn: conn, br: br}, resp
}

func (c *client) send(opcode byte, payload []byte) {
	c.t.Helper()
	frame := []byte{0x80 | opcode}
	switch n := len(payload); {
	case n < 126:
		frame = append(frame, 0x80|byte(n))
	case n <= 0xffff:
		frame = append(frame, 0x80|126, byte(n>>8), byte(n))
	default:
		frame = append(frame, 0x80|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(n))
	}
	mask := [4]byte{9, 8, 7, 6}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i&3])
	}
	if _, err := c.conn.Write(frame); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) recv() (opcode byte, payload []byte) {
	c.t.Helper()
	var header [2]byte
	if _, err := io.ReadFull(c.br, header[:]); err != nil {
		c.t.Fatalf("reading frame: %v", err)
	}
	length := int(header[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		_, _ = io.ReadFull(c.br, ext[:])
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		_, _ = io.ReadFull(c.br, ext[:])
		length = int(binary.BigEndian.Uint64(ext[:]))
	}
	payload = make([]byte, length)
	_, _ = io.ReadFull(c.br, payload)
	return header[0] & 0x0f, payload
}

func TestHandshakeAndEcho(t *testing.T) {
	s := startWS(t, &websocket.Upgrader{}, echo(nil))
	c, resp := dial(t, s.addr)
	defer resp.Body.Close()

	if resp.StatusCode != 101 || resp.Header.Get("Upgrade") != "websocket" ||
		resp.Header.Get("Sec-Websocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("handshake response: %d %v", resp.StatusCode, resp.Header)
	}

	for _, size := range []int{5, 200, 70_000} {
		payload := []byte(strings.Repeat("x", size))
		c.send(websocket.TextMessage, payload)
		if op, got := c.recv(); op != websocket.TextMessage || string(got) != string(payload) {
			t.Fatalf("echo of %d bytes: opcode %d, %d bytes back", size, op, len(got))
		}
	}
	c.send(websocket.PingMessage, []byte("hi"))
	if op, got := c.recv(); op != websocket.PongMessage || string(got) != "hi" {
		t.Fatalf("pong = %d %q", op, got)
	}
}

func TestSubprotocolNegotiation(t *testing.T) {
	upgrader := &websocket.Upgrader{Subprotocols: []string{"v2.searchboard", "v1.searchboard"}}
	s := startWS(t, upgrader, echo(nil))

	_, resp := dial(t, s.addr, "Sec-WebSocket-Protocol: other, v1.searchboard\r\n")
	if got := resp.Header.Get("Sec-Websocket-Protocol"); got != "v1.searchboard" {
		t.Errorf("negotiated %q, want the first mutually supported protocol", got)
	}
	_, resp = dial(t, s.addr, "Sec-WebSocket-Protocol: unknown\r\n")
	if resp.StatusCode != 101 || resp.Header.Get("Sec-Websocket-Protocol") != "" {
		t.Errorf("no common subprotocol: %d %q", resp.StatusCode, resp.Header.Get("Sec-Websocket-Protocol"))
	}
}

func TestOriginPolicy(t *testing.T) {
	s := startWS(t, &websocket.Upgrader{}, echo(nil))

	if _, resp := dial(t, s.addr, "Origin: http://evil.example\r\n"); resp.StatusCode != 403 {
		t.Errorf("cross-origin handshake = %d, want 403", resp.StatusCode)
	}
	if _, resp := dial(t, s.addr, "Origin: http://"+s.addr+"\r\n"); resp.StatusCode != 101 {
		t.Errorf("same-origin handshake = %d, want 101", resp.StatusCode)
	}
	if _, resp := dial(t, s.addr); resp.StatusCode != 101 {
		t.Errorf("no Origin header (non-browser client) = %d, want 101", resp.StatusCode)
	}

	allowList := &websocket.Upgrader{CheckOrigin: func(c *framework.Context) bool {
		return c.GetHeader("Origin") == "https://app.example.com"
	}}
	s2 := startWS(t, allowList, echo(nil))
	if _, resp := dial(t, s2.addr, "Origin: https://app.example.com\r\n"); resp.StatusCode != 101 {
		t.Errorf("allowed origin = %d", resp.StatusCode)
	}
	if _, resp := dial(t, s2.addr, "Origin: https://other.example.com\r\n"); resp.StatusCode != 403 {
		t.Errorf("other origin = %d", resp.StatusCode)
	}
}

func TestInvalidHandshakesAreRejectedWithoutHijacking(t *testing.T) {
	s := framework.New(quiet())
	var upgradeErr error
	s.Any("/ws", func(_ context.Context, c *framework.Context) {
		upgradeErr = (&websocket.Upgrader{}).Upgrade(c, func(*websocket.Conn) { t.Error("handler must not run") })
	})

	valid := []ut.Header{
		{Key: "Connection", Value: "keep-alive, Upgrade"},
		{Key: "Upgrade", Value: "WebSocket"},
		{Key: "Sec-WebSocket-Version", Value: "13"},
		{Key: "Sec-WebSocket-Key", Value: clientKey},
	}
	without := func(name string) []ut.Header {
		var out []ut.Header
		for _, h := range valid {
			if h.Key != name {
				out = append(out, h)
			}
		}
		return out
	}

	tests := []struct {
		name    string
		method  string
		headers []ut.Header
		want    int
	}{
		{"wrong method", "POST", valid, 405},
		{"missing Upgrade", "GET", without("Upgrade"), 400},
		{"missing Connection", "GET", without("Connection"), 400},
		{"missing version", "GET", without("Sec-WebSocket-Version"), 426},
		{"old version", "GET", append(without("Sec-WebSocket-Version"), ut.Header{Key: "Sec-WebSocket-Version", Value: "8"}), 426},
		{"missing key", "GET", without("Sec-WebSocket-Key"), 400},
		{"key of the wrong length", "GET", append(without("Sec-WebSocket-Key"), ut.Header{Key: "Sec-WebSocket-Key", Value: base64.StdEncoding.EncodeToString([]byte("short"))}), 400},
		{"key that is not base64", "GET", append(without("Sec-WebSocket-Key"), ut.Header{Key: "Sec-WebSocket-Key", Value: "***"}), 400},
		// A valid handshake cannot be completed without a real connection.
		{"valid handshake on an in-process request", "GET", valid, 501},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upgradeErr = nil
			got := ut.PerformRequest(s, tt.method, "/ws", nil, tt.headers...)
			if got.Status != tt.want || upgradeErr == nil {
				t.Fatalf("status = %d (error %v), want %d with an error", got.Status, upgradeErr, tt.want)
			}
			if tt.want == 426 && got.Header.Get("Sec-Websocket-Version") != "13" {
				t.Errorf("426 must advertise the supported version, got %q", got.Header.Get("Sec-Websocket-Version"))
			}
		})
	}
}

func TestDefaultNetpollTransportCannotHijack(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("netpoll is only the default transport on Linux and the BSDs")
	}
	s := framework.New(quiet(), framework.WithHostPorts("127.0.0.1:0"))
	s.GET("/ws", func(_ context.Context, c *framework.Context) {
		_ = (&websocket.Upgrader{}).Upgrade(c, echo(nil))
	})
	ln, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = s.Serve(ln); close(done) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		<-done
	}()

	_, resp := dial(t, ln.Addr().String())
	if resp.StatusCode != 501 {
		t.Fatalf("status = %d, want 501: WebSocket needs the standard transport", resp.StatusCode)
	}
}

func TestMessageLimitClosesWith1009(t *testing.T) {
	s := startWS(t, &websocket.Upgrader{ReadLimit: 100}, echo(nil))
	c, _ := dial(t, s.addr)

	c.send(websocket.BinaryMessage, make([]byte, 101))
	op, payload := c.recv()
	if op != websocket.CloseMessage || binary.BigEndian.Uint16(payload) != websocket.CloseMessageTooBig {
		t.Fatalf("expected close 1009, got opcode %d %v", op, payload)
	}
}

func TestCleanCloseEndsTheHandlerAndTheServerKeepsServing(t *testing.T) {
	finished := make(chan error, 1)
	s := startWS(t, &websocket.Upgrader{}, echo(finished))
	c, _ := dial(t, s.addr)

	c.send(websocket.CloseMessage, binary.BigEndian.AppendUint16(nil, websocket.CloseNormalClosure))
	if op, payload := c.recv(); op != websocket.CloseMessage || binary.BigEndian.Uint16(payload) != 1000 {
		t.Fatalf("close echo = %d %v", op, payload)
	}
	select {
	case err := <-finished:
		if ce, ok := err.(*websocket.CloseError); !ok || ce.Code != websocket.CloseNormalClosure {
			t.Fatalf("handler saw %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not finish")
	}

	resp, err := http.Get("http://" + s.addr + "/ping")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("server stopped serving after a WebSocket closed: %v", err)
	}
	resp.Body.Close()
}

func TestHijackedConnectionsDoNotBlockGracefulShutdown(t *testing.T) {
	handlerDone := make(chan struct{})
	s := startWS(t, &websocket.Upgrader{}, func(conn *websocket.Conn) {
		defer close(handlerDone)
		_, _, _ = conn.ReadMessage() // blocks until the server asks it to stop
	})

	c, resp := dial(t, s.addr)
	if resp.StatusCode != 101 {
		t.Fatalf("handshake = %d", resp.StatusCode)
	}

	began := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown = %v: an open WebSocket must not count as an in-flight request", err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("Shutdown took %v", took)
	}

	// The open WebSocket is the application's to close; Shutdown leaves it alone.
	c.send(websocket.CloseMessage, binary.BigEndian.AppendUint16(nil, websocket.CloseGoingAway))
	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never finished")
	}
}

func TestRegisterOnShutdownClosesWebSockets(t *testing.T) {
	connections := make(chan *websocket.Conn, 1)
	finished := make(chan struct{})
	s := startWS(t, &websocket.Upgrader{}, func(conn *websocket.Conn) {
		connections <- conn
		defer close(finished)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	s.server.RegisterOnShutdown(func() {
		select {
		case conn := <-connections:
			_ = conn.WriteClose(websocket.CloseGoingAway, "server restarting")
		default:
		}
	})

	c, _ := dial(t, s.addr)
	// Give the handler time to publish its connection.
	deadline := time.Now().Add(3 * time.Second)
	for len(connections) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = s.server.Shutdown(ctx) }()

	op, payload := c.recv()
	if op != websocket.CloseMessage || binary.BigEndian.Uint16(payload) != websocket.CloseGoingAway || string(payload[2:]) != "server restarting" {
		t.Fatalf("expected a 1001 close frame from the shutdown hook, got opcode %d %q", op, payload)
	}
	c.send(websocket.CloseMessage, binary.BigEndian.AppendUint16(nil, websocket.CloseGoingAway))
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not finish after the closing handshake")
	}
}
