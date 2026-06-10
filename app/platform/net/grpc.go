package net

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var grpcHTTPStatus = map[int]int{
	0:  200,
	4:  504,
	7:  403,
	8:  429,
	14: 503,
	16: 401,
}

type GRPCStatus struct {
	Code    int
	Message string
}

type GRPCClient struct{}

type GRPCResponse struct {
	Messages [][]byte
	Trailers map[string]string
}

func (s GRPCStatus) OK() bool {
	return s.Code == 0
}

func (s GRPCStatus) HTTPEquivalent() int {
	if httpStatus, ok := grpcHTTPStatus[s.Code]; ok {
		return httpStatus
	}
	return 502
}

func GRPCStatusFromTrailers(trailers map[string]string) GRPCStatus {
	raw := strings.TrimSpace(trailers["grpc-status"])
	message := strings.TrimSpace(trailers["grpc-message"])
	code, err := strconv.Atoi(raw)
	if err != nil {
		code = -1
	}
	return GRPCStatus{Code: code, Message: message}
}

func (GRPCClient) EncodePayload(data []byte) []byte {
	frame := make([]byte, 5+len(data))
	frame[0] = 0x00
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(data)))
	copy(frame[5:], data)
	return frame
}

func (client GRPCClient) ParseResponse(body []byte, contentType string, headers map[string]string) (GRPCResponse, error) {
	decoded, err := maybeDecodeBase64(body, contentType)
	if err != nil {
		return GRPCResponse{}, err
	}

	response := GRPCResponse{Messages: [][]byte{}, Trailers: map[string]string{}}
	for offset := 0; offset < len(decoded); {
		if len(decoded)-offset < 5 {
			break
		}
		flag := decoded[offset]
		length := int(binary.BigEndian.Uint32(decoded[offset+1 : offset+5]))
		offset += 5
		if len(decoded)-offset < length {
			break
		}
		payload := decoded[offset : offset+length]
		offset += length

		if flag&0x80 != 0 {
			for key, value := range parseGRPCTrailers(payload) {
				response.Trailers[key] = value
			}
			continue
		}
		if flag&0x01 != 0 {
			return response, errors.New("grpc-web compressed frame is not supported")
		}
		response.Messages = append(response.Messages, append([]byte(nil), payload...))
	}

	mergeHeaderTrailers(response.Trailers, headers)
	return response, nil
}

func (client GRPCClient) GetStatus(trailers map[string]string) GRPCStatus {
	return GRPCStatusFromTrailers(trailers)
}

func maybeDecodeBase64(body []byte, contentType string) ([]byte, error) {
	if strings.Contains(strings.ToLower(contentType), "grpc-web-text") {
		return base64.StdEncoding.DecodeString(string(compactWhitespace(body)))
	}
	head := body
	if len(head) > 2048 {
		head = head[:2048]
	}
	if len(head) > 0 && isBase64Head(head) {
		if decoded, err := base64.StdEncoding.DecodeString(string(compactWhitespace(body))); err == nil {
			return decoded, nil
		}
	}
	return body, nil
}

func compactWhitespace(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for _, ch := range data {
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			continue
		}
		out = append(out, ch)
	}
	return out
}

func isBase64Head(data []byte) bool {
	for _, ch := range data {
		if ('A' <= ch && ch <= 'Z') || ('a' <= ch && ch <= 'z') || ('0' <= ch && ch <= '9') {
			continue
		}
		if ch == '+' || ch == '/' || ch == '=' || ch == '\r' || ch == '\n' {
			continue
		}
		return false
	}
	return true
}

func parseGRPCTrailers(payload []byte) map[string]string {
	result := map[string]string{}
	text := strings.ReplaceAll(string(payload), "\r\n", "\n")
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "grpc-message" {
			value = unquoteGRPCMessage(value)
		}
		result[key] = value
	}
	return result
}

func mergeHeaderTrailers(trailers map[string]string, headers map[string]string) {
	for rawKey, rawValue := range headers {
		key := strings.ToLower(rawKey)
		if key != "grpc-status" && key != "grpc-message" {
			continue
		}
		if _, exists := trailers[key]; exists {
			continue
		}
		value := strings.TrimSpace(rawValue)
		if key == "grpc-message" {
			value = unquoteGRPCMessage(value)
		}
		trailers[key] = value
	}
}

func unquoteGRPCMessage(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}
