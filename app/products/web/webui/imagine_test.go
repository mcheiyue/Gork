package webui

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/platform/auth"
)

func TestWebUIImagineWebSocketStreamsStartEvents(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIImagineRunID = func() string { return "run-1" }
	webUIImagineDefaultNSFW = func() bool { return false }

	var gotPrompt string
	var gotOptions webUIImagineOptions
	webUIImagineEvents = func(_ context.Context, prompt string, options webUIImagineOptions) ([]map[string]any, bool, error) {
		gotPrompt, gotOptions = prompt, options
		return []map[string]any{
			{"type": "progress", "progress": 50},
			{"type": "image", "url": "https://example.test/image.jpg", "is_final": true},
		}, true, nil
	}

	client := newWebUIWSTestClient(t, "Bearer web")
	defer client.Close()
	client.SendJSON(map[string]any{
		"type": "start", "prompt": "  cats  ", "aspect_ratio": "1024x1024",
		"quality": "QUALITY", "count": 9,
	})

	running := client.ReadJSON()
	if running["status"] != "running" || running["prompt"] != "cats" || running["aspect_ratio"] != "1:1" {
		t.Fatalf("running payload = %#v", running)
	}
	if running["run_id"] != "run-1" || running["quality"] != "quality" || running["count"].(float64) != 6 {
		t.Fatalf("running metadata = %#v", running)
	}
	progress := client.ReadJSON()
	if progress["type"] != "progress" || progress["run_id"] != "run-1" {
		t.Fatalf("progress payload = %#v", progress)
	}
	image := client.ReadJSON()
	if image["type"] != "image" || image["run_id"] != "run-1" {
		t.Fatalf("image payload = %#v", image)
	}
	completed := client.ReadJSON()
	if completed["status"] != "completed" || completed["run_id"] != "run-1" || completed["count"].(float64) != 6 {
		t.Fatalf("completed payload = %#v", completed)
	}
	if gotPrompt != "cats" || gotOptions.AspectRatio != "1:1" || gotOptions.Count != 6 || !gotOptions.EnablePro {
		t.Fatalf("stream args prompt=%q options=%#v", gotPrompt, gotOptions)
	}
	if gotOptions.EnableNSFW == nil || *gotOptions.EnableNSFW {
		t.Fatalf("default nsfw = %#v", gotOptions.EnableNSFW)
	}
}

func TestWebUIImagineWebSocketMessageErrorsMatchPythonShape(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	client := newWebUIWSTestClient(t, "Bearer web")
	defer client.Close()

	client.SendText("{")
	assertWebUIWSError(t, client.ReadJSON(), "Invalid message format.", "invalid_payload")
	client.SendJSON(map[string]any{"type": "start", "prompt": "   "})
	assertWebUIWSError(t, client.ReadJSON(), "Prompt cannot be empty.", "invalid_prompt")
	client.SendJSON(map[string]any{"type": "wat"})
	assertWebUIWSError(t, client.ReadJSON(), "Unknown action.", "invalid_action")
}

func TestWebUIImagineWebSocketNoAccountAndStop(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web", WebUIEnabled: true} }
	webUIImagineRunID = func() string { return "run-stop" }
	webUIImagineEvents = func(ctx context.Context, _ string, _ webUIImagineOptions) ([]map[string]any, bool, error) {
		<-ctx.Done()
		return nil, true, ctx.Err()
	}

	client := newWebUIWSTestClient(t, "")
	defer client.Close()
	client.SendJSON(map[string]any{"type": "start", "prompt": "slow"})
	_ = client.ReadJSON()
	client.SendJSON(map[string]any{"type": "stop"})
	stopped := client.ReadJSON()
	if stopped["type"] != "status" || stopped["status"] != "stopped" || stopped["run_id"] != "run-stop" {
		t.Fatalf("stopped payload = %#v", stopped)
	}

	webUIImagineEvents = func(context.Context, string, webUIImagineOptions) ([]map[string]any, bool, error) {
		return nil, false, nil
	}
	client.SendJSON(map[string]any{"type": "start", "prompt": "again"})
	_ = client.ReadJSON()
	assertWebUIWSError(t, client.ReadJSON(), "No available accounts for this model tier", "rate_limit_exceeded")
}

