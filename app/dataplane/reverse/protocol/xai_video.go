package protocol

const (
	MediaPostURL    = "https://grok.com/rest/media/post/create"
	MediaLinkURL    = "https://grok.com/rest/media/post/create-link"
	VideoUpscaleURL = "https://grok.com/rest/media/video/upscale"
)

type MediaPostPayloadOptions struct {
	MediaType string
	MediaURL  string
	Prompt    string
}

func BuildMediaPostPayload(options MediaPostPayloadOptions) map[string]any {
	payload := map[string]any{"mediaType": options.MediaType}
	if options.MediaURL != "" {
		payload["mediaUrl"] = options.MediaURL
	}
	if options.Prompt != "" {
		payload["prompt"] = options.Prompt
	}
	return payload
}

func BuildVideoUpscalePayload(videoID string) map[string]any {
	return map[string]any{"videoId": videoID}
}

func BuildMediaLinkPayload(postID string) map[string]any {
	return map[string]any{
		"postId":   postID,
		"source":   "post-page",
		"platform": "web",
	}
}
