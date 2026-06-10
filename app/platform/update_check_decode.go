package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func decodeGithubReleases(reader io.Reader) ([]githubRelease, error) {
	var rawItems []json.RawMessage
	if err := json.NewDecoder(reader).Decode(&rawItems); err != nil {
		return nil, errors.New("GitHub releases response invalid")
	}
	releases := make([]githubRelease, 0, len(rawItems))
	for _, raw := range rawItems {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil || item == nil {
			continue
		}
		releases = append(releases, githubRelease{
			TagName:     truthyString(item["tag_name"]),
			Name:        truthyString(item["name"]),
			HTMLURL:     truthyString(item["html_url"]),
			PublishedAt: truthyString(item["published_at"]),
			Body:        truthyString(item["body"]),
			Draft:       truthy(item["draft"]),
		})
	}
	return releases, nil
}

func truthyString(value any) string {
	if !truthy(value) {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
