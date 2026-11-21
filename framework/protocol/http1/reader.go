package http1

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/bytedance/gopkg/lang/mcache"

	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/protocol"
)

// Limits bound the resources one request may consume.
type Limits struct {
	MaxHeaderBytes int   // request line plus all header fields
	MaxHeaderCount int   // number of header fields
	MaxBodyBytes   int64 // decoded body size
}

// DefaultLimits are conservative defaults for API traffic.
func DefaultLimits() Limits {
	return Limits{MaxHeaderBytes: 32 << 10, MaxHeaderCount: 128, MaxBodyBytes: 4 << 20}
}

var (
	crlf     = []byte("\r\n")
	crlfcrlf = []byte("\r\n\r\n")
)

const (
	bodyReadStep   = 32 << 10
	maxChunkLine   = 4 << 10
	maxTrailerSize = 8 << 10
)

// ReadHeader reads the request line and header fields into req. It returns
// io.EOF when the peer closed the connection before sending anything.
func ReadHeader(r network.Reader, req *protocol.Request, lim Limits) error {
	block, err := readHeaderBlock(r, lim.MaxHeaderBytes)
	if err != nil {
		return err
	}
	parseErr := parseHeaderBlock(block, req, lim)
	if err := r.Skip(len(block)); err != nil {
		return err
	}
	_ = r.Release()
	return parseErr
}

// readHeaderBlock returns the bytes up to and including the blank line that ends
// the header. The slice stays valid until the caller skips it.
func readHeaderBlock(r network.Reader, maxBytes int) ([]byte, error) {
	// Tolerate empty lines before the request line (RFC 9112, section 2.2).
	for {
		if _, err := r.Peek(1); err != nil {
			return nil, err
		}
		if r.Len() < 2 {
			if _, err := r.Peek(2); err != nil {
				return nil, err
			}
		}
		head, _ := r.Peek(2)
		if !bytes.Equal(head, crlf) {
			break
		}
		if err := r.Skip(2); err != nil {
			return nil, err
		}
	}

	need, scanFrom := 1, 0
	for {
		if _, err := r.Peek(need); err != nil {
			return nil, headerReadError(err)
		}
		buf, err := r.Peek(r.Len())
		if err != nil {
			return nil, headerReadError(err)
		}
		if i := bytes.Index(buf[scanFrom:], crlfcrlf); i >= 0 {
			end := scanFrom + i + len(crlfcrlf)
			if end > maxBytes {
				return nil, errHeaderTooLarge
			}
			return buf[:end], nil
		}
		if len(buf) >= maxBytes {
			return nil, errHeaderTooLarge
		}
		scanFrom = max(len(buf)-len(crlfcrlf)+1, 0)
		need = len(buf) + 1
	}
}

// headerReadError turns "the buffer is full" conditions into 431 and keeps I/O
// errors (timeouts, closed connections) as they are.
func headerReadError(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF // the peer disappeared mid-header
	}
	if strings.Contains(err.Error(), "buffer full") {
		return errHeaderTooLarge
	}
	return err
}

func parseHeaderBlock(block []byte, req *protocol.Request, lim Limits) error {
	lines := bytes.Split(block[:len(block)-len(crlfcrlf)], crlf)

	if err := parseRequestLine(string(lines[0]), req); err != nil {
		return err
	}
	if len(lines)-1 > lim.MaxHeaderCount {
		return errHeaderTooLarge
	}

	for _, raw := range lines[1:] {
		line := string(raw)
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			return errBadRequest // empty line inside the block or obsolete line folding
		}
		name, value, found := strings.Cut(line, ":")
		if !found || !validToken(name) {
			return errBadRequest
		}
		value = strings.Trim(value, " \t")
		if !validFieldValue(value) {
			return errBadRequest
		}
		req.Header.Add(name, value)
	}

	hosts := req.Header.Values("Host")
	switch {
	case len(hosts) > 1:
		return errBadRequest
	case len(hosts) == 1:
		req.Host = hosts[0]
	case req.Host == "" && req.Proto == "HTTP/1.1":
		return errMissingHost
	}
	return nil
}

func parseRequestLine(line string, req *protocol.Request) error {
	method, rest, ok := strings.Cut(line, " ")
	if !ok || !validToken(method) {
		return errBadRequest
	}
	target, proto, ok := strings.Cut(rest, " ")
	if !ok || target == "" {
		return errBadRequest
	}
	if proto != "HTTP/1.1" && proto != "HTTP/1.0" {
		if strings.HasPrefix(proto, "HTTP/") {
			return errBadVersion
		}
		return errBadRequest
	}

	req.Method, req.Proto = method, proto
	if err := req.SetTarget(target); err != nil {
		return errBadRequest
	}
	return nil
}

// ExpectsContinue reports whether the client waits for "100 Continue" before
// sending the body.
func ExpectsContinue(req *protocol.Request) bool {
	return req.Proto == "HTTP/1.1" && strings.EqualFold(req.Header.Get("Expect"), "100-continue")
}

// KeepAlive reports whether the connection may serve another request after this one.
func KeepAlive(req *protocol.Request) bool {
	tokens := strings.ToLower(strings.Join(req.Header.Values("Connection"), ","))
	if req.Proto == "HTTP/1.0" {
		return containsToken(tokens, "keep-alive")
	}
	return !containsToken(tokens, "close")
}

