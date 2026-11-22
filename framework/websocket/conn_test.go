package websocket

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// clientFrame builds a masked client frame. rsv holds the three reserved bits in 0x70 position.
func clientFrame(fin bool, opcode int, payload []byte, rsv byte, masked bool) []byte {
	first := byte(opcode) | rsv
	if fin {
		first |= 0x80
	}
	frame := []byte{first}

	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch length := len(payload); {
	case length < 126:
		frame = append(frame, maskBit|byte(length))
	case length <= 0xffff:
		frame = append(frame, maskBit|126, byte(length>>8), byte(length))
	default:
		frame = append(frame, maskBit|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(length))
	}

	if !masked {
		return append(frame, payload...)
	}
	key := [4]byte{0x12, 0x34, 0x56, 0x78}
	frame = append(frame, key[:]...)
	for i, b := range payload {
		frame = append(frame, b^key[i&3])
	}
	return frame
}

// serverFrame reads one unmasked server frame.
func serverFrame(t *testing.T, r io.Reader) (fin bool, opcode int, payload []byte) {
	t.Helper()
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		t.Fatalf("reading frame header: %v", err)
	}
	if header[1]&0x80 != 0 {
		t.Fatal("server frames must not be masked")
	}
	length := int(header[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		_, _ = io.ReadFull(r, ext[:])
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		_, _ = io.ReadFull(r, ext[:])
		length = int(binary.BigEndian.Uint64(ext[:]))
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatalf("reading frame payload: %v", err)
	}
	return header[0]&0x80 != 0, int(header[0] & 0x0f), payload
}

// pipe returns a server Conn and the client end of an in-memory connection.
func pipe(t *testing.T, readLimit int64) (*Conn, net.Conn) {
	t.Helper()
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { _ = serverEnd.Close(); _ = clientEnd.Close() })
	return newConn(serverEnd, readLimit, time.Second, ""), clientEnd
}

// send writes raw bytes from the client in the background (net.Pipe is synchronous).
func send(t *testing.T, client net.Conn, data ...[]byte) {
	t.Helper()
	go func() {
		for _, chunk := range data {
			if _, err := client.Write(chunk); err != nil {
				return
			}
		}
	}()
}

func TestAcceptKeyMatchesRFCExample(t *testing.T) {
	// RFC 6455, section 1.3.
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("acceptKey = %q", got)
	}
}

func TestReadMessagesOfEveryLengthEncoding(t *testing.T) {
	for _, size := range []int{0, 1, 125, 126, 65535, 65536, 200_000} {
		conn, client := pipe(t, 1<<20)
		payload := bytes.Repeat([]byte("a"), size)
		send(t, client, clientFrame(true, BinaryMessage, payload, 0, true))

		kind, data, err := conn.ReadMessage()
		if err != nil || kind != BinaryMessage || !bytes.Equal(data, payload) {
			t.Fatalf("size %d: kind=%d len=%d err=%v", size, kind, len(data), err)
		}
	}
}

func TestFragmentedMessageWithInterleavedPing(t *testing.T) {
	conn, client := pipe(t, 1<<20)
	send(t, client,
		clientFrame(false, TextMessage, []byte("Hel"), 0, true),
		clientFrame(true, PingMessage, []byte("p"), 0, true),
		clientFrame(false, opContinuation, []byte("l"), 0, true),
		clientFrame(true, opContinuation, []byte("o"), 0, true),
	)

	type result struct {
		kind int
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		kind, data, err := conn.ReadMessage()
		done <- result{kind, data, err}
	}()

	_, opcode, payload := serverFrame(t, client) // the pong must arrive while the message is still incomplete
	if opcode != PongMessage || string(payload) != "p" {
		t.Fatalf("expected a pong echoing the ping, got opcode %d %q", opcode, payload)
	}
	res := <-done
	if res.err != nil || res.kind != TextMessage || string(res.data) != "Hello" {
		t.Fatalf("message = %d %q %v", res.kind, res.data, res.err)
	}
}

