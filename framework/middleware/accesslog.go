package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/searchboard/searchboard/framework"
)

// AccessLog logs one structured line per request after it has been handled.
func AccessLog(logger *slog.Logger) framework.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, c *framework.Context) {
		start := time.Now()
		c.Next(ctx)

		logger.LogAttrs(ctx, slog.LevelInfo, "http request",
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.String("route", c.FullPath()),
			slog.Int("status", c.Response.StatusCode),
			slog.Int("bytes", len(c.Response.Body())),
			slog.Duration("duration", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
			slog.String("request_id", GetRequestID(c)),
		)
	}
}
