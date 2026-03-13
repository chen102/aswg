package server

import (
	"bytes"
	"net"
	"testing"
	"time"
)

type shortWriteConn struct {
	buf      bytes.Buffer
	maxChunk int
}

func (c *shortWriteConn) Read(b []byte) (int, error)         { return 0, nil }
func (c *shortWriteConn) Close() error                       { return nil }
func (c *shortWriteConn) LocalAddr() net.Addr                { return dummyAddr("local") }
func (c *shortWriteConn) RemoteAddr() net.Addr               { return dummyAddr("remote") }
func (c *shortWriteConn) SetDeadline(_ time.Time) error      { return nil }
func (c *shortWriteConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *shortWriteConn) SetWriteDeadline(_ time.Time) error { return nil }

func (c *shortWriteConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := len(p)
	if c.maxChunk > 0 && n > c.maxChunk {
		n = c.maxChunk
	}
	if _, err := c.buf.Write(p[:n]); err != nil {
		return 0, err
	}
	return n, nil
}

type dummyAddr string

func (d dummyAddr) Network() string { return "tcp" }
func (d dummyAddr) String() string  { return string(d) }

func TestWriteAllHandlesShortWrites(t *testing.T) {
	conn := &shortWriteConn{maxChunk: 3}
	payload := []byte("abcdefghijklmnopqrstuvwxyz")

	if err := writeAll(conn, payload); err != nil {
		t.Fatalf("writeAll() error = %v", err)
	}
	if got := conn.buf.Bytes(); !bytes.Equal(got, payload) {
		t.Fatalf("writeAll() payload mismatch: got %q, want %q", string(got), string(payload))
	}
}

func TestWSConnWriteFrameHandlesShortWrites(t *testing.T) {
	conn := &shortWriteConn{maxChunk: 2}
	ws := &wsConn{conn: conn}
	payload := []byte("hello websocket")

	if err := ws.writeFrame(0x1, payload); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}

	raw := conn.buf.Bytes()
	if len(raw) < 2 {
		t.Fatalf("raw frame too short: %d", len(raw))
	}
	if raw[0] != 0x81 {
		t.Fatalf("unexpected opcode byte: got 0x%x", raw[0])
	}
	if got := raw[len(raw)-len(payload):]; !bytes.Equal(got, payload) {
		t.Fatalf("frame payload mismatch: got %q, want %q", string(got), string(payload))
	}
}
