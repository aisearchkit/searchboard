package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/searchboard/searchboard/framework"
)

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	// AllowOrigins lists exact origins such as "https://app.example.com", or
	// "*" to allow any origin. "*" cannot be combined with AllowCredentials.
	AllowOrigins []string
	// AllowMethods defaults to the common REST methods.
	AllowMethods []string
	// AllowHeaders defaults to echoing the headers the preflight asked for.
	AllowHeaders []string
	// ExposeHeaders are response headers that scripts may read.
	ExposeHeaders    []string
	AllowCredentials bool
	// MaxAge is how long browsers may cache a preflight answer.
	MaxAge time.Duration
}

var defaultCORSMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions,
}

// CORS answers cross-origin requests. Requests without an Origin header, and
// requests from origins that are not allowed, pass through untouched (the
// browser then blocks them). Preflight requests are answered with 204 and do
// not reach later handlers. It panics at construction on an unsafe combination
// of wildcard origin and credentials.
func CORS(cfg CORSConfig) framework.HandlerFunc {
	allowAll := false
	allowed := make(map[string]struct{}, len(cfg.AllowOrigins))
	for _, origin := range cfg.AllowOrigins {
		if origin == "*" {
			allowAll = true
			continue
		}
		allowed[origin] = struct{}{}
	}
	if allowAll && cfg.AllowCredentials {
		panic("middleware: CORS cannot combine a wildcard origin with credentials")
	}
	if len(cfg.AllowMethods) == 0 {
		cfg.AllowMethods = defaultCORSMethods
	}

	methods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")
	expose := strings.Join(cfg.ExposeHeaders, ", ")

	return func(ctx context.Context, c *framework.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next(ctx)
			return
		}

		c.Response.Header.Add("Vary", "Origin")
		if _, ok := allowed[origin]; !ok && !allowAll {
			c.Next(ctx)
			return
		}

		if allowAll {
			c.SetHeader("Access-Control-Allow-Origin", "*")
		} else {
			c.SetHeader("Access-Control-Allow-Origin", origin)
		}
		if cfg.AllowCredentials {
			c.SetHeader("Access-Control-Allow-Credentials", "true")
		}

		isPreflight := c.Method() == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != ""
		if !isPreflight {
			if expose != "" {
				c.SetHeader("Access-Control-Expose-Headers", expose)
			}
			c.Next(ctx)
			return
		}

		c.SetHeader("Access-Control-Allow-Methods", methods)
		if allowHeaders != "" {
			c.SetHeader("Access-Control-Allow-Headers", allowHeaders)
		} else if requested := c.GetHeader("Access-Control-Request-Headers"); requested != "" {
			c.SetHeader("Access-Control-Allow-Headers", requested)
			c.Response.Header.Add("Vary", "Access-Control-Request-Headers")
		}
		if cfg.MaxAge > 0 {
			c.SetHeader("Access-Control-Max-Age", strconv.Itoa(int(cfg.MaxAge/time.Second)))
		}
		c.AbortWithStatus(http.StatusNoContent)
	}
}
