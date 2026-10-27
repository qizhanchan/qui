// Package ws is a minimal RFC 6455 WebSocket — text/binary messages,
// fragmentation, ping/pong and close — enough for the agent protocol's
// JSON-RPC channel, with no dependency outside the standard library.
package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server side upgrades an HTTP request; client side (the qui-agent CLI,
// tests) dials a UDS or TCP address.

const (
	opContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	// MaxMessage bounds one inbound message: requests are small JSON.
	MaxMessage = 16 << 20
)

var ErrClosed = errors.New("websocket: closed")

type Conn struct {
	conn   net.Conn
	br     *bufio.Reader
	client bool // clients mask their frames; servers must not

	wmu    sync.Mutex
	closed bool
}

func accept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + guid))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// IsUpgrade reports whether r asks for a WebSocket.
func IsUpgrade(r *http.Request) bool {
	return headerHasToken(r.Header, "Connection", "upgrade") && headerHasToken(r.Header, "Upgrade", "websocket")
}

// Upgrade completes the server handshake and hijacks the connection.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if r.Method != http.MethodGet || !IsUpgrade(r) {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return nil, errors.New("websocket: not an upgrade request")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "unsupported websocket version", http.StatusUpgradeRequired)
		return nil, errors.New("websocket: bad version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("websocket: missing key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket unsupported", http.StatusInternalServerError)
		return nil, errors.New("websocket: response is not hijackable")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept(key) + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &Conn{conn: conn, br: rw.Reader}, nil
}

// Dial opens a client WebSocket on conn (already connected to the
// server) for path. header carries extra request headers (Authorization).
func Dial(conn net.Conn, host, path string, header http.Header) (*Conn, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n", path, host)
	fmt.Fprintf(&b, "Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", key)
	for k, vs := range header {
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, b.String()); err != nil {
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("websocket: handshake failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != accept(key) {
		return nil, errors.New("websocket: bad Sec-WebSocket-Accept")
	}
	_ = conn.SetDeadline(time.Time{})
	return &Conn{conn: conn, br: br, client: true}, nil
}

// ReadMessage returns the next complete data message, answering pings and
// close frames along the way.
func (c *Conn) ReadMessage() (op byte, data []byte, err error) {
	var msgOp byte
	var msg []byte
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.writeFrame(opClose, payload)
			c.Close()
			return 0, nil, io.EOF
		case OpText, OpBinary:
			if msgOp != 0 {
				return 0, nil, errors.New("websocket: new message inside a fragmented one")
			}
			msgOp = op
		case opContinuation:
			if msgOp == 0 {
				return 0, nil, errors.New("websocket: continuation without a message")
			}
		default:
			return 0, nil, fmt.Errorf("websocket: unknown opcode %#x", op)
		}
		if len(msg)+len(payload) > MaxMessage {
			return 0, nil, errors.New("websocket: message too large")
		}
		msg = append(msg, payload...)
		if fin {
			return msgOp, msg, nil
		}
	}
}

func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return
	}
	fin = hdr[0]&0x80 != 0
	op = hdr[0] & 0x0f
	masked := hdr[1]&0x80 != 0
	n := uint64(hdr[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	if n > MaxMessage {
		err = errors.New("websocket: frame too large")
		return
	}
	if masked == c.client {
		// Client frames must be masked, server frames must not be.
		err = errors.New("websocket: bad frame masking")
		return
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}

// WriteMessage sends one data message as a single frame.
func (c *Conn) WriteMessage(op byte, data []byte) error {
	return c.writeFrame(op, data)
}

func (c *Conn) writeFrame(op byte, data []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return ErrClosed
	}
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|op)
	maskBit := byte(0)
	if c.client {
		maskBit = 0x80
	}
	switch n := len(data); {
	case n < 126:
		hdr = append(hdr, maskBit|byte(n))
	case n <= 0xffff:
		hdr = append(hdr, maskBit|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, maskBit|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	payload := data
	if c.client {
		var mask [4]byte
		if _, err := rand.Read(mask[:]); err != nil {
			return err
		}
		hdr = append(hdr, mask[:]...)
		payload = make([]byte, len(data))
		for i := range data {
			payload[i] = data[i] ^ mask[i%4]
		}
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

// Close closes the underlying connection. Idempotent.
func (c *Conn) Close() error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}
