package webui

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const webUIWebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type webUIWebSocket struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

func acceptWebUIWebSocket(w http.ResponseWriter, r *http.Request) (*webUIWebSocket, error) {
	if !webUIWebSocketUpgradeRequest(r) {
		http.Error(w, "Bad websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("bad websocket upgrade")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Websocket unsupported", http.StatusInternalServerError)
		return nil, errors.New("websocket unsupported")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	accept := webUIWebSocketAccept(r.Header.Get("Sec-WebSocket-Key"))
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\n")
	_, _ = fmt.Fprintf(rw, "Upgrade: websocket\r\nConnection: Upgrade\r\n")
	_, _ = fmt.Fprintf(rw, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &webUIWebSocket{conn: conn, reader: rw.Reader}, nil
}

func webUIWebSocketUpgradeRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key")) != "" &&
		r.Header.Get("Sec-WebSocket-Version") == "13"
}

func webUIWebSocketAccept(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key) + webUIWebSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (ws *webUIWebSocket) ReadText() (string, error) {
	for {
		opcode, payload, err := ws.readFrame()
		if err != nil {
			return "", err
		}
		switch opcode {
		case 1:
			return string(payload), nil
		case 8:
			return "", io.EOF
		case 9:
			_ = ws.writeFrame(10, payload)
		}
	}
}

func (ws *webUIWebSocket) WriteJSON(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return ws.WriteText(string(raw))
}

func (ws *webUIWebSocket) WriteText(text string) error {
	return ws.writeFrame(1, []byte(text))
}

func (ws *webUIWebSocket) Close() error {
	_ = ws.writeFrame(8, nil)
	return ws.conn.Close()
}

func (ws *webUIWebSocket) readFrame() (byte, []byte, error) {
	first, err := ws.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := ws.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length, err := ws.readFrameLength(second)
	if err != nil {
		return 0, nil, err
	}
	mask, err := ws.readMask(second)
	if err != nil {
		return 0, nil, err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(ws.reader, payload); err != nil {
		return 0, nil, err
	}
	if mask != nil {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return first & 0x0f, payload, nil
}

func (ws *webUIWebSocket) readFrameLength(second byte) (int, error) {
	length := int(second & 0x7f)
	if length < 126 {
		return length, nil
	}
	raw := make([]byte, 2)
	if length == 127 {
		raw = make([]byte, 8)
	}
	if _, err := io.ReadFull(ws.reader, raw); err != nil {
		return 0, err
	}
	if length == 126 {
		return int(binary.BigEndian.Uint16(raw)), nil
	}
	value := binary.BigEndian.Uint64(raw)
	if value > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("websocket frame too large")
	}
	return int(value), nil
}

func (ws *webUIWebSocket) readMask(second byte) ([]byte, error) {
	if second&0x80 == 0 {
		return nil, nil
	}
	mask := make([]byte, 4)
	_, err := io.ReadFull(ws.reader, mask)
	return mask, err
}

func (ws *webUIWebSocket) writeFrame(opcode byte, payload []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	header := webUIWebSocketFrameHeader(opcode, len(payload))
	if _, err := ws.conn.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := ws.conn.Write(payload)
	return err
}

func webUIWebSocketFrameHeader(opcode byte, length int) []byte {
	header := []byte{0x80 | opcode}
	if length < 126 {
		return append(header, byte(length))
	}
	if length <= 65535 {
		out := append(header, 126, 0, 0)
		binary.BigEndian.PutUint16(out[2:], uint16(length))
		return out
	}
	out := append(header, 127, 0, 0, 0, 0, 0, 0, 0, 0)
	binary.BigEndian.PutUint64(out[2:], uint64(length))
	return out
}
