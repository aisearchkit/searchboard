package framework

import (
	"net/http"
	"strings"
	"time"
)

// Cookie returns the value of the named request cookie and whether it exists.
// With several cookies of the same name the first one wins.
func (c *Context) Cookie(name string) (string, bool) {
	for _, line := range c.Request.Header.Values("Cookie") {
		cookies, err := http.ParseCookie(line)
		if err != nil {
			continue // ignore malformed lines, keep looking
		}
		for _, cookie := range cookies {
			if cookie.Name == name {
				return cookie.Value, true
			}
		}
	}
	return "", false
}

// SetCookie adds a Set-Cookie header. It returns an error, and sets nothing,
// when the cookie name or an attribute is invalid. Value and attributes are
// sanitized by net/http, so a handler cannot inject extra header content.
func (c *Context) SetCookie(cookie *http.Cookie) error {
	if err := cookie.Valid(); err != nil {
		return err
	}
	c.Response.Header.Add("Set-Cookie", cookie.String())
	return nil
}

// DeleteCookie asks the client to drop a cookie. Path and domain must match the
// ones the cookie was set with.
func (c *Context) DeleteCookie(name, path, domain string) error {
	return c.SetCookie(&http.Cookie{
		Name:    name,
		Path:    path,
		Domain:  domain,
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})
}

// SecureSessionCookie returns a cookie with safe defaults for session data:
// HttpOnly, Secure, SameSite=Lax and Path=/.
func SecureSessionCookie(name, value string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// mediaType returns the lower-cased media type of a Content-Type value without parameters.
func mediaType(contentType string) string {
	value, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(value))
}
