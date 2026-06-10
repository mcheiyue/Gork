package protocol

import (
	"reflect"
	"testing"
)

func TestImagineMessageBuildersMatchPythonShape(t *testing.T) {
	if WSImagineURL != "wss://grok.com/ws/imagine/listen" {
		t.Fatalf("WSImagineURL mismatch: %s", WSImagineURL)
	}
	reset := BuildImagineResetMessage(ImagineMessageOptions{TimestampMS: 1234})
	wantReset := map[string]any{
		"type":      "conversation.item.create",
		"timestamp": int64(1234),
		"item":      map[string]any{"type": "message", "content": []map[string]any{{"type": "reset"}}},
	}
	if !reflect.DeepEqual(wantReset, reset) {
		t.Fatalf("reset mismatch\nwant: %#v\n got: %#v", wantReset, reset)
	}

	request := BuildImagineRequestMessage("req-1", "draw a cat", ImagineMessageOptions{TimestampMS: 2345, AspectRatio: "16:9", EnableNSFW: boolRef(false), EnablePro: true})
	content := request["item"].(map[string]any)["content"].([]map[string]any)[0]
	if request["type"] != "conversation.item.create" || request["timestamp"] != int64(2345) ||
		content["requestId"] != "req-1" || content["text"] != "draw a cat" || content["type"] != "input_text" {
		t.Fatalf("request basics mismatch: %#v", request)
	}
	props := content["properties"].(map[string]any)
	wantProps := map[string]any{
		"section_count":       0,
		"is_kids_mode":        false,
		"enable_nsfw":         false,
		"skip_upsampler":      false,
		"enable_side_by_side": true,
		"is_initial":          false,
		"aspect_ratio":        "16:9",
		"enable_pro":          true,
	}
	if !reflect.DeepEqual(wantProps, props) {
		t.Fatalf("properties mismatch\nwant: %#v\n got: %#v", wantProps, props)
	}

	request = BuildImagineRequestMessage("req-default", "prompt", ImagineMessageOptions{TimestampMS: 3456})
	content = request["item"].(map[string]any)["content"].([]map[string]any)[0]
	props = content["properties"].(map[string]any)
	defaultProps := map[string]any{
		"section_count":       0,
		"is_kids_mode":        false,
		"enable_nsfw":         true,
		"skip_upsampler":      false,
		"enable_side_by_side": true,
		"is_initial":          false,
		"aspect_ratio":        "2:3",
		"enable_pro":          false,
	}
	if request["timestamp"] != int64(3456) || !reflect.DeepEqual(defaultProps, props) {
		t.Fatalf("default request mismatch request=%#v props=%#v", request, props)
	}
}

func TestImagineFrameParsersMatchPythonBehavior(t *testing.T) {
	id, ext := ParseImagineImageURL("https://assets.grok.com/images/abc-def.PNG?x=1")
	if id != "abc-def" || ext != "png" {
		t.Fatalf("ParseImagineImageURL mismatch id=%q ext=%q", id, ext)
	}
	id, ext = ParseImagineImageURL("no-image")
	if id == "" || ext != "jpg" {
		t.Fatalf("fallback image url mismatch id=%q ext=%q", id, ext)
	}

	got := ParseImagineJSONFrame(map[string]any{
		"current_status": "completed",
		"job_id":         "job-1",
		"order":          float64(2),
		"width":          float64(1024),
		"height":         float64(768),
		"moderated":      true,
		"r_rated":        false,
	})
	want := map[string]any{
		"status":    "completed",
		"image_id":  "job-1",
		"order":     2,
		"width":     1024,
		"height":    768,
		"moderated": true,
		"r_rated":   false,
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("json frame mismatch\nwant: %#v\n got: %#v", want, got)
	}
	got = ParseImagineJSONFrame(map[string]any{
		"current_status": "start_stage",
		"image_id":       123,
		"order":          "3",
		"width":          "1024",
		"height":         "0",
		"moderated":      "false",
		"r_rated":        "yes",
	})
	want = map[string]any{
		"status":    "start_stage",
		"image_id":  "123",
		"order":     3,
		"width":     1024,
		"height":    0,
		"moderated": true,
		"r_rated":   true,
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("string-coerced json frame mismatch\nwant: %#v\n got: %#v", want, got)
	}
	if ParseImagineJSONFrame(map[string]any{"current_status": "other", "image_id": "x"}) != nil {
		t.Fatalf("unrecognized status should return nil")
	}
	if ParseImagineJSONFrame(map[string]any{"current_status": "completed"}) != nil {
		t.Fatalf("missing image id should return nil")
	}
}
