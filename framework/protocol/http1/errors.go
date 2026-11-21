// Package http1 reads HTTP/1.1 requests from and writes responses to a
// network.Reader / network.Writer. It is strict about framing so that request
// smuggling tricks are rejected instead of guessed at.
package http1

import (
	"errors"
	"net/http"
)

// ProtocolError is a client error that maps to an HTTP status.
type ProtocolError struct {
	Status  int
	Message string
}

func (e *ProtocolError) Error() string { return e.Message }

func newError(status int, message string) *ProtocolError {
	return &ProtocolError{Status: status, Message: message}
}

var (
	errBadRequest      = newError(http.StatusBadRequest, "malformed request")
	errHeaderTooLarge  = newError(http.StatusRequestHeaderFieldsTooLarge, "request header too large")
	errBodyTooLarge    = newError(http.StatusRequestEntityTooLarge, "request body too large")
	errBadVersion      = newError(http.StatusHTTPVersionNotSupported, "unsupported HTTP version")
	errNotImplemented  = newError(http.StatusNotImplemented, "unsupported transfer encoding")
	errMissingHost     = newError(http.StatusBadRequest, "missing Host header")
	errConflictingBody = newError(http.StatusBadRequest, "conflicting message framing headers")
)

// StatusOf returns the HTTP status for a parse error, or 0 if err is not a
// ProtocolError (for example an I/O error that needs no response).
func StatusOf(err error) int {
	var pe *ProtocolError
	if errors.As(err, &pe) {
		return pe.Status
	}
	return 0
}