func TestWebUIImagineWebSocketInternalRunErrorMatchesPythonShape(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIImagineEvents = func(context.Context, string, webUIImagineOptions) ([]map[string]any, bool, error) {
		return nil, true, errors.New("stream failed")
	}

	client := newWebUIWSTestClient(t, "Bearer web")
	defer client.Close()
	client.SendJSON(map[string]any{"type": "start", "prompt": "boom"})
	_ = client.ReadJSON()
	assertWebUIWSError(t, client.ReadJSON(), "stream failed", "internal_error")
}

func TestWebUIImagineWebSocketRejectsInvalidToken(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	server := httptest.NewServer(NewRouter())
	defer server.Close()

	status := webUIWSDialStatus(t, server.URL, "Bearer wrong")
	if status != http.StatusForbidden {
		t.Fatalf("invalid token status = %d", status)
	}
}

func assertWebUIWSError(t *testing.T, payload map[string]any, message, code string) {
	t.Helper()
	if payload["type"] != "error" || payload["message"] != message || payload["code"] != code {
		t.Fatalf("error payload = %#v", payload)
	}
}

type webUIWSTestClient struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
}

func newWebUIWSTestClient(t *testing.T, authorization string) *webUIWSTestClient {
	t.Helper()
	server := httptest.NewServer(NewRouter())
	t.Cleanup(server.Close)
	target := "/webui/api/imagine/ws"
	if authorization == "" {
		target += "?access_token=web"
	}
	conn, reader, status := webUIWSDial(t, server.URL, target, authorization)
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("websocket status = %d", status)
	}
	return &webUIWSTestClient{t: t, conn: conn, reader: reader}
}

func webUIWSDialStatus(t *testing.T, serverURL, authorization string) int {
	t.Helper()
	conn, _, status := webUIWSDial(t, serverURL, "/webui/api/imagine/ws", authorization)
	if conn != nil {
		_ = conn.Close()
	}
	return status
}

func webUIWSDial(t *testing.T, serverURL, target, authorization string) (net.Conn, *bufio.Reader, int) {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	key := webUIWSKey(t)
	lines := []string{
		"GET " + target + " HTTP/1.1",
		"Host: " + parsed.Host,
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Key: " + key,
		"Sec-WebSocket-Version: 13",
	}
	if authorization != "" {
		lines = append(lines, "Authorization: "+authorization)
	}
	_, _ = fmt.Fprintf(conn, "%s\r\n\r\n", strings.Join(lines, "\r\n"))
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	return conn, reader, resp.StatusCode
}

func webUIWSKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func (c *webUIWSTestClient) SendJSON(payload map[string]any) {
	c.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		c.t.Fatal(err)
	}
	c.SendText(string(raw))
}

func (c *webUIWSTestClient) SendText(text string) {
	c.t.Helper()
	if err := writeWebUIWSClientText(c.conn, []byte(text)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *webUIWSTestClient) ReadJSON() map[string]any {
	c.t.Helper()
	raw, err := readWebUIWSServerText(c.reader)
	if err != nil {
		c.t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		c.t.Fatalf("decode websocket JSON %q: %v", raw, err)
	}
	return payload
}

func (c *webUIWSTestClient) Close() {
	_ = c.conn.Close()
}

func writeWebUIWSClientText(conn net.Conn, payload []byte) error {
	header := []byte{0x81, byte(0x80 | len(payload))}
	if len(payload) >= 126 {
		header = []byte{0x81, 0x80 | 126, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(len(payload)))
	}
	mask := []byte{1, 2, 3, 4}
	frame := append(append(header, mask...), payload...)
	for i := range payload {
		frame[len(header)+4+i] ^= mask[i%4]
	}
	_, err := conn.Write(frame)
	return err
}

func readWebUIWSServerText(reader *bufio.Reader) ([]byte, error) {
	for {
		opcode, payload, err := readWebUIWSFrame(reader)
		if err != nil {
			return nil, err
		}
		if opcode == 1 {
			return payload, nil
		}
		if opcode == 8 {
			return nil, io.EOF
		}
	}
}

func readWebUIWSFrame(reader *bufio.Reader) (byte, []byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length := int(second & 0x7f)
	if length == 126 {
		raw := make([]byte, 2)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(raw))
	}
	payload := make([]byte, length)
	_, err = io.ReadFull(reader, payload)
	return first & 0x0f, payload, err
}
