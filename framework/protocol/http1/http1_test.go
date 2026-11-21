package http1

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/searchboard/searchboard/framework/protocol"
)

// pieceReader implements network.Reader over data that "arrives" in small
// pieces, like a TCP stream. Peek pulls more pieces until n bytes are buffered
// and fails with io.EOF when the stream ends first.
type pieceReader struct {
	pending [][]byte
	buf     []byte
}

func newPieceReader(data string, pieceSize int) *pieceReader {
	r := &pieceReader{}
	for len(data) > 0 {
		n := min(pieceSize, len(data))
		r.pending = append(r.pending, []byte(data[:n]))
		data = data[n:]
	}
	return r
}

func (r *pieceReader) Peek(n int) ([]byte, error) {
	for len(r.buf) < n {
		if len(r.pending) == 0 {
			return nil, io.EOF
		}
		r.buf = append(r.buf, r.pending[0]...)
		r.pending = r.pending[1:]
	}
	return r.buf[:n], nil
}

func (r *pieceReader) Skip(n int) error {
	if n > len(r.buf) {
		return io.ErrShortBuffer
	}
	r.buf = r.buf[n:]
	return nil
}

func (r *pieceReader) Release() error { return nil }
func (r *pieceReader) Len() int       { return len(r.buf) }

// bufWriter implements network.Writer into memory.
type bufWriter struct {
	bytes.Buffer
	flushes int
}

func (w *bufWriter) WriteBinary(p []byte) (int, error) { return w.Write(p) }
func (w *bufWriter) Flush() error                      { w.flushes++; return nil }

func readRequest(t *testing.T, raw string, pieceSize int, lim Limits) (*protocol.Request, error) {
	t.Helper()
	r := newPieceReader(raw, pieceSize)
	req := &protocol.Request{}
	if err := ReadHeader(r, req, lim); err != nil {
		return req, err
	}
	return req, ReadBody(r, req, lim)
}

func TestReadSimpleGet(t *testing.T) {
	for _, pieceSize := range []int{1, 3, 7, 1 << 20} { // byte-by-byte up to one packet
		raw := "GET /a/b%20c?x=1&y=2 HTTP/1.1\r\nHost: example.com\r\nUser-Agent:  test \r\nX-Multi: 1\r\nx-multi: 2\r\n\r\n"
		req, err := readRequest(t, raw, pieceSize, DefaultLimits())
		if err != nil {
			t.Fatalf("piece %d: %v", pieceSize, err)
		}
		if req.Method != "GET" || req.Path != "/a/b%20c" || req.RawQuery != "x=1&y=2" || req.Host != "example.com" {
			t.Fatalf("piece %d: unexpected request line parse: %+v", pieceSize, req)
		}
		if got := req.Header.Get("user-agent"); got != "test" {
			t.Errorf("header value not trimmed: %q", got)
		}
		if got := req.Header.Values("X-Multi"); len(got) != 2 || got[0] != "1" || got[1] != "2" {
			t.Errorf("multi-value header: %v", got)
		}
		if req.Query().Get("y") != "2" {
			t.Errorf("query parse: %v", req.Query())
		}
	}
}

func TestReadSkipsLeadingEmptyLines(t *testing.T) {
	req, err := readRequest(t, "\r\n\r\nGET / HTTP/1.1\r\nHost: h\r\n\r\n", 5, DefaultLimits())
	if err != nil || req.Method != "GET" {
		t.Fatalf("req=%+v err=%v", req, err)
	}
}

func TestReadAbsoluteAndAsteriskTargets(t *testing.T) {
	req, err := readRequest(t, "GET http://proxy.example/x?q=1 HTTP/1.1\r\nHost: ignored\r\n\r\n", 64, DefaultLimits())
	if err != nil || req.Path != "/x" || req.RawQuery != "q=1" {
		t.Fatalf("absolute-form: %+v %v", req, err)
	}
	req, err = readRequest(t, "OPTIONS * HTTP/1.1\r\nHost: h\r\n\r\n", 64, DefaultLimits())
	if err != nil || req.Path != "*" {
		t.Fatalf("asterisk-form: %+v %v", req, err)
	}
}

