package protocol

import "strings"

// ParseSSEEventLines is a minimal parser for the event/data subset consumed by
// the current xAI/Grok adapters. It trims data payloads and ignores id/retry.
func ParseSSEEventLines(lines []string) (string, string, bool) {
	event := "message"
	dataLines := []string{}
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			if value := strings.TrimSpace(line[6:]); value != "" {
				event = value
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			return event, "", true
		}
		dataLines = append(dataLines, data)
	}
	return event, strings.Join(dataLines, "\n"), false
}
