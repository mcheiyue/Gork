package webui

import (
	"net/http"
	"time"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/platform/auth"
	"github.com/jiujiu532/grok2api/app/platform/config"
	"github.com/jiujiu532/grok2api/app/products/openai"
)

var (
	webUIAuthSettings = func() auth.AuthSettings {
		return auth.AuthSettings{
			WebUIKey:     config.GetConfig("app.webui_key", ""),
			WebUIEnabled: config.GetConfig("app.webui_enabled", false),
		}
	}
	webUIListModels         = controlmodel.ListEnabled
	webUIUnixNow            = defaultWebUIUnixNow
	webUIChatCompletions    = defaultWebUIChatCompletions
	webUIImagineEvents      = defaultWebUIImagineEvents
	webUIImagineRunID       = defaultWebUIImagineRunID
	webUIImagineDefaultNSFW = defaultWebUIImagineDefaultNSFW
	webUIVoiceDirectory     = defaultWebUIVoiceDirectory
	webUIVoiceFetchToken    = defaultWebUIVoiceFetchToken
)

// NewRouter returns the WebUI API surface under /webui/api.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webui/api/models", webUIProtected(http.MethodGet, handleWebUIModels))
	mux.HandleFunc("/webui/api/chat/completions", webUIProtected(http.MethodPost, handleWebUIChatCompletions))
	mux.HandleFunc("/webui/api/imagine/ws", handleWebUIImagineWS)
	mux.HandleFunc("/webui/api/voice/token", webUIProtected(http.MethodPost, handleWebUIVoiceToken))
	return mux
}

func handleWebUIModels(w http.ResponseWriter, _ *http.Request) {
	models := make([]map[string]any, 0, len(webUIListModels()))
	created := webUIUnixNow()
	for _, spec := range webUIListModels() {
		models = append(models, webUIModelBody(spec, created))
	}
	writeWebUIJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}

func handleWebUIChatCompletions(w http.ResponseWriter, r *http.Request) {
	webUIChatCompletions(w, r)
}

func webUIModelBody(spec controlmodel.ModelSpec, created int64) map[string]any {
	return map[string]any{
		"id": spec.ModelName, "object": "model", "created": created,
		"owned_by": "xai", "name": spec.PublicName, "capability": webUICapabilityName(spec),
	}
}

func webUICapabilityName(spec controlmodel.ModelSpec) string {
	if spec.IsImageEdit() {
		return "image_edit"
	}
	if spec.IsImage() {
		return "image"
	}
	if spec.IsVideo() {
		return "video"
	}
	return "chat"
}

func defaultWebUIUnixNow() int64 {
	return time.Now().Unix()
}

func defaultWebUIChatCompletions(w http.ResponseWriter, r *http.Request) {
	openai.ServeChatCompletions(w, r)
}