func TestReadFixedBody(t *testing.T) {
	body := strings.Repeat("x", 100_000) // larger than one read step
	raw := "POST /p HTTP/1.1\r\nHost: h\r\nContent-Length: 100000\r\n\r\n" + body
	for _, pieceSize := range []int{1000, 1 << 20} {
		req, err := readRequest(t, raw, pieceSize, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if string(req.Body()) != body {
			t.Fatalf("piece %d: body mismatch (len %d)", pieceSize, len(req.Body()))
		}
		req.Reset() // must hand the pooled buffer back without panicking
	}
}

func TestReadChunkedBodyWithExtensionsAndTrailers(t *testing.T) {
	raw := "POST /p HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5;ext=1\r\nhello\r\n" +
		"6\r\n world\r\n" +
		"0\r\nX-Trailer: ignored\r\n\r\n"
	for _, pieceSize := range []int{1, 4, 1 << 20} {
		req, err := readRequest(t, raw, pieceSize, DefaultLimits())
		if err != nil || string(req.Body()) != "hello world" {
			t.Fatalf("piece %d: body=%q err=%v", pieceSize, req.Body(), err)
		}
	}
}

func TestPipelinedRequestsShareOneReader(t *testing.T) {
	raw := "GET /1 HTTP/1.1\r\nHost: h\r\n\r\n" +
		"POST /2 HTTP/1.1\r\nHost: h\r\nContent-Length: 3\r\n\r\nabc" +
		"GET /3 HTTP/1.1\r\nHost: h\r\n\r\n"
	r := newPieceReader(raw, 1<<20)

	var paths []string
	for {
		req := &protocol.Request{}
		if err := ReadHeader(r, req, DefaultLimits()); err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		if err := ReadBody(r, req, DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, req.Path+":"+string(req.Body()))
	}
	if strings.Join(paths, ",") != "/1:,/2:abc,/3:" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestCleanEOFBeforeAnyByte(t *testing.T) {
	err := ReadHeader(newPieceReader("", 1), &protocol.Request{}, DefaultLimits())
	if err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestTruncatedRequestIsUnexpectedEOF(t *testing.T) {
	_, err := readRequest(t, "GET / HTTP/1.1\r\nHost: h\r\n", 4, DefaultLimits())
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want ErrUnexpectedEOF", err)
	}
	_, err = readRequest(t, "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 10\r\n\r\nabc", 4, DefaultLimits())
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated body err = %v, want ErrUnexpectedEOF", err)
	}
}

func TestMalformedRequestsAreRejected(t *testing.T) {
	small := DefaultLimits()
	small.MaxHeaderBytes = 128
	small.MaxHeaderCount = 3
	small.MaxBodyBytes = 10

	tests := []struct {
		name   string
		raw    string
		lim    Limits
		status int
	}{
		{"bare LF line endings", "GET / HTTP/1.1\nHost: h\n\n", DefaultLimits(), 0}, // never finds CRLFCRLF: stream ends
		{"unsupported version", "GET / HTTP/2.0\r\nHost: h\r\n\r\n", DefaultLimits(), 505},
		{"not http", "GET / FTP/1.1\r\nHost: h\r\n\r\n", DefaultLimits(), 400},
		{"missing target", "GET  HTTP/1.1\r\nHost: h\r\n\r\n", DefaultLimits(), 400},
		{"bad method", "G@T / HTTP/1.1\r\nHost: h\r\n\r\n", DefaultLimits(), 400},
		{"relative target", "GET foo HTTP/1.1\r\nHost: h\r\n\r\n", DefaultLimits(), 400},
		{"missing host", "GET / HTTP/1.1\r\n\r\n", DefaultLimits(), 400},
		{"duplicate host", "GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\n\r\n", DefaultLimits(), 400},
		{"space before colon", "GET / HTTP/1.1\r\nHost: h\r\nX-A : b\r\n\r\n", DefaultLimits(), 400},
		{"obsolete line folding", "GET / HTTP/1.1\r\nHost: h\r\nX-A: b\r\n c\r\n\r\n", DefaultLimits(), 400},
		{"control char in value", "GET / HTTP/1.1\r\nHost: h\r\nX-A: b\x01\r\n\r\n", DefaultLimits(), 400},
		{"no colon", "GET / HTTP/1.1\r\nHost: h\r\nbogus\r\n\r\n", DefaultLimits(), 400},
		{"header too large", "GET / HTTP/1.1\r\nHost: h\r\nX-A: " + strings.Repeat("a", 200) + "\r\n\r\n", small, 431},
		{"too many headers", "GET / HTTP/1.1\r\nHost: h\r\nA: 1\r\nB: 2\r\nC: 3\r\n\r\n", small, 431},
		{"content-length and chunked", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 3\r\nTransfer-Encoding: chunked\r\n\r\n", DefaultLimits(), 400},
		{"conflicting content-length", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 3\r\nContent-Length: 4\r\n\r\nabcd", DefaultLimits(), 400},
		{"negative content-length", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: -1\r\n\r\n", DefaultLimits(), 400},
		{"signed content-length", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: +3\r\n\r\nabc", DefaultLimits(), 400},
		{"non-numeric content-length", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 3x\r\n\r\n", DefaultLimits(), 400},
		{"body too large", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 11\r\n\r\n01234567890", small, 413},
		{"unsupported transfer coding", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: gzip, chunked\r\n\r\n", DefaultLimits(), 501},
		{"chunked on http/1.0", "POST / HTTP/1.0\r\nTransfer-Encoding: chunked\r\n\r\n", DefaultLimits(), 400},
		{"bad chunk size", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\n", DefaultLimits(), 400},
		{"chunk too large", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\nb\r\n01234567890\r\n0\r\n\r\n", small, 413},
		{"chunk missing crlf", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabcXX0\r\n\r\n", DefaultLimits(), 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readRequest(t, tt.raw, 1<<20, tt.lim)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := StatusOf(err); got != tt.status {
				t.Fatalf("status = %d (%v), want %d", got, err, tt.status)
			}
		})
	}
}

func TestKeepAlive(t *testing.T) {
	tests := []struct {
		proto, connection string
		want              bool
	}{
		{"HTTP/1.1", "", true},
		{"HTTP/1.1", "close", false},
		{"HTTP/1.1", "Keep-Alive, Close", false},
		{"HTTP/1.0", "", false},
		{"HTTP/1.0", "keep-alive", true},
	}
	for _, tt := range tests {
		req := &protocol.Request{Proto: tt.proto}
		if tt.connection != "" {
			req.Header.Set("Connection", tt.connection)
		}
		if got := KeepAlive(req); got != tt.want {
			t.Errorf("%s %q: KeepAlive = %v, want %v", tt.proto, tt.connection, got, tt.want)
		}
	}
}

func TestExpectsContinue(t *testing.T) {
	req := &protocol.Request{Proto: "HTTP/1.1"}
	req.Header.Set("Expect", "100-Continue")
	if !ExpectsContinue(req) {
		t.Error("100-continue not detected")
	}
	req.Proto = "HTTP/1.0"
	if ExpectsContinue(req) {
		t.Error("HTTP/1.0 clients must not get 100 Continue")
	}
}

func TestWriteResponse(t *testing.T) {
	resp := &protocol.Response{StatusCode: 201}
	resp.Header.Set("Content-Type", "text/plain")
	resp.Header.Set("Date", "Mon, 01 Jan 2024 00:00:00 GMT")
	resp.Header.Set("Content-Length", "999") // must be ignored
	resp.Header.Set("Connection", "upgrade") // must be ignored
	resp.SetBodyString("hello")

	w := &bufWriter{}
	if err := WriteResponse(w, resp, WriteOptions{KeepAlive: true}); err != nil {
		t.Fatal(err)
	}
	want := "HTTP/1.1 201 Created\r\nContent-Type: text/plain\r\nDate: Mon, 01 Jan 2024 00:00:00 GMT\r\nContent-Length: 5\r\n\r\nhello"
	if w.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", w.String(), want)
	}
	if w.flushes != 1 {
		t.Errorf("flushes = %d, want 1", w.flushes)
	}
}

func TestWriteResponseAddsDateAndConnectionClose(t *testing.T) {
	resp := &protocol.Response{StatusCode: 200}
	w := &bufWriter{}
	if err := WriteResponse(w, resp, WriteOptions{KeepAlive: false}); err != nil {
		t.Fatal(err)
	}
	out := w.String()
	if !strings.Contains(out, "\r\nDate: ") || !strings.Contains(out, "\r\nConnection: close\r\n") || !strings.Contains(out, "Content-Length: 0") {
		t.Fatalf("unexpected response: %q", out)
	}
}

func TestWriteResponseBodylessStatuses(t *testing.T) {
	for _, status := range []int{204, 304, 100} {
		resp := &protocol.Response{StatusCode: status}
		resp.SetBodyString("must not be sent")
		w := &bufWriter{}
		if err := WriteResponse(w, resp, WriteOptions{KeepAlive: true}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(w.String(), "must not") || strings.Contains(w.String(), "Content-Length") {
			t.Errorf("status %d: body or length leaked: %q", status, w.String())
		}
	}
}

func TestWriteResponseHead(t *testing.T) {
	resp := &protocol.Response{StatusCode: 200}
	resp.SetBodyString("hello")
	w := &bufWriter{}
	if err := WriteResponse(w, resp, WriteOptions{KeepAlive: true, HeadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.String(), "Content-Length: 5\r\n") || strings.HasSuffix(w.String(), "hello") {
		t.Fatalf("HEAD response wrong: %q", w.String())
	}
}

func TestWriteResponseLargeBodyIsWrittenSeparately(t *testing.T) {
	resp := &protocol.Response{StatusCode: 200}
	resp.SetBody(bytes.Repeat([]byte("z"), inlineBodyLimit+1))
	w := &bufWriter{}
	if err := WriteResponse(w, resp, WriteOptions{KeepAlive: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(w.String(), strings.Repeat("z", inlineBodyLimit+1)) {
		t.Fatal("large body missing")
	}
}

func TestWriteResponseStopsHeaderInjection(t *testing.T) {
	resp := &protocol.Response{StatusCode: 200}
	resp.Header.Set("X-Evil", "a\r\nSet-Cookie: stolen=1")
	w := &bufWriter{}
	if err := WriteResponse(w, resp, WriteOptions{KeepAlive: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.String(), "\r\nSet-Cookie:") {
		t.Fatalf("header injection succeeded: %q", w.String())
	}
}

func TestWriteResponseInvalidStatusBecomes500(t *testing.T) {
	w := &bufWriter{}
	if err := WriteResponse(w, &protocol.Response{StatusCode: 0}, WriteOptions{KeepAlive: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.String(), "HTTP/1.1 500 Internal Server Error\r\n") {
		t.Fatalf("got %q", w.String())
	}
}

func TestWriteContinueAndError(t *testing.T) {
	w := &bufWriter{}
	if err := WriteContinue(w); err != nil || w.String() != "HTTP/1.1 100 Continue\r\n\r\n" {
		t.Fatalf("continue = %q, %v", w.String(), err)
	}

	w = &bufWriter{}
	if err := WriteError(w, 431, "too big"); err != nil {
		t.Fatal(err)
	}
	out := w.String()
	if !strings.HasPrefix(out, "HTTP/1.1 431 Request Header Fields Too Large\r\n") ||
		!strings.Contains(out, "Connection: close") || !strings.HasSuffix(out, "too big\n") {
		t.Fatalf("error response = %q", out)
	}
}
