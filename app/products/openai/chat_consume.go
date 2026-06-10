package openai

import (
	"context"
	"strings"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

type consumeChatLinesOptions struct {
	Model      string
	ResponseID string
	EmitThink  bool
	IsStream   bool
	ToolNames  []string
}

func consumeChatLines(lines []string, options consumeChatLinesOptions) (chatCompletionState, []string, error) {
	adapter := protocol.NewStreamAdapter(protocol.StreamAdapterOptions{
		ThinkingSummary:   options.EmitThink,
		ShowSearchSources: true,
	})
	frames := []string{}
	ended := false
	toolCallsEmitted := false
	var sieve *ToolSieve
	if options.IsStream && len(options.ToolNames) > 0 {
		sieve = NewToolSieve(options.ToolNames)
	}

	for _, line := range lines {
		eventType, data := protocol.ClassifyLine(line)
		if eventType == "done" {
			ended = true
			break
		}
		if eventType != "data" || data == "" {
			continue
		}
		events, err := adapter.Feed(data)
		if err != nil {
			return chatCompletionState{}, nil, err
		}
		if !options.IsStream {
			continue
		}
		for _, event := range events {
			if toolCallsEmitted {
				continue
			}
			switch event.Kind {
			case "text":
				if event.Content != "" {
					content := event.Content
					if sieve != nil {
						safeText, calls := sieve.Feed(event.Content)
						if safeText != "" {
							frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
								ResponseID: options.ResponseID,
								Model:      options.Model,
								Content:    safeText,
							})))
						}
						if calls != nil {
							frames = appendToolCallFrames(frames, options.ResponseID, options.Model, calls)
							toolCallsEmitted = true
						}
						continue
					}
					frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
						ResponseID: options.ResponseID,
						Model:      options.Model,
						Content:    content,
					})))
				}
			case "thinking":
				if options.EmitThink && event.Content != "" {
					frames = append(frames, formatChatDataFrame(MakeThinkingChunk(ThinkingChunkParams{
						ResponseID: options.ResponseID,
						Model:      options.Model,
						Content:    event.Content,
					})))
				}
			}
		}
	}

	if options.IsStream && !toolCallsEmitted && sieve != nil {
		if calls := sieve.Flush(); calls != nil {
			frames = appendToolCallFrames(frames, options.ResponseID, options.Model, calls)
			toolCallsEmitted = true
		}
	}

	state := chatCompletionState{
		Text:          strings.Join(adapter.TextBuf, ""),
		Thinking:      strings.Join(adapter.ThinkingBuf, ""),
		References:    adapter.ReferencesSuffix(),
		Annotations:   adapter.AnnotationsList(),
		SearchSources: adapter.SearchSourcesList(),
	}
	for _, image := range adapter.ImageURLs {
		resolved, err := resolveImage(context.Background(), "", image.URL, image.ImageID)
		if err == nil && resolved != "" {
			state.ImageTexts = append(state.ImageTexts, resolved)
		}
	}

	if options.IsStream && !toolCallsEmitted {
		final := MakeStreamChunk(StreamChunkParams{
			ResponseID:  options.ResponseID,
			Model:       options.Model,
			Content:     "",
			IsFinal:     true,
			Annotations: toChatAnnotations(state.Annotations),
		})
		if len(state.SearchSources) > 0 {
			final["search_sources"] = state.SearchSources
		}
		frames = append(frames, formatChatDataFrame(final), "data: [DONE]\n\n")
	}
	_ = ended
	return state, frames, nil
}

func appendToolCallFrames(frames []string, responseID string, modelName string, calls []protocol.ParsedToolCall) []string {
	for i, call := range calls {
		frames = append(frames, formatChatDataFrame(MakeToolCallChunk(ToolCallChunkParams{
			ResponseID: responseID,
			Model:      modelName,
			Index:      i,
			CallID:     call.CallID,
			Name:       call.Name,
			Arguments:  call.Arguments,
			IsFirst:    true,
		})))
	}
	frames = append(frames, formatChatDataFrame(MakeToolCallDoneChunk(ToolCallDoneChunkParams{
		ResponseID: responseID,
		Model:      modelName,
	})), "data: [DONE]\n\n")
	return frames
}

func formatChatDataFrame(payload any) string {
	data, err := marshalCompactJSON(payload)
	if err != nil {
		data = "null"
	}
	return "data: " + data + "\n\n"
}
