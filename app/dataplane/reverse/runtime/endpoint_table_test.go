package runtime

import "testing"

func TestEndpointTableMatchesPythonConstants(t *testing.T) {
	tests := map[string]string{
		"Base":             Base,
		"AssetsCDN":        AssetsCDN,
		"ConsoleBase":      ConsoleBase,
		"Chat":             Chat,
		"AssetsUpload":     AssetsUpload,
		"AssetsList":       AssetsList,
		"AssetsDelete":     AssetsDelete,
		"AssetsDownload":   AssetsDownload,
		"RateLimits":       RateLimits,
		"AcceptTOS":        AcceptTOS,
		"NSFWMgmt":         NSFWMgmt,
		"SetBirth":         SetBirth,
		"MediaPost":        MediaPost,
		"MediaPostLink":    MediaPostLink,
		"VideoUpscale":     VideoUpscale,
		"WSImagine":        WSImagine,
		"WSLiveKit":        WSLiveKit,
		"LiveKitTokens":    LiveKitTokens,
		"ConsoleResponses": ConsoleResponses,
		"ConsoleChat":      ConsoleChat,
	}

	want := map[string]string{
		"Base":             "https://grok.com",
		"AssetsCDN":        "https://assets.grok.com",
		"ConsoleBase":      "https://console.x.ai",
		"Chat":             "https://grok.com/rest/app-chat/conversations/new",
		"AssetsUpload":     "https://grok.com/rest/app-chat/upload-file",
		"AssetsList":       "https://grok.com/rest/assets",
		"AssetsDelete":     "https://grok.com/rest/assets-metadata",
		"AssetsDownload":   "https://assets.grok.com",
		"RateLimits":       "https://grok.com/rest/rate-limits",
		"AcceptTOS":        "https://accounts.x.ai/auth_mgmt.AuthManagement/SetTosAcceptedVersion",
		"NSFWMgmt":         "https://grok.com/auth_mgmt.AuthManagement/UpdateUserFeatureControls",
		"SetBirth":         "https://grok.com/rest/auth/set-birth-date",
		"MediaPost":        "https://grok.com/rest/media/post/create",
		"MediaPostLink":    "https://grok.com/rest/media/post/create-link",
		"VideoUpscale":     "https://grok.com/rest/media/video/upscale",
		"WSImagine":        "wss://grok.com/ws/imagine/listen",
		"WSLiveKit":        "wss://livekit.grok.com",
		"LiveKitTokens":    "https://grok.com/rest/livekit/tokens",
		"ConsoleResponses": "https://console.x.ai/v1/responses",
		"ConsoleChat":      "https://console.x.ai/v1/chat/completions",
	}

	for name, expected := range want {
		if tests[name] != expected {
			t.Fatalf("%s = %q, want %q", name, tests[name], expected)
		}
	}
}
