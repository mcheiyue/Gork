package protocol

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	platform "github.com/jiujiu532/grok2api/app/platform"
)

var grokRenderRe = regexp.MustCompile(`(?s)<grok:render\s+card_id="([^"]+)"\s+card_type="([^"]+)"\s+type="([^"]+)"[^>]*>.*?</grok:render>`)

var toolFormat = map[string]struct {
	emoji string
	keys  []string
}{
	"web_search":        {"🔍", []string{"query", "q"}},
	"x_search":          {"🔍", []string{"query"}},
	"x_keyword_search":  {"🔍", []string{"query"}},
	"x_semantic_search": {"🔍", []string{"query"}},
	"browse_page":       {"🌐", []string{"url"}},
	"search_images":     {"🖼️", []string{"image_description", "imageDescription"}},
	"image_search":      {"🖼️", []string{"image_description", "imageDescription"}},
	"chatroom_send":     {"📋", []string{"message"}},
	"code_execution":    {"💻", nil},
}

func (a *StreamAdapter) handleCard(cardRaw map[string]any) []FrameEvent {
	jsonData := stringFromAny(cardRaw["jsonData"])
	if jsonData == "" {
		return nil
	}
	var card map[string]any
	if err := json.Unmarshal([]byte(jsonData), &card); err != nil {
		return nil
	}
	cardID := stringFromAny(card["id"])
	a.cardCache[cardID] = card
	chunk, ok := card["image_chunk"].(map[string]any)
	if !ok || len(chunk) == 0 {
		return nil
	}
	events := []FrameEvent{}
	progress, hasProgress := numberAsInt(chunk["progress"])
	uuid := stringFromAny(chunk["imageUuid"])
	if hasProgress {
		events = append(events, FrameEvent{Kind: "image_progress", Content: fmt.Sprint(progress), ImageID: uuid})
	}
	if progress == 100 && !boolFromAny(chunk["moderated"]) {
		url := imageBaseURL + stringFromAny(chunk["imageUrl"])
		a.ImageURLs = append(a.ImageURLs, imageURLRef{URL: url, ImageID: uuid})
		events = append(events, FrameEvent{Kind: "image", Content: url, ImageID: uuid})
	}
	return events
}

func (a *StreamAdapter) cleanToken(token string) (string, []map[string]any) {
	if !strings.Contains(token, "<grok:render") {
		return token, nil
	}
	cleaned := grokRenderRe.ReplaceAllStringFunc(token, func(match string) string {
		parts := grokRenderRe.FindStringSubmatch(match)
		if len(parts) < 4 {
			return ""
		}
		return a.renderReplace(parts[1], parts[3])
	})
	if strings.HasPrefix(cleaned, "\n") && strings.Contains(cleaned, "[[") {
		cleaned = strings.TrimLeft(cleaned, "\n")
	}
	localAnnotations := []map[string]any{}
	if len(a.pendingCitations) > 0 {
		searchStart := 0
		for _, cite := range a.pendingCitations {
			relative := strings.Index(cleaned[searchStart:], cite.Needle)
			if relative >= 0 {
				pos := searchStart + relative
				localAnnotations = append(localAnnotations, map[string]any{
					"type":        "url_citation",
					"url":         cite.URL,
					"title":       cite.Title,
					"local_start": pos,
					"local_end":   pos + len(cite.Needle),
				})
				searchStart = pos + len(cite.Needle)
			}
		}
		a.pendingCitations = nil
	}
	return cleaned, localAnnotations
}

func (a *StreamAdapter) renderReplace(cardID, renderType string) string {
	card := a.cardCache[cardID]
	if card == nil {
		return ""
	}
	switch renderType {
	case "render_searched_image":
		img, _ := card["image"].(map[string]any)
		title := stringFromAny(img["title"])
		if title == "" {
			title = "image"
		}
		thumb := stringFromAny(img["thumbnail"])
		if thumb == "" {
			thumb = stringFromAny(img["original"])
		}
		link := stringFromAny(img["link"])
		if link != "" {
			return fmt.Sprintf("[![%s](%s)](%s)", title, thumb, link)
		}
		return fmt.Sprintf("![%s](%s)", title, thumb)
	case "render_generated_image":
		return ""
	case "render_inline_citation":
		return a.renderInlineCitation(card)
	default:
		return ""
	}
}

func (a *StreamAdapter) renderInlineCitation(card map[string]any) string {
	url := stringFromAny(card["url"])
	if url == "" {
		return ""
	}
	index, ok := a.citationMap[url]
	if !ok {
		a.citationOrder = append(a.citationOrder, url)
		index = len(a.citationOrder)
		a.citationMap[url] = index
	}
	if index == a.lastCitationIndex {
		return ""
	}
	a.lastCitationIndex = index
	citationText := fmt.Sprintf(" [[%d]](%s)", index, url)
	title := stringFromAny(card["title"])
	if title == "" {
		for _, item := range a.webSearchResults {
			if stringFromAny(item["url"]) == url {
				title = stringFromAny(item["title"])
				break
			}
		}
	}
	if title == "" {
		title = url
	}
	a.pendingCitations = append(a.pendingCitations, pendingCitation{URL: url, Title: title, Needle: citationText})
	return citationText
}

func (a *StreamAdapter) collectSearchResults(resp map[string]any) {
	if wsr, ok := resp["webSearchResults"].(map[string]any); ok {
		for _, raw := range anySlice(wsr["results"]) {
			item, ok := raw.(map[string]any)
			if !ok || stringFromAny(item["url"]) == "" {
				continue
			}
			url := stringFromAny(item["url"])
			if _, seen := a.webSearchURLsSeen[url]; seen {
				continue
			}
			a.webSearchURLsSeen[url] = struct{}{}
			copied := cloneAnyMap(item)
			copied["type"] = "web"
			a.webSearchResults = append(a.webSearchResults, copied)
		}
	}
	if xsr, ok := resp["xSearchResults"].(map[string]any); ok {
		for _, raw := range anySlice(xsr["results"]) {
			item, ok := raw.(map[string]any)
			if !ok || stringFromAny(item["postId"]) == "" || stringFromAny(item["username"]) == "" {
				continue
			}
			username := stringFromAny(item["username"])
			url := fmt.Sprintf("https://x.com/%s/status/%s", username, stringFromAny(item["postId"]))
			if _, seen := a.webSearchURLsSeen[url]; seen {
				continue
			}
			a.webSearchURLsSeen[url] = struct{}{}
			rawText := whitespaceRe.ReplaceAllString(strings.TrimSpace(stringFromAny(item["text"])), " ")
			title := "𝕏/@" + username
			if rawText != "" {
				if len([]rune(rawText)) > 50 {
					rawText = string([]rune(rawText)[:50]) + "..."
				}
				title += ": " + rawText
			}
			a.webSearchResults = append(a.webSearchResults, map[string]any{"url": url, "title": title, "type": "x_post"})
		}
	}
}

func raiseForStreamErrorObject(obj map[string]any) error {
	if err := StreamErrorFromPayload(obj); err != nil {
		return err
	}
	return nil
}

func RaiseForStreamError(data string) error {
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return nil
	}
	return raiseForStreamErrorObject(obj)
}

func upstreamFromError(err error) *platform.UpstreamError {
	if upstream, ok := err.(*platform.UpstreamError); ok {
		return upstream
	}
	return nil
}
