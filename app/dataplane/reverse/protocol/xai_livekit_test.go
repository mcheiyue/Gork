package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestLiveKitTokenPayloadMatchesPythonShape(t *testing.T) {
	if LiveKitTokenURL != "https://grok.com/rest/livekit/tokens" || LiveKitWSBase != "wss://livekit.grok.com" {
		t.Fatalf("LiveKit constants mismatch")
	}
	body := BuildLiveKitTokenRequestPayload(LiveKitTokenOptions{})
	outer := decodeJSONMap(t, body)
	session := decodeJSONStringMap(t, outer["sessionPayload"].(string))
	wantSession := map[string]any{
		"voice":          "ara",
		"personality":    "assistant",
		"playback_speed": float64(1),
		"enable_vision":  false,
		"turn_detection": map[string]any{"type": "server_vad"},
	}
	if !reflect.DeepEqual(wantSession, session) {
		t.Fatalf("default session mismatch\nwant: %#v\n got: %#v", wantSession, session)
	}
	if outer["requestAgentDispatch"] != false || outer["livekitUrl"] != "wss://livekit.grok.com" ||
		!reflect.DeepEqual(map[string]any{"enable_markdown_transcript": "true"}, outer["params"]) {
		t.Fatalf("outer payload mismatch: %#v", outer)
	}

	body = BuildLiveKitTokenRequestPayload(LiveKitTokenOptions{Speed: 1.25, CustomInstruction: "raw"})
	session = decodeJSONStringMap(t, decodeJSONMap(t, body)["sessionPayload"].(string))
	if session["personality"] != nil || session["instructions"] != "raw" ||
		session["is_raw_instructions"] != true || session["playback_speed"] != 1.25 {
		t.Fatalf("custom instruction session mismatch: %#v", session)
	}

	body = BuildLiveKitTokenRequestPayload(LiveKitTokenOptions{VoiceSet: true, PersonalitySet: true, SpeedSet: true})
	session = decodeJSONStringMap(t, decodeJSONMap(t, body)["sessionPayload"].(string))
	wantExplicitFalsey := map[string]any{
		"voice":          "",
		"personality":    "",
		"playback_speed": float64(0),
		"enable_vision":  false,
		"turn_detection": map[string]any{"type": "server_vad"},
	}
	if !reflect.DeepEqual(wantExplicitFalsey, session) {
		t.Fatalf("explicit falsey session mismatch\nwant: %#v\n got: %#v", wantExplicitFalsey, session)
	}

	body = BuildLiveKitTokenRequestPayload(LiveKitTokenOptions{VoiceSet: true, SpeedSet: true, CustomInstruction: "raw"})
	session = decodeJSONStringMap(t, decodeJSONMap(t, body)["sessionPayload"].(string))
	if session["voice"] != "" || session["personality"] != nil || session["playback_speed"] != float64(0) ||
		session["instructions"] != "raw" || session["is_raw_instructions"] != true {
		t.Fatalf("custom instruction falsey session mismatch: %#v", session)
	}
}

func TestLiveKitWSURLMatchesPythonQuery(t *testing.T) {
	got := BuildLiveKitWSURL("tok space")
	want := "wss://livekit.grok.com/rtc?auto_subscribe=1&sdk=js&version=2.11.4&protocol=15&access_token=tok+space"
	if got != want {
		t.Fatalf("url mismatch\nwant: %s\n got: %s", want, got)
	}

	got = BuildLiveKitWSURL("tok /+&=%")
	want = "wss://livekit.grok.com/rtc?auto_subscribe=1&sdk=js&version=2.11.4&protocol=15&access_token=tok+%2F%2B%26%3D%25"
	if got != want {
		t.Fatalf("special url mismatch\nwant: %s\n got: %s", want, got)
	}
}

func decodeJSONMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return out
}

func decodeJSONStringMap(t *testing.T, text string) map[string]any {
	t.Helper()
	return decodeJSONMap(t, []byte(text))
}
