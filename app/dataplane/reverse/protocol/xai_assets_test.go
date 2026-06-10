package protocol

import "testing"

func TestXAIAssetConstantsAndDeleteURLMatchPython(t *testing.T) {
	if AssetsListURL != "https://grok.com/rest/assets" {
		t.Fatalf("AssetsListURL = %q", AssetsListURL)
	}
	if AssetsDeleteURL != "https://grok.com/rest/assets-metadata" {
		t.Fatalf("AssetsDeleteURL = %q", AssetsDeleteURL)
	}
	if AssetsDownloadBase != "https://assets.grok.com" {
		t.Fatalf("AssetsDownloadBase = %q", AssetsDownloadBase)
	}
	if AppChatUploadURL != "https://grok.com/rest/app-chat/upload-file" {
		t.Fatalf("AppChatUploadURL = %q", AppChatUploadURL)
	}
	if got := AssetDeleteURL("asset-1"); got != "https://grok.com/rest/assets-metadata/asset-1" {
		t.Fatalf("AssetDeleteURL() = %q", got)
	}
}

func TestResolveDownloadURLMatchesPython(t *testing.T) {
	tests := []struct {
		input   string
		url     string
		origin  string
		referer string
	}{
		{
			input:   "https://assets.grok.com/foo/bar.png",
			url:     "https://assets.grok.com/foo/bar.png",
			origin:  "https://assets.grok.com",
			referer: "https://assets.grok.com/",
		},
		{
			input:   "https://cdn.example.com/foo/bar.png",
			url:     "https://cdn.example.com/foo/bar.png",
			origin:  "https://cdn.example.com",
			referer: "https://cdn.example.com/",
		},
		{
			input:   "/foo/bar.png",
			url:     "https://assets.grok.com/foo/bar.png",
			origin:  "https://assets.grok.com",
			referer: "https://assets.grok.com/",
		},
		{
			input:   "foo/bar.png",
			url:     "https://assets.grok.com/foo/bar.png",
			origin:  "https://assets.grok.com",
			referer: "https://assets.grok.com/",
		},
		{
			input:   "",
			url:     "https://assets.grok.com/",
			origin:  "https://assets.grok.com",
			referer: "https://assets.grok.com/",
		},
	}

	for _, tt := range tests {
		url, origin, referer := ResolveDownloadURL(tt.input)
		if url != tt.url || origin != tt.origin || referer != tt.referer {
			t.Fatalf("ResolveDownloadURL(%q) = %q/%q/%q", tt.input, url, origin, referer)
		}
	}
}

func TestInferContentTypeMatchesPythonExtensionMap(t *testing.T) {
	tests := map[string]*string{
		"https://assets.grok.com/a.JPG":     strPtr("image/jpeg"),
		"https://assets.grok.com/a.jpeg":    strPtr("image/jpeg"),
		"https://assets.grok.com/a.png?x=1": strPtr("image/png"),
		"https://assets.grok.com/a.webp":    strPtr("image/webp"),
		"https://assets.grok.com/a.mp4":     strPtr("video/mp4"),
		"https://assets.grok.com/a.webm":    strPtr("video/webm"),
		"https://assets.grok.com/a.unknown": nil,
		"https://assets.grok.com/no_suffix": nil,
	}

	for url, want := range tests {
		got := InferContentType(url)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Fatalf("InferContentType(%q) = %#v, want %#v", url, got, want)
		}
	}
}

func TestResolveAssetReferenceMatchesPython(t *testing.T) {
	if got := ResolveAssetReference("file-id", "path/image.png", "user-1"); got == nil || *got != "https://assets.grok.com/path/image.png" {
		t.Fatalf("file uri reference = %#v", got)
	}
	if got := ResolveAssetReference("file-id", "", "user-1"); got == nil || *got != "https://assets.grok.com/users/user-1/file-id/content" {
		t.Fatalf("file id reference = %#v", got)
	}
	if got := ResolveAssetReference("file-id", "", ""); got != nil {
		t.Fatalf("missing user reference = %#v, want nil", got)
	}
	if got := ResolveAssetReference("", "", "user-1"); got != nil {
		t.Fatalf("missing file id reference = %#v, want nil", got)
	}
}

func strPtr(value string) *string {
	return &value
}
