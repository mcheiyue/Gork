package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const WSImagineURL = "wss://grok.com/ws/imagine/listen"

var imagineURLPattern = regexp.MustCompile(`(?i)/images/([a-f0-9\-]+)\.(png|jpg|jpeg)`)

type ImagineMessageOptions struct {
	TimestampMS int64
	AspectRatio string
	EnableNSFW  *bool
	EnablePro   bool
}

func BuildImagineResetMessage(options ImagineMessageOptions) map[string]any {
	return map[string]any{
		"type":      "conversation.item.create",
		"timestamp": imagineTimestamp(options.TimestampMS),
		"item":      map[string]any{"type": "message", "content": []map[string]any{{"type": "reset"}}},
	}
}

func BuildImagineRequestMessage(requestID, prompt string, options ImagineMessageOptions) map[string]any {
	aspectRatio := options.AspectRatio
	if aspectRatio == "" {
		aspectRatio = "2:3"
	}
	enableNSFW := true
	if options.EnableNSFW != nil {
		enableNSFW = *options.EnableNSFW
	}
	return map[string]any{
		"type":      "conversation.item.create",
		"timestamp": imagineTimestamp(options.TimestampMS),
		"item": map[string]any{
			"type": "message",
			"content": []map[string]any{{
				"requestId": requestID,
				"text":      prompt,
				"type":      "input_text",
				"properties": map[string]any{
					"section_count":       0,
					"is_kids_mode":        false,
					"enable_nsfw":         enableNSFW,
					"skip_upsampler":      false,
					"enable_side_by_side": true,
					"is_initial":          false,
					"aspect_ratio":        aspectRatio,
					"enable_pro":          options.EnablePro,
				},
			}},
		},
	}
}

func ParseImagineImageURL(url string) (string, string) {
	match := imagineURLPattern.FindStringSubmatch(url)
	if len(match) == 3 {
		return match[1], strings.ToLower(match[2])
	}
	return randomHexID(), "jpg"
}

func ParseImagineJSONFrame(msg map[string]any) map[string]any {
	status := stringFromAny(msg["current_status"])
	if status != "start_stage" && status != "completed" {
		return nil
	}
	imageID := stringFromAny(msg["image_id"])
	if imageID == "" {
		imageID = stringFromAny(msg["job_id"])
	}
	if imageID == "" {
		return nil
	}
	order := imagineNumberAsInt(msg["order"])
	width := imagineNumberAsInt(msg["width"])
	height := imagineNumberAsInt(msg["height"])
	return map[string]any{
		"status":    status,
		"image_id":  imageID,
		"order":     order,
		"width":     width,
		"height":    height,
		"moderated": truthyAny(msg["moderated"]),
		"r_rated":   truthyAny(msg["r_rated"]),
	}
}

func imagineNumberAsInt(value any) int {
	if i, ok := numberAsInt(value); ok {
		return i
	}
	if s, ok := value.(string); ok {
		if i, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return i
		}
	}
	return 0
}

func imagineTimestamp(value int64) int64 {
	if value != 0 {
		return value
	}
	return time.Now().UnixMilli()
}

func randomHexID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}
	return hex.EncodeToString(data[:])
}
