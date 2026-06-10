package openai

import (
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/platform"
)

func handleImageEdits(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid multipart body", "body", ""))
		return
	}
	modelName := r.FormValue("model")
	spec, ok := model.Get(modelName)
	if !ok || !spec.Enabled || !spec.IsImageEdit() {
		writeRouterError(w, platform.NewValidationError("Model '"+modelName+"' is not an image-edit model", "model", ""))
		return
	}
	if filesForField(r, "mask") != nil {
		writeRouterError(w, platform.NewValidationError("mask is not supported yet", "mask", ""))
		return
	}
	n := routerFormInt(r, "n", 1)
	if err := validateImageEditN(n, "n"); err != nil {
		writeRouterError(w, err)
		return
	}
	uploads := filesForField(r, "image[]")
	if len(uploads) == 0 {
		uploads = filesForField(r, "image")
	}
	if len(uploads) == 0 {
		writeRouterError(w, platform.NewValidationError("Uploaded image cannot be empty", "image", ""))
		return
	}
	content, err := imageEditMultipartContent(r.FormValue("prompt"), uploads)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	result, err := routerEditImages(r.Context(), imageEditOptions{
		Model:          modelName,
		Messages:       []map[string]any{{"role": "user", "content": content}},
		N:              n,
		Size:           routerStringDefault(r.FormValue("size"), "1024x1024"),
		ResponseFormat: routerStringDefault(r.FormValue("response_format"), "url"),
		Stream:         false,
		ChatFormat:     false,
	})
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeImageResult(w, result)
}

func imageEditMultipartContent(prompt string, uploads []*multipart.FileHeader) ([]any, error) {
	content := []any{map[string]any{"type": "text", "text": prompt}}
	for index, upload := range uploads {
		dataURI, err := uploadFileToDataURI(upload, "image."+strconv.Itoa(index))
		if err != nil {
			return nil, err
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": dataURI},
		})
	}
	return content, nil
}

func handleVideosCreate(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeRouterError(w, platform.NewValidationError("Invalid multipart body", "body", ""))
			return
		}
	} else if err := r.ParseForm(); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid form body", "body", ""))
		return
	}
	inputReferences := []map[string]any(nil)
	for _, upload := range filesForField(r, "input_reference[]") {
		dataURI, err := uploadFileToDataURI(upload, "input_reference")
		if err != nil {
			writeRouterError(w, err)
			return
		}
		inputReferences = append(inputReferences, map[string]any{"image_url": dataURI})
		if len(inputReferences) >= 7 {
			break
		}
	}
	result, err := CreateVideo(r.Context(), VideoCreateOptions{
		Model:           routerStringDefault(r.FormValue("model"), "grok-video"),
		Prompt:          r.FormValue("prompt"),
		Seconds:         routerFormInt(r, "seconds", 6),
		Size:            routerStringDefault(r.FormValue("size"), "720x1280"),
		ResolutionName:  r.FormValue("resolution_name"),
		Preset:          r.FormValue("preset"),
		InputReferences: inputReferences,
	})
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeRouterJSON(w, http.StatusOK, result)
}

func handleVideosRead(w http.ResponseWriter, r *http.Request) {
	videoID := strings.TrimPrefix(r.URL.Path, "/v1/videos/")
	if strings.HasSuffix(videoID, "/content") {
		videoID = strings.TrimSuffix(videoID, "/content")
		path, err := VideoContentPath(videoID)
		if err != nil {
			writeRouterError(w, err)
			return
		}
		if !serveRouterFile(w, path, "video/mp4") {
			writeRouterError(w, platform.NewValidationError("Video content for '"+videoID+"' not found", "video_id", ""))
		}
		return
	}
	result, err := RetrieveVideo(videoID)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeRouterJSON(w, http.StatusOK, result)
}

func routerFormInt(r *http.Request, key string, defaultValue int) int {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func filesForField(r *http.Request, key string) []*multipart.FileHeader {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil
	}
	return r.MultipartForm.File[key]
}
