package runtime

const (
	Base        = "https://grok.com"
	AssetsCDN   = "https://assets.grok.com"
	ConsoleBase = "https://console.x.ai"
)

const (
	Chat = Base + "/rest/app-chat/conversations/new"
)

const (
	AssetsUpload   = Base + "/rest/app-chat/upload-file"
	AssetsList     = Base + "/rest/assets"
	AssetsDelete   = Base + "/rest/assets-metadata"
	AssetsDownload = AssetsCDN
)

const (
	RateLimits = Base + "/rest/rate-limits"
)

const (
	AcceptTOS = "https://accounts.x.ai/auth_mgmt.AuthManagement/SetTosAcceptedVersion"
	NSFWMgmt  = Base + "/auth_mgmt.AuthManagement/UpdateUserFeatureControls"
	SetBirth  = Base + "/rest/auth/set-birth-date"
)

const (
	MediaPost     = Base + "/rest/media/post/create"
	MediaPostLink = Base + "/rest/media/post/create-link"
	VideoUpscale  = Base + "/rest/media/video/upscale"
)

const (
	WSImagine = "wss://grok.com/ws/imagine/listen"
	WSLiveKit = "wss://livekit.grok.com"
)

const (
	LiveKitTokens = Base + "/rest/livekit/tokens"
)

const (
	ConsoleResponses = ConsoleBase + "/v1/responses"
	ConsoleChat      = ConsoleBase + "/v1/chat/completions"
)
