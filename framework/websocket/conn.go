// Package websocket implements the server side of the WebSocket protocol
// (RFC 6455) on top of a hijacked framework connection.
//
// It supports text and binary messages, fragmentation, ping/pong, the closing
// handshake, a message size limit and subprotocol negotiation. Extensions such
// as permessage-deflate are not supported: any frame with a reserved bit set is
// rejected as a protocol error.
package websocket

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
	"unicode/utf8"
)

// Message and frame types (RFC 6455, section 11.8).
const (
	TextMessage   = 1
	BinaryMessage = 2
	CloseMessage  = 8
	PingMessage   = 9
	PongMessage   = 10

	opContinuation = 0
)

// Close status codes (RFC 6455, section 7.4.1).
const (
	CloseNormalClosure      = 1000
	CloseGoingAway          = 1001
	CloseProtocolError      = 1002
	CloseUnsupportedData    = 1003
	CloseNoStatusReceived   = 1005
	CloseAbnormalClosure    = 1006
	CloseInvalidPayload     = 1007
	ClosePolicyViolation    = 1008
	CloseMessageTooBig      = 1009
	CloseMandatoryExtension = 1010
	CloseInternalServerErr  = 1011
)

const (
	maxControlPayload  = 125
	defaultReadLimit   = 1 << 20
	defaultWriteTimout = 10 * time.Second
)

// ErrCloseSent is returned by writes after a close frame has been sent.
var ErrCloseSent = errors.New("websocket: close frame already sent")

// CloseError describes why a connection was closed by the peer, or by a
// protocol violation detected locally.
type CloseError struct {
	Code int
	Text string
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("websocket: close %d %s", e.Code, e.Text)
}

// Conn is a WebSocket connection. ReadMessage must be called from one
// goroutine at a time; the write methods are safe for concurrent use.
type Conn struct {
	raw          net.Conn
	reader       *bufio.Reader
	readLimit    int64
	writeTimeout time.Duration
	subprotocol  string

	writeMu   sync.Mutex
	closeSent bool

	// fragmentation state, touched only by the reading goroutine
	fragmented bool
	fragType   int
	fragBuf    []byte
}

func newConn(raw net.Conn, readLimit int64, writeTimeout time.Duration, subprotocol string) *Conn {
	if readLimit <= 0 {
		readLimit = defaultReadLimit
	}
	if writeTimeout <= 0 {
		writeTimeout = defaultWriteTimout
	}
	return &Conn{
		raw:          raw,
		reader:       bufio.NewReaderSize(raw, 4096),
		readLimit:    readLimit,
		writeTimeout: writeTimeout,
		subprotocol:  subprotocol,
	}
}

// Subprotocol returns the negotiated subprotocol, or "".
func (c *Conn) Subprotocol() string { return c.subprotocol }

// RemoteAddr returns the peer address.
func (c *Conn) RemoteAddr() net.Addr { return c.raw.RemoteAddr() }

// SetReadDeadline sets the deadline for ReadMessage. A zero value disables it.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.raw.SetReadDeadline(t) }

// Close closes the underlying connection without a closing handshake. Use
// WriteClose first for an orderly shutdown.
func (c *Conn) Close() error { return c.raw.Close() }

// ReadMessage returns the next complete data message. Ping frames are answered
// with a pong, pong frames are ignored, and fragmented messages are reassembled.
// A close frame from the peer is answered and reported as *CloseError. A
// protocol violation closes the connection with the matching status code and is
// also reported as *CloseError.
func (c *Conn) ReadMessage() (messageType int, data []byte, err error) {
	for {
		fin, opcode, payload, ferr := c.readFrame()
		if ferr != nil {
			return 0, nil, ferr
		}

		switch opcode {
		case PingMessage:
			if err := c.writeFrame(PongMessage, payload); err != nil && !errors.Is(err, ErrCloseSent) {
				return 0, nil, err
			}
			continue
		case PongMessage:
			continue
		case CloseMessage:
			return 0, nil, c.handleClose(payload)
		case TextMessage, BinaryMessage:
			if c.fragmented {
				return 0, nil, c.fail(CloseProtocolError, "new data frame inside a fragmented message")
			}
			c.fragType, c.fragBuf, c.fragmented = opcode, payload, !fin
		case opContinuation:
			if !c.fragmented {
				return 0, nil, c.fail(CloseProtocolError, "continuation frame without a message to continue")
			}
			if int64(len(c.fragBuf))+int64(len(payload)) > c.readLimit {
				return 0, nil, c.fail(CloseMessageTooBig, "message too big")
			}
			c.fragBuf = append(c.fragBuf, payload...)
			c.fragmented = !fin
		default:
			return 0, nil, c.fail(CloseProtocolError, "unknown opcode")
		}

		if c.fragmented {
			continue
		}
		message, kind := c.fragBuf, c.fragType
		c.fragBuf = nil
		if kind == TextMessage && !utf8.Valid(message) {
			return 0, nil, c.fail(CloseInvalidPayload, "text message is not valid UTF-8")
		}
		return kind, message, nil
	}
}