func TestPongFramesAreIgnored(t *testing.T) {
	conn, client := pipe(t, 1<<20)
	send(t, client, clientFrame(true, PongMessage, []byte("x"), 0, true), clientFrame(true, TextMessage, []byte("after"), 0, true))
	if _, data, err := conn.ReadMessage(); err != nil || string(data) != "after" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestProtocolViolationsCloseTheConnection(t *testing.T) {
	validClose := func(code int) []byte {
		return binary.BigEndian.AppendUint16(nil, uint16(code))
	}
	tests := []struct {
		name  string
		limit int64
		wire  [][]byte
		code  int
	}{
		{"unmasked client frame", 1 << 20, [][]byte{clientFrame(true, TextMessage, []byte("x"), 0, false)}, CloseProtocolError},
		{"reserved bit set", 1 << 20, [][]byte{clientFrame(true, TextMessage, []byte("x"), 0x40, true)}, CloseProtocolError},
		{"reserved opcode", 1 << 20, [][]byte{clientFrame(true, 3, []byte("x"), 0, true)}, CloseProtocolError},
		{"fragmented control frame", 1 << 20, [][]byte{clientFrame(false, PingMessage, nil, 0, true)}, CloseProtocolError},
		{"oversized control frame", 1 << 20, [][]byte{clientFrame(true, PingMessage, make([]byte, 126), 0, true)}, CloseProtocolError},
		{"continuation without start", 1 << 20, [][]byte{clientFrame(true, opContinuation, []byte("x"), 0, true)}, CloseProtocolError},
		{"data frame inside a fragmented message", 1 << 20, [][]byte{
			clientFrame(false, TextMessage, []byte("a"), 0, true), clientFrame(true, TextMessage, []byte("b"), 0, true)}, CloseProtocolError},
		{"length with the top bit set", 1 << 20, [][]byte{{0x82, 0xff, 0x80, 0, 0, 0, 0, 0, 0, 0}}, CloseProtocolError},
		{"invalid UTF-8 text", 1 << 20, [][]byte{clientFrame(true, TextMessage, []byte{0xff, 0xfe}, 0, true)}, CloseInvalidPayload},
		{"message too big", 10, [][]byte{clientFrame(true, BinaryMessage, make([]byte, 11), 0, true)}, CloseMessageTooBig},
		{"fragments add up past the limit", 10, [][]byte{
			clientFrame(false, BinaryMessage, make([]byte, 6), 0, true), clientFrame(true, opContinuation, make([]byte, 6), 0, true)}, CloseMessageTooBig},
		{"one byte close payload", 1 << 20, [][]byte{clientFrame(true, CloseMessage, []byte{1}, 0, true)}, CloseProtocolError},
		{"close code 1005 on the wire", 1 << 20, [][]byte{clientFrame(true, CloseMessage, validClose(1005), 0, true)}, CloseProtocolError},
		{"close code below 1000", 1 << 20, [][]byte{clientFrame(true, CloseMessage, validClose(999), 0, true)}, CloseProtocolError},
		{"invalid UTF-8 close reason", 1 << 20, [][]byte{clientFrame(true, CloseMessage, append(validClose(1000), 0xff), 0, true)}, CloseInvalidPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, client := pipe(t, tt.limit)
			send(t, client, tt.wire...)

			errCh := make(chan error, 1)
			go func() { _, _, err := conn.ReadMessage(); errCh <- err }()

			_, opcode, payload := serverFrame(t, client)
			if opcode != CloseMessage || len(payload) < 2 || int(binary.BigEndian.Uint16(payload)) != tt.code {
				t.Fatalf("server answered opcode %d payload %v, want a close frame with code %d", opcode, payload, tt.code)
			}
			var closeErr *CloseError
			if err := <-errCh; !errors.As(err, &closeErr) || closeErr.Code != tt.code {
				t.Fatalf("ReadMessage error = %v, want CloseError %d", err, tt.code)
			}
		})
	}
}

func TestClosingHandshakeInitiatedByPeer(t *testing.T) {
	conn, client := pipe(t, 1<<20)
	closePayload := append(binary.BigEndian.AppendUint16(nil, CloseNormalClosure), "bye"...)
	send(t, client, clientFrame(true, CloseMessage, closePayload, 0, true))

	errCh := make(chan error, 1)
	go func() { _, _, err := conn.ReadMessage(); errCh <- err }()

	_, opcode, payload := serverFrame(t, client)
	if opcode != CloseMessage || binary.BigEndian.Uint16(payload) != CloseNormalClosure {
		t.Fatalf("close echo = opcode %d %v", opcode, payload)
	}
	var closeErr *CloseError
	if err := <-errCh; !errors.As(err, &closeErr) || closeErr.Code != CloseNormalClosure || closeErr.Text != "bye" {
		t.Fatalf("error = %v", err)
	}
	if err := conn.WriteMessage(TextMessage, []byte("late")); !errors.Is(err, ErrCloseSent) {
		t.Fatalf("write after close = %v, want ErrCloseSent", err)
	}
}

