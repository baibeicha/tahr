package jupyter

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	wsOpcodeContinuation = 0x0
	wsOpcodeText         = 0x1
	wsOpcodeBinary       = 0x2
	wsOpcodeClose        = 0x8
	wsOpcodePing         = 0x9
	wsOpcodePong         = 0xA
)

// WSConn is a pure-Go RFC 6455 WebSocket client connection.
type WSConn struct {
	conn    net.Conn
	br      *bufio.Reader
	bw      *bufio.Writer
	writeMu sync.Mutex
	readMu  sync.Mutex
	closed  bool
}

// DialWS establishes an RFC 6455 WebSocket client connection.
func DialWS(ctx context.Context, rawURL string, headers http.Header) (*WSConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid websocket url: %w", err)
	}

	isSecure := false
	port := u.Port()
	switch strings.ToLower(u.Scheme) {
	case "ws":
		if port == "" {
			port = "80"
		}
	case "wss":
		isSecure = true
		if port == "" {
			port = "443"
		}
	case "http":
		u.Scheme = "ws"
		if port == "" {
			port = "80"
		}
	case "https":
		u.Scheme = "wss"
		isSecure = true
		if port == "" {
			port = "443"
		}
	default:
		return nil, fmt.Errorf("unsupported websocket scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	addr := net.JoinHostPort(host, port)

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial tcp %s: %w", addr, err)
	}

	if isSecure {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: host,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("tls handshake failed for %s: %w", addr, err)
		}
		conn = tlsConn
	}

	// Generate 16 random bytes for Sec-WebSocket-Key
	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to generate websocket key: %w", err)
	}
	secKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	// Format HTTP Upgrade Request
	reqPath := u.RequestURI()
	if reqPath == "" {
		reqPath = "/"
	}

	reqBuf := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n", reqPath, u.Host, secKey)

	for k, vals := range headers {
		for _, v := range vals {
			reqBuf += fmt.Sprintf("%s: %s\r\n", k, v)
		}
	}
	reqBuf += "\r\n"

	if _, err := conn.Write([]byte(reqBuf)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to send websocket upgrade request: %w", err)
	}

	br := bufio.NewReader(conn)
	req, _ := http.NewRequest("GET", rawURL, nil)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read websocket handshake response: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake failed with status %d %s", resp.StatusCode, resp.Status)
	}

	return &WSConn{
		conn: conn,
		br:   br,
		bw:   bufio.NewWriter(conn),
	}, nil
}

// WriteTextMessage sends a UTF-8 text message frame to the server (masked per RFC 6455).
func (ws *WSConn) WriteTextMessage(msg []byte) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	if ws.closed {
		return errors.New("websocket connection closed")
	}

	return ws.writeFrame(wsOpcodeText, msg)
}

// writeFrame writes a single frame to the wire.
func (ws *WSConn) writeFrame(opcode int, payload []byte) error {
	var header [14]byte
	header[0] = byte(0x80 | (opcode & 0x0F)) // FIN = 1

	length := len(payload)
	headerLen := 2

	// Client-to-server frames MUST have mask bit set
	if length <= 125 {
		header[1] = byte(0x80 | length)
	} else if length <= 65535 {
		header[1] = byte(0x80 | 126)
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
		headerLen = 4
	} else {
		header[1] = byte(0x80 | 127)
		binary.BigEndian.PutUint64(header[2:10], uint64(length))
		headerLen = 10
	}

	// Generate 4-byte mask
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	copy(header[headerLen:headerLen+4], mask[:])
	headerLen += 4

	if _, err := ws.bw.Write(header[:headerLen]); err != nil {
		return err
	}

	// Apply XOR mask to payload
	maskedPayload := make([]byte, length)
	for i := 0; i < length; i++ {
		maskedPayload[i] = payload[i] ^ mask[i%4]
	}

	if _, err := ws.bw.Write(maskedPayload); err != nil {
		return err
	}

	return ws.bw.Flush()
}

// ReadTextMessage reads the next full text message from the connection.
func (ws *WSConn) ReadTextMessage(timeout time.Duration) ([]byte, error) {
	ws.readMu.Lock()
	defer ws.readMu.Unlock()

	if ws.closed {
		return nil, errors.New("websocket connection closed")
	}

	var accumulated []byte

	for {
		if timeout > 0 {
			_ = ws.conn.SetReadDeadline(time.Now().Add(timeout))
		} else {
			_ = ws.conn.SetReadDeadline(time.Time{})
		}

		b0, err := ws.br.ReadByte()
		if err != nil {
			return nil, err
		}
		fin := (b0 & 0x80) != 0
		opcode := int(b0 & 0x0F)

		b1, err := ws.br.ReadByte()
		if err != nil {
			return nil, err
		}
		masked := (b1 & 0x80) != 0
		payloadLen := int(b1 & 0x7F)

		if payloadLen == 126 {
			var l uint16
			if err := binary.Read(ws.br, binary.BigEndian, &l); err != nil {
				return nil, err
			}
			payloadLen = int(l)
		} else if payloadLen == 127 {
			var l uint64
			if err := binary.Read(ws.br, binary.BigEndian, &l); err != nil {
				return nil, err
			}
			payloadLen = int(l)
		}

		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(ws.br, mask[:]); err != nil {
				return nil, err
			}
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(ws.br, payload); err != nil {
			return nil, err
		}

		if masked {
			for i := 0; i < payloadLen; i++ {
				payload[i] ^= mask[i%4]
			}
		}

		switch opcode {
		case wsOpcodePing:
			// Respond with Pong
			ws.writeMu.Lock()
			_ = ws.writeFrame(wsOpcodePong, payload)
			ws.writeMu.Unlock()
			continue

		case wsOpcodePong:
			continue

		case wsOpcodeClose:
			ws.closed = true
			return nil, io.EOF

		case wsOpcodeText, wsOpcodeBinary, wsOpcodeContinuation:
			accumulated = append(accumulated, payload...)
			if fin {
				return accumulated, nil
			}
		default:
			// Ignore unknown opcodes
		}
	}
}

// Close closes the WebSocket connection.
func (ws *WSConn) Close() error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	if ws.closed {
		return nil
	}
	ws.closed = true

	// Send close frame
	_ = ws.writeFrame(wsOpcodeClose, []byte{})
	return ws.conn.Close()
}