// readFrame reads one frame and unmasks it. Frames that break the protocol
// close the connection and are returned as *CloseError.
func (c *Conn) readFrame() (fin bool, opcode int, payload []byte, err error) {
	var header [2]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return false, 0, nil, c.readError(err)
	}

	fin = header[0]&0x80 != 0
	if header[0]&0x70 != 0 {
		return false, 0, nil, c.fail(CloseProtocolError, "reserved bits set without a negotiated extension")
	}
	opcode = int(header[0] & 0x0f)
	if header[1]&0x80 == 0 {
		return false, 0, nil, c.fail(CloseProtocolError, "client frames must be masked")
	}

	length := int64(header[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return false, 0, nil, c.readError(err)
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return false, 0, nil, c.readError(err)
		}
		unsigned := binary.BigEndian.Uint64(ext[:])
		if unsigned>>63 != 0 {
			return false, 0, nil, c.fail(CloseProtocolError, "frame length has its most significant bit set")
		}
		length = int64(unsigned)
	}

	if opcode >= CloseMessage { // control frame
		if !fin || length > maxControlPayload {
			return false, 0, nil, c.fail(CloseProtocolError, "invalid control frame")
		}
	} else if length > c.readLimit || int64(len(c.fragBuf))+length > c.readLimit {
		return false, 0, nil, c.fail(CloseMessageTooBig, "message too big")
	}

	var mask [4]byte
	if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
		return false, 0, nil, c.readError(err)
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return false, 0, nil, c.readError(err)
	}
	for i := range payload {
		payload[i] ^= mask[i&3]
	}
	return fin, opcode, payload, nil
}

// readError reports an abrupt end of the stream as close code 1006.
func (c *Conn) readError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return &CloseError{Code: CloseAbnormalClosure, Text: "connection closed without a close frame"}
	}
	return err
}

// handleClose answers the peer's close frame and returns the CloseError to report.
func (c *Conn) handleClose(payload []byte) error {
	switch {
	case len(payload) == 0:
		_ = c.writeClose(nil)
		return &CloseError{Code: CloseNoStatusReceived}
	case len(payload) == 1:
		return c.fail(CloseProtocolError, "close frame with a one-byte payload")
	}

	code := int(binary.BigEndian.Uint16(payload))
	reason := payload[2:]
	if !validCloseCode(code) {
		return c.fail(CloseProtocolError, "invalid close code")
	}
	if !utf8.Valid(reason) {
		return c.fail(CloseInvalidPayload, "close reason is not valid UTF-8")
	}
	_ = c.writeClose(payload[:2]) // echo the status code
	return &CloseError{Code: code, Text: string(reason)}
}

func validCloseCode(code int) bool {
	switch {
	case code >= 1000 && code <= 1003, code >= 1007 && code <= 1011, code >= 3000 && code <= 4999:
		return true
	}
	return false
}

// fail sends a close frame with code and returns the matching error.
func (c *Conn) fail(code int, text string) error {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, uint16(code))
	_ = c.writeClose(payload)
	return &CloseError{Code: code, Text: text}
}

// WriteMessage sends one unfragmented message. messageType is TextMessage,
// BinaryMessage, PingMessage, PongMessage or CloseMessage; control messages
// carry at most 125 bytes. It is safe to call from several goroutines.
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	switch messageType {
	case TextMessage, BinaryMessage:
	case PingMessage, PongMessage, CloseMessage:
		if len(data) > maxControlPayload {
			return errors.New("websocket: control frame payload exceeds 125 bytes")
		}
	default:
		return fmt.Errorf("websocket: invalid message type %d", messageType)
	}
	if messageType == CloseMessage {
		return c.writeClose(data)
	}
	return c.writeFrame(messageType, data)
}

// WriteClose sends a close frame with a status code and a short reason
// (at most 123 bytes). After it, only reads are meaningful: keep calling
// ReadMessage until it returns the peer's *CloseError, then Close.
func (c *Conn) WriteClose(code int, reason string) error {
	if len(reason) > maxControlPayload-2 {
		return errors.New("websocket: close reason is too long")
	}
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], reason)
	return c.writeClose(payload)
}

func (c *Conn) writeClose(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closeSent {
		return ErrCloseSent
	}
	c.closeSent = true
	return c.writeLocked(CloseMessage, payload)
}

func (c *Conn) writeFrame(opcode int, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closeSent {
		return ErrCloseSent
	}
	return c.writeLocked(opcode, payload)
}

// writeLocked writes one final, unmasked frame. The caller holds writeMu.
func (c *Conn) writeLocked(opcode int, payload []byte) error {
	var header [10]byte
	header[0] = 0x80 | byte(opcode)
	n := 2
	switch length := len(payload); {
	case length < 126:
		header[1] = byte(length)
	case length <= 0xffff:
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:], uint16(length))
		n = 4
	default:
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(length))
		n = 10
	}

	_ = c.raw.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	buffers := net.Buffers{header[:n], payload}
	_, err := buffers.WriteTo(c.raw)
	return err
}
