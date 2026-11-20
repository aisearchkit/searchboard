package esclient

import (
	"errors"
	"fmt"
)

// maxErrorBodyLen bounds how much of an error response is kept in APIError.
const maxErrorBodyLen = 512

// APIError is returned when the cluster answers with a non-2xx status.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("esclient: cluster returned HTTP %d: %s", e.Status, e.Body)
}

func newAPIError(status int, body []byte) *APIError {
	text := string(body)
	if len(text) > maxErrorBodyLen {
		text = text[:maxErrorBodyLen] + "..."
	}
	return &APIError{Status: status, Body: text}
}

// HasStatus reports whether err is an APIError with the given HTTP status.
func HasStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == status
}
