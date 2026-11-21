package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/searchboard/searchboard/framework"
)

// Recovery turns a panic in a later handler into a logged 500 JSON response.
// The stack trace goes to the log only, never to the client.
func Recovery(logger *slog.Logger) framework.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, c *framework.Context) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			requestID := GetRequestID(c)
			logger.Error("panic recovered",
				"panic", r,
				"method", c.Method(),
				"path", c.Path(),
				"request_id", requestID,
				"stack", string(debug.Stack()),
			)

			c.Response.Reset() // drop any half-written response
			if requestID != "" {
				c.SetHeader(RequestIDHeader, requestID)
			}
			c.AbortWithJSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}()
		c.Next(ctx)
	}
}