func TestCloseWithoutStatusCode(t *testing.T) {
	conn, client := pipe(t, 1<<20)
	send(t, client, clientFrame(true, CloseMessage, nil, 0, true))

	errCh := make(chan error, 1)
	go func() { _, _, err := conn.ReadMessage(); errCh <- err }()

	if _, opcode, payload := serverFrame(t, client); opcode != CloseMessage || len(payload) != 0 {
		t.Fatalf("expected an empty close frame, got %d %v", opcode, payload)
	}
	var closeErr *CloseError
	if err := <-errCh; !errors.As(err, &closeErr) || closeErr.Code != CloseNoStatusReceived {
		t.Fatalf("error = %v", err)
	}
}

func TestClosingHandshakeInitiatedByServer(t *testing.T) {
	conn, client := pipe(t, 1<<20)

	go func() { _ = conn.WriteClose(CloseGoingAway, "restart") }()
	_, opcode, payload := serverFrame(t, client)
	if opcode != CloseMessage || binary.BigEndian.Uint16(payload) != CloseGoingAway || string(payload[2:]) != "restart" {
		t.Fatalf("close frame = %d %v", opcode, payload)
	}

	send(t, client, clientFrame(true, CloseMessage, binary.BigEndian.AppendUint16(nil, CloseGoingAway), 0, true))
	var closeErr *CloseError
	if _, _, err := conn.ReadMessage(); !errors.As(err, &closeErr) || closeErr.Code != CloseGoingAway {
		t.Fatalf("error = %v", err)
	}
	if err := conn.WriteClose(CloseNormalClosure, ""); !errors.Is(err, ErrCloseSent) {
		t.Fatalf("second close = %v", err)
	}
}

func TestAbruptDisconnectIsReportedAs1006(t *testing.T) {
	conn, client := pipe(t, 1<<20)
	_ = client.Close()
	var closeErr *CloseError
	if _, _, err := conn.ReadMessage(); !errors.As(err, &closeErr) || closeErr.Code != CloseAbnormalClosure {
		t.Fatalf("error = %v, want code 1006", err)
	}
}

func TestWriteMessageEncodesEveryLengthClass(t *testing.T) {
	for _, size := range []int{0, 125, 126, 65535, 65536} {
		conn, client := pipe(t, 1<<20)
		payload := bytes.Repeat([]byte("z"), size)
		go func() { _ = conn.WriteMessage(TextMessage, payload) }()

		fin, opcode, got := serverFrame(t, client)
		if !fin || opcode != TextMessage || !bytes.Equal(got, payload) {
			t.Fatalf("size %d: fin=%v opcode=%d len=%d", size, fin, opcode, len(got))
		}
	}
}

func TestWriteMessageValidation(t *testing.T) {
	conn, _ := pipe(t, 1<<20)
	if err := conn.WriteMessage(42, nil); err == nil {
		t.Error("an unknown message type must be rejected")
	}
	if err := conn.WriteMessage(PingMessage, make([]byte, 126)); err == nil {
		t.Error("an oversized control payload must be rejected")
	}
	if err := conn.WriteClose(1000, string(make([]byte, 124))); err == nil {
		t.Error("an oversized close reason must be rejected")
	}
}

func TestConcurrentWritersNeverInterleaveFrames(t *testing.T) {
	conn, client := pipe(t, 1<<20)
	const writers, perWriter = 20, 25

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			message := bytes.Repeat([]byte{byte('A' + w)}, 300) // multi-byte length header
			for i := 0; i < perWriter; i++ {
				if err := conn.WriteMessage(BinaryMessage, message); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}

	for i := 0; i < writers*perWriter; i++ {
		_, _, payload := serverFrame(t, client)
		if len(payload) != 300 || bytes.Count(payload, payload[:1]) != 300 {
			t.Fatalf("frame %d is corrupt: interleaved writes", i)
		}
	}
	wg.Wait()
}
