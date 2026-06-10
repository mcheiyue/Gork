package protocol

import (
	"reflect"
	"testing"
)

func TestVideoPayloadBuildersMatchPython(t *testing.T) {
	if MediaPostURL != "https://grok.com/rest/media/post/create" ||
		MediaLinkURL != "https://grok.com/rest/media/post/create-link" ||
		VideoUpscaleURL != "https://grok.com/rest/media/video/upscale" {
		t.Fatalf("video endpoint constants mismatch")
	}
	if got := BuildMediaPostPayload(MediaPostPayloadOptions{MediaType: "video"}); !reflect.DeepEqual(map[string]any{"mediaType": "video"}, got) {
		t.Fatalf("minimal media post payload mismatch: %#v", got)
	}
	if got := BuildMediaPostPayload(MediaPostPayloadOptions{MediaType: "video", MediaURL: "https://v"}); !reflect.DeepEqual(map[string]any{"mediaType": "video", "mediaUrl": "https://v"}, got) {
		t.Fatalf("media-url-only media post payload mismatch: %#v", got)
	}
	if got := BuildMediaPostPayload(MediaPostPayloadOptions{MediaType: "video", Prompt: "make it cinematic"}); !reflect.DeepEqual(map[string]any{"mediaType": "video", "prompt": "make it cinematic"}, got) {
		t.Fatalf("prompt-only media post payload mismatch: %#v", got)
	}
	want := map[string]any{"mediaType": "video", "mediaUrl": "https://v", "prompt": "make it cinematic"}
	if got := BuildMediaPostPayload(MediaPostPayloadOptions{MediaType: "video", MediaURL: "https://v", Prompt: "make it cinematic"}); !reflect.DeepEqual(want, got) {
		t.Fatalf("full media post payload mismatch\nwant: %#v\n got: %#v", want, got)
	}
	if got := BuildVideoUpscalePayload("vid-1"); !reflect.DeepEqual(map[string]any{"videoId": "vid-1"}, got) {
		t.Fatalf("upscale payload mismatch: %#v", got)
	}
	if got := BuildVideoUpscalePayload(""); !reflect.DeepEqual(map[string]any{"videoId": ""}, got) {
		t.Fatalf("empty upscale payload mismatch: %#v", got)
	}
	want = map[string]any{"postId": "post-1", "source": "post-page", "platform": "web"}
	if got := BuildMediaLinkPayload("post-1"); !reflect.DeepEqual(want, got) {
		t.Fatalf("media link payload mismatch\nwant: %#v\n got: %#v", want, got)
	}
	want = map[string]any{"postId": "", "source": "post-page", "platform": "web"}
	if got := BuildMediaLinkPayload(""); !reflect.DeepEqual(want, got) {
		t.Fatalf("empty media link payload mismatch\nwant: %#v\n got: %#v", want, got)
	}
}
