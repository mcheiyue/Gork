package protocol

import (
	"net/url"
	"path"
	"strings"
)

const (
	AssetsListURL      = "https://grok.com/rest/assets"
	AssetsDeleteURL    = "https://grok.com/rest/assets-metadata"
	AssetsDownloadBase = "https://assets.grok.com"
	AppChatUploadURL   = "https://grok.com/rest/app-chat/upload-file"
)

var extensionMIME = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".mp4":  "video/mp4",
	".webm": "video/webm",
}

func AssetDeleteURL(assetID string) string {
	return AssetsDeleteURL + "/" + assetID
}

func ResolveDownloadURL(filePath string) (string, string, string) {
	parsed, err := url.Parse(filePath)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin := parsed.Scheme + "://" + parsed.Host
		return filePath, origin, origin + "/"
	}
	resolvedPath := filePath
	if !strings.HasPrefix(resolvedPath, "/") {
		resolvedPath = "/" + resolvedPath
	}
	assetURL := AssetsDownloadBase + resolvedPath
	return assetURL, AssetsDownloadBase, AssetsDownloadBase + "/"
}

func InferContentType(rawURL string) *string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	if mimeType, ok := extensionMIME[strings.ToLower(path.Ext(parsed.Path))]; ok {
		return &mimeType
	}
	return nil
}

func ResolveAssetReference(fileID, fileURI, userID string) *string {
	if fileURI != "" {
		assetURL, _, _ := ResolveDownloadURL(fileURI)
		return &assetURL
	}
	if fileID != "" && userID != "" {
		assetURL := AssetsDownloadBase + "/users/" + userID + "/" + fileID + "/content"
		return &assetURL
	}
	return nil
}