func containsToken(list, want string) bool {
	for _, token := range strings.Split(list, ",") {
		if strings.TrimSpace(token) == want {
			return true
		}
	}
	return false
}

// ReadBody reads the request body announced by Content-Length or
// Transfer-Encoding: chunked into req.
func ReadBody(r network.Reader, req *protocol.Request, lim Limits) error {
	contentLength, hasLength, err := contentLength(req)
	if err != nil {
		return err
	}
	chunked, err := isChunked(req)
	if err != nil {
		return err
	}

	switch {
	case chunked && hasLength:
		return errConflictingBody // the classic request smuggling setup
	case chunked:
		return readChunked(r, req, lim)
	case hasLength && contentLength > 0:
		if contentLength > lim.MaxBodyBytes {
			return errBodyTooLarge
		}
		return readFixed(r, req, int(contentLength))
	}
	return nil
}

func contentLength(req *protocol.Request) (n int64, present bool, err error) {
	values := req.Header.Values("Content-Length")
	if len(values) == 0 {
		return 0, false, nil
	}
	for i, value := range values {
		value = strings.TrimSpace(value)
		parsed, perr := strconv.ParseUint(value, 10, 63)
		if perr != nil || value == "" || value[0] == '+' {
			return 0, false, errBadRequest
		}
		if i > 0 && int64(parsed) != n {
			return 0, false, errBadRequest // conflicting lengths
		}
		n = int64(parsed)
	}
	return n, true, nil
}

func isChunked(req *protocol.Request) (bool, error) {
	values := req.Header.Values("Transfer-Encoding")
	if len(values) == 0 {
		return false, nil
	}
	if req.Proto == "HTTP/1.0" {
		return false, errBadRequest
	}
	codings := strings.ToLower(strings.Join(values, ","))
	if strings.TrimSpace(codings) != "chunked" {
		return false, errNotImplemented
	}
	return true, nil
}

// readFixed copies n bytes into a pooled buffer, never asking the reader to
// buffer more than bodyReadStep at once.
func readFixed(r network.Reader, req *protocol.Request, n int) error {
	body := mcache.Malloc(n)
	for off := 0; off < n; {
		step := min(n-off, bodyReadStep)
		chunk, err := r.Peek(step)
		if err != nil {
			mcache.Free(body)
			return bodyReadError(err)
		}
		copy(body[off:], chunk)
		if err := r.Skip(step); err != nil {
			mcache.Free(body)
			return err
		}
		off += step
	}
	_ = r.Release()
	req.SetPooledBody(body)
	return nil
}

func readChunked(r network.Reader, req *protocol.Request, lim Limits) error {
	var body []byte
	for {
		line, err := readLine(r, maxChunkLine)
		if err != nil {
			return err
		}
		sizeText, _, _ := strings.Cut(line, ";") // ignore chunk extensions
		sizeText = strings.TrimSpace(sizeText)
		if sizeText == "" || len(sizeText) > 15 {
			return errBadRequest
		}
		size, perr := strconv.ParseUint(sizeText, 16, 63)
		if perr != nil {
			return errBadRequest
		}

		if size == 0 {
			if err := skipTrailers(r); err != nil {
				return err
			}
			_ = r.Release()
			req.SetBody(body)
			return nil
		}
		if int64(len(body))+int64(size) > lim.MaxBodyBytes {
			return errBodyTooLarge
		}

		for remaining := int(size); remaining > 0; {
			step := min(remaining, bodyReadStep)
			chunk, err := r.Peek(step)
			if err != nil {
				return bodyReadError(err)
			}
			body = append(body, chunk...)
			if err := r.Skip(step); err != nil {
				return err
			}
			remaining -= step
		}

		tail, err := r.Peek(2)
		if err != nil {
			return bodyReadError(err)
		}
		if !bytes.Equal(tail, crlf) {
			return errBadRequest
		}
		if err := r.Skip(2); err != nil {
			return err
		}
	}
}

// skipTrailers discards the optional trailer section after the last chunk.
func skipTrailers(r network.Reader) error {
	total := 0
	for {
		line, err := readLine(r, maxTrailerSize)
		if err != nil {
			return err
		}
		if line == "" {
			return nil
		}
		total += len(line)
		if total > maxTrailerSize {
			return errHeaderTooLarge
		}
	}
}

// readLine reads up to and consumes a CRLF-terminated line of at most limit bytes.
func readLine(r network.Reader, limit int) (string, error) {
	need, scanFrom := 1, 0
	for {
		if _, err := r.Peek(need); err != nil {
			return "", bodyReadError(err)
		}
		buf, err := r.Peek(r.Len())
		if err != nil {
			return "", bodyReadError(err)
		}
		if i := bytes.Index(buf[scanFrom:], crlf); i >= 0 {
			end := scanFrom + i
			line := string(buf[:end])
			if err := r.Skip(end + len(crlf)); err != nil {
				return "", err
			}
			return line, nil
		}
		if len(buf) > limit {
			return "", errBadRequest
		}
		scanFrom = max(len(buf)-1, 0)
		need = len(buf) + 1
	}
}

func bodyReadError(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// validToken reports whether s is a non-empty RFC 9110 token.
func validToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isTokenChar(s[i]) {
			return false
		}
	}
	return true
}

func isTokenChar(c byte) bool {
	switch {
	case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// validFieldValue rejects control characters other than horizontal tab.
func validFieldValue(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}
