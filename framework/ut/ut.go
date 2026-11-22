// Package ut helps test handlers and middleware without a network: it runs a
// request straight through a Server's router and returns the response.
package ut

import (
	"context"
	"net/http"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/protocol"
)

// Header is one request header.
type Header struct {
	Key   string
	Value string
}

// Result is the response of PerformRequest. It owns its data.
type Result struct {
	Status int
	Header protocol.Header
	Body   []byte
}

// BodyString returns the body as a string.
func (r *Result) BodyString() string { return string(r.Body) }

// PerformRequest sends a request through s and returns the response. The target
// may include a query string. It panics on a malformed target, because that is
// a mistake in the test itself.
func PerformRequest(s *framework.Server, method, target string, body []byte, headers ...Header) *Result {
	req := &protocol.Request{Method: method, Proto: "HTTP/1.1", Scheme: "http", Host: "example.com", RemoteAddr: "192.0.2.1:1234"}
	if err := req.SetTarget(target); err != nil {
		panic("ut: " + err.Error())
	}
	for _, h := range headers {
		req.Header.Add(h.Key, h.Value)
	}
	if len(body) > 0 {
		req.SetBody(body)
	}

	resp := &protocol.Response{StatusCode: http.StatusOK}
	s.ServeRequest(context.Background(), req, resp)

	result := &Result{Status: resp.StatusCode, Body: append([]byte(nil), resp.Body()...)}
	resp.Header.VisitAll(func(key, value string) { result.Header.Add(key, value) })
	return result
}
