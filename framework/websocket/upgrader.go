package websocket

import (
	"crypto/sha1" //nolint:gosec // mandated by RFC 6455 for the handshake, not used for security
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/searchboard/searchboard/framework"
)

// handshakeGUID is the constant from RFC 6455, section 1.3.
const handshakeGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Upgrader turns an HTTP request into a WebSocket connection. The zero value
// is ready to use and enforces a same-origin policy.
type Upgrader struct {
	// CheckOrigin decides whether a browser page from the request's Origin may
	// connect. The default accepts requests without an Origin header and
	// requests whose Origin host equals the Host header. Cross-origin access
	// must be allowed explicitly, for example with a list of known origins.
	CheckOrigin func(c *framework.Context) bool
	// Subprotocols lists the supported subprotocols in order of preference.
	Subprotocols []string
	// ReadLimit is the largest accepted message in bytes (default 1 MiB).
	ReadLimit int64
	// WriteTimeout bounds every write (default 10 seconds).
	WriteTimeout time.Duration
}

// Upgrade validates the handshake, switches the connection to WebSocket and runs
// handler with it. It blocks until handler returns, then closes the connection.
//
// When the request is not a valid upgrade, Upgrade writes an HTTP error
// response and returns an error; the handler is not called. The connection must
// be hijackable: the server needs the standard transport, see
// framework.ErrHijackUnsupported.
func (u *Upgrader) Upgrade(c *framework.Context, handler func(*Conn)) error {
	reject := func(status int, message string, err error) error {
		c.String(status, "%s", message)
		c.Abort()
		return err
	}

	if c.Method() != http.MethodGet {
		c.SetHeader("Allow", http.MethodGet)
		return reject(http.StatusMethodNotAllowed, "WebSocket handshake requires GET", errors.New("websocket: handshake method is not GET"))
	}
	if !hasToken(c.Request.Header.Values("Connection"), "upgrade") || !hasToken(c.Request.Header.Values("Upgrade"), "websocket") {
		return reject(http.StatusBadRequest, "not a WebSocket handshake", errors.New("websocket: missing Connection: Upgrade or Upgrade: websocket"))
	}
	if c.GetHeader("Sec-Websocket-Version") != "13" {
		c.SetHeader("Sec-Websocket-Version", "13")
		return reject(http.StatusUpgradeRequired, "unsupported WebSocket version", errors.New("websocket: unsupported version"))
	}
	key := c.GetHeader("Sec-Websocket-Key")
	if decoded, err := base64.StdEncoding.DecodeString(key); err != nil || len(decoded) != 16 {
		return reject(http.StatusBadRequest, "invalid Sec-WebSocket-Key", errors.New("websocket: invalid Sec-WebSocket-Key"))
	}
	check := u.CheckOrigin
	if check == nil {
		check = sameOrigin
	}
	if !check(c) {
		return reject(http.StatusForbidden, "origin not allowed", errors.New("websocket: origin not allowed"))
	}

	subprotocol := u.selectSubprotocol(c.Request.Header.Values("Sec-Websocket-Protocol"))

	raw, err := c.Hijack()
	if err != nil {
		return reject(http.StatusNotImplemented, "WebSocket is not available on this connection", err)
	}
	c.Abort()

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n"
	if subprotocol != "" {
		response += "Sec-WebSocket-Protocol: " + subprotocol + "\r\n"
	}
	response += "\r\n"

	_ = raw.SetWriteDeadline(time.Now().Add(defaultWriteTimout))
	if _, err := raw.Write([]byte(response)); err != nil {
		_ = raw.Close()
		return err
	}
	_ = raw.SetWriteDeadline(time.Time{})

	conn := newConn(raw, u.ReadLimit, u.WriteTimeout, subprotocol)
	defer conn.Close()
	handler(conn)
	return nil
}

// acceptKey computes Sec-WebSocket-Accept for a client key.
func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + handshakeGUID)) //nolint:gosec // see import
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (u *Upgrader) selectSubprotocol(offered []string) string {
	for _, header := range offered {
		for _, candidate := range strings.Split(header, ",") {
			candidate = strings.TrimSpace(candidate)
			for _, supported := range u.Subprotocols {
				if candidate == supported {
					return supported
				}
			}
		}
	}
	return ""
}

// hasToken reports whether a comma separated header list contains token, ignoring case.
func hasToken(values []string, token string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// sameOrigin accepts requests with no Origin header and requests whose Origin
// has the same host as the request.
func sameOrigin(c *framework.Context) bool {
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, c.Request.Host)
}
