// Package middleware contains ready-made framework.HandlerFunc middleware.
//
// A sensible order is RequestID, AccessLog, Recovery: the log line then carries
// the request ID and still sees the 500 that Recovery produces.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/searchboard/searchboard/framework"
)

const (
	// RequestIDHeader is the header that carries the request ID.
	RequestIDHeader = "X-Request-Id"
	// RequestIDKey is the Context key under which the ID is stored.
	RequestIDKey = "request_id"

	maxRequestIDLen = 128
)

// RequestID assigns every request an ID, exposes it as the X-Request-Id
// response header and stores it in the Context. A client-supplied ID is kept
// only if it is short and uses safe characters; otherwise a random one is
// generated, so IDs can never inject log or header content.
func RequestID() framework.HandlerFunc {
	return func(ctx context.Context, c *framework.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Set(RequestIDKey, id)
		c.SetHeader(RequestIDHeader, id)
		c.Next(ctx)
	}
}

// GetRequestID returns the request ID stored by RequestID, or "".
func GetRequestID(c *framework.Context) string { return c.GetString(RequestIDKey) }

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		ok := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' ||
			ch == '-' || ch == '_' || ch == '.'
		if !ok {
			return false
		}
	}
	return true
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}
