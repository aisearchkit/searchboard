package http1

import (
	"bytes"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/protocol"
)

// inlineBodyLimit is the largest body copied next to the header so that small
// responses leave in a single write.
const inlineBodyLimit = 16 << 10

// WriteOptions control how a response is serialized.
type WriteOptions struct {
	KeepAlive bool // false adds "Connection: close"
	HeadOnly  bool // answer to HEAD: send headers (with Content-Length) but no body
}

var bufferPool = sync.Pool{New: func() any { return bytes.NewBuffer(make([]byte, 0, 1024)) }}

// WriteResponse serializes resp and flushes it. Content-Length, Connection and
// Transfer-Encoding are controlled by the framework and any value set by a
// handler for them is ignored. CR and LF inside header names or values are
// replaced by spaces to prevent response splitting.
func WriteResponse(w network.Writer, resp *protocol.Response, opts WriteOptions) error {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	status := resp.StatusCode
	if status < 100 || status > 999 {
		status = http.StatusInternalServerError
	}
	bodyAllowed := bodyAllowedFor(status)

	buf.WriteString("HTTP/1.1 ")
	buf.WriteString(strconv.Itoa(status))
	buf.WriteByte(' ')
	buf.WriteString(statusText(status))
	buf.WriteString("\r\n")

	hasDate := false
	resp.Header.VisitAll(func(key, value string) {
		switch key {
		case "Content-Length", "Connection", "Transfer-Encoding":
			return
		case "Date":
			hasDate = true
		}
		writeField(buf, key, value)
	})
	if !hasDate {
		writeField(buf, "Date", httpDate())
	}
	if bodyAllowed {
		writeField(buf, "Content-Length", strconv.Itoa(len(resp.Body())))
	}
	if !opts.KeepAlive {
		writeField(buf, "Connection", "close")
	}
	buf.WriteString("\r\n")

	body := resp.Body()
	sendBody := bodyAllowed && !opts.HeadOnly && len(body) > 0
	if sendBody && len(body) <= inlineBodyLimit {
		buf.Write(body)
		sendBody = false
	}

	if _, err := w.WriteBinary(buf.Bytes()); err != nil {
		return err
	}
	if sendBody {
		if _, err := w.WriteBinary(body); err != nil {
			return err
		}
	}
	// Flush before the pooled buffer is reused: writers may not copy.
	return w.Flush()
}

// WriteContinue sends the interim "100 Continue" response.
func WriteContinue(w network.Writer) error {
	if _, err := w.WriteBinary([]byte("HTTP/1.1 100 Continue\r\n\r\n")); err != nil {
		return err
	}
	return w.Flush()
}

// WriteError sends a minimal plain-text error response and asks to close the connection.
func WriteError(w network.Writer, status int, message string) error {
	resp := &protocol.Response{StatusCode: status}
	resp.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp.SetBodyString(message + "\n")
	return WriteResponse(w, resp, WriteOptions{KeepAlive: false})
}

func writeField(buf *bytes.Buffer, key, value string) {
	writeSanitized(buf, key)
	buf.WriteString(": ")
	writeSanitized(buf, value)
	buf.WriteString("\r\n")
}

func writeSanitized(buf *bytes.Buffer, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' {
			c = ' '
		}
		buf.WriteByte(c)
	}
}

func bodyAllowedFor(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}

func statusText(status int) string {
	if text := http.StatusText(status); text != "" {
		return text
	}
	return "Status " + strconv.Itoa(status)
}

// httpDate returns the current time formatted for a Date header. The value is
// cached for one second because formatting it per response is measurable.
var dateCache atomic.Pointer[dateEntry]

type dateEntry struct {
	second int64
	text   string
}

func httpDate() string {
	now := time.Now()
	if entry := dateCache.Load(); entry != nil && entry.second == now.Unix() {
		return entry.text
	}
	entry := &dateEntry{second: now.Unix(), text: now.UTC().Format(http.TimeFormat)}
	dateCache.Store(entry)
	return entry.text
}
