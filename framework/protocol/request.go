package protocol

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bytedance/gopkg/lang/mcache"
)

// Request is a parsed HTTP request. Instances are pooled by the server, so a
// handler must not retain a Request, its Header or its Body after returning.
type Request struct {
	Method     string
	Proto      string // "HTTP/1.1" or "HTTP/1.0"
	RequestURI string // request target exactly as received
	// Path is the raw (still percent-encoded) path. Routing uses it as is and
	// decodes parameter values individually, so an encoded slash never changes
	// which route matches.
	Path       string
	RawQuery   string
	Host       string
	Header     Header
	RemoteAddr string

	body       []byte
	bodyPooled bool

	query       url.Values
	queryParsed bool
}

// Body returns the request body. The slice is valid until the request is reset.
func (r *Request) Body() []byte { return r.body }

// SetBody sets a caller-owned body.
func (r *Request) SetBody(body []byte) {
	r.releaseBody()
	r.body = body
}

// SetPooledBody gives the request ownership of a buffer obtained from
// mcache.Malloc. It is returned to the pool by Reset.
func (r *Request) SetPooledBody(body []byte) {
	r.releaseBody()
	r.body = body
	r.bodyPooled = true
}

func (r *Request) releaseBody() {
	if r.bodyPooled {
		mcache.Free(r.body)
	}
	r.body = nil
	r.bodyPooled = false
}

// Query returns the parsed query string. Malformed pairs are skipped.
func (r *Request) Query() url.Values {
	if !r.queryParsed {
		r.query, _ = url.ParseQuery(r.RawQuery) // keep the valid pairs on error
		if r.query == nil {
			r.query = url.Values{}
		}
		r.queryParsed = true
	}
	return r.query
}

// ErrBadTarget is returned by SetTarget for a malformed request target.
var ErrBadTarget = errors.New("protocol: malformed request target")

// SetTarget parses a request target in origin-form ("/a?b=c"), absolute-form
// ("http://host/a?b=c") or asterisk-form ("*") and fills RequestURI, Path,
// RawQuery and, for absolute-form, Host.
func (r *Request) SetTarget(target string) error {
	for i := 0; i < len(target); i++ {
		if target[i] < 0x21 || target[i] == 0x7f {
			return ErrBadTarget
		}
	}

	r.RequestURI = target
	r.queryParsed = false

	switch {
	case target == "*":
		r.Path, r.RawQuery = "*", ""
	case strings.HasPrefix(target, "/"):
		r.Path, r.RawQuery, _ = strings.Cut(target, "?")
	case strings.Contains(target, "://"):
		u, err := url.ParseRequestURI(target)
		if err != nil || u.Host == "" {
			return ErrBadTarget
		}
		r.Path, r.RawQuery, r.Host = u.EscapedPath(), u.RawQuery, u.Host
		if r.Path == "" {
			r.Path = "/"
		}
	default:
		return fmt.Errorf("%w: %q", ErrBadTarget, target)
	}
	return nil
}

// Reset clears the request for reuse and returns pooled memory.
func (r *Request) Reset() {
	r.releaseBody()
	r.Method, r.Proto, r.RequestURI, r.Path, r.RawQuery, r.Host, r.RemoteAddr = "", "", "", "", "", "", ""
	r.Header.Reset()
	r.query, r.queryParsed = nil, false
}
