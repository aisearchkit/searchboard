package network_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework/network"
	nptransport "github.com/searchboard/searchboard/framework/network/netpoll"
	"github.com/searchboard/searchboard/framework/network/standard"
)

// lineEcho reads one line and writes it back upper-cased. It exercises
// Peek, Skip, Release, Len, WriteBinary and Flush through both transports.
func lineEcho(_ context.Context, c network.Conn) error {
	var line []byte
	for need := 1; ; need++ {
		if _, err := c.Peek(need); err != nil {
			return network.ErrCloseConn
		}
		buf, err := c.Peek(c.Len())
		if err != nil {
			return network.ErrCloseConn
		}
		if i := strings.IndexByte(string(buf), '\n'); i >= 0 {
			line = append(line, buf[:i+1]...)
			if err := c.Skip(i + 1); err != nil {
				return network.ErrCloseConn
			}
			break
		}
		need = len(buf)
	}
	_ = c.Release()

	if string(line) == "bye\n" {
		return network.ErrCloseConn
	}
	if _, err := c.WriteBinary([]byte(strings.ToUpper(string(line)))); err != nil {
		return err
	}
	return c.Flush()
}

func transports() map[string]func() network.Transport {
	return map[string]func() network.Transport{
		"standard": func() network.Transport { return standard.New() },
		"netpoll":  func() network.Transport { return nptransport.New() },
	}
}

func TestTransportsEchoAndClose(t *testing.T) {
	for name, build := range transports() {
		t.Run(name, func(t *testing.T) {
			tr := build()
			ln, err := tr.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			served := make(chan error, 1)
			go func() { served <- tr.Serve(ln, lineEcho) }()

			client, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(5 * time.Second))
			reader := bufio.NewReader(client)

			// Two requests on one keep-alive connection.
			for _, word := range []string{"hello", "world"} {
				if _, err := io.WriteString(client, word+"\n"); err != nil {
					t.Fatal(err)
				}
				got, err := reader.ReadString('\n')
				if err != nil || got != strings.ToUpper(word)+"\n" {
					t.Fatalf("echo = %q, %v", got, err)
				}
			}

			// Pipelined requests arrive in one packet.
			if _, err := io.WriteString(client, "a\nb\n"); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"A\n", "B\n"} {
				if got, err := reader.ReadString('\n'); err != nil || got != want {
					t.Fatalf("pipelined echo = %q, %v; want %q", got, err, want)
				}
			}

			// ErrCloseConn closes the connection cleanly.
			if _, err := io.WriteString(client, "bye\n"); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.ReadString('\n'); err != io.EOF {
				t.Fatalf("expected EOF after bye, got %v", err)
			}

			if err := tr.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-served:
				if err != nil {
					t.Fatalf("Serve returned %v after Shutdown, want nil", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Serve did not return after Shutdown")
			}
		})
	}
}

func TestShutdownBeforeServe(t *testing.T) {
	for name, build := range transports() {
		t.Run(name, func(t *testing.T) {
			tr := build()
			ln, err := tr.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			_ = tr.Shutdown(context.Background())
			if err := tr.Serve(ln, lineEcho); err != nil {
				t.Fatalf("Serve after Shutdown = %v, want nil", err)
			}
		})
	}
}
