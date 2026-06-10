package openai

import (
	"context"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

type responseStreamResult struct {
	State     chatCompletionState
	ToolItems []map[string]any
}

func collectResponseStream(ctx context.Context, lines []string, options responseAttemptOptions) (responseStreamResult, []string, error) {
	adapter := protocol.NewStreamAdapter(protocol.StreamAdapterOptions{})
	frames := responseInitialFrames(options)
	var state chatCompletionState
	toolItems := []map[string]any{}
	messageStarted := false
	reasoningStarted := false
	reasoningClosed := false
	toolCallsEmitted := false
	var sieve *ToolSieve
	if len(options.ToolNames) > 0 {
		sieve = NewToolSieve(options.ToolNames)
	}
	for _, line := range lines {
		eventType, data := protocol.ClassifyLine(line)
		if eventType == "done" {
			break
		}
		if eventType != "data" || data == "" {
			continue
		}
		events, err := adapter.Feed(data)
		if err != nil {
			return responseStreamResult{}, nil, err
		}
		for _, event := range events {
			switch event.Kind {
			case "thinking":
				if options.Request.EmitThink && event.Content != "" {
					if !reasoningStarted && options.Request.Stream {
						reasoningStarted = true
						frames = append(frames, responseReasoningStartFrames(options.IDs)...)
					}
					state.Thinking += event.Content
					if options.Request.Stream {
						frames = append(frames, FormatSSE("response.reasoning_summary_text.delta", map[string]any{
							"type":          "response.reasoning_summary_text.delta",
							"item_id":       options.IDs.ReasoningID,
							"output_index":  0,
							"summary_index": 0,
							"delta":         event.Content,
						}))
					}
				}
			case "text":
				if reasoningStarted && !reasoningClosed && options.Request.Stream {
					reasoningClosed = true
					frames = append(frames, responseReasoningDoneFrames(options.IDs, state.Thinking)...)
				}
				text := event.Content
				if sieve != nil {
					safeText, calls := sieve.Feed(text)
					if calls != nil {
						toolItems = buildResponseFunctionCallItems(calls)
						frames = append(frames, emitResponseFunctionCallEvents(toolItems, responseMessageIndex(reasoningStarted))...)
						toolCallsEmitted = true
						continue
					}
					text = safeText
				}
				if text == "" {
					continue
				}
				if !messageStarted && options.Request.Stream {
					messageStarted = true
					frames = append(frames, responseMessageStartFrames(options.IDs, responseMessageIndex(reasoningStarted))...)
				}
				state.Text += text
				if options.Request.Stream {
					frames = append(frames, responseTextDeltaFrame(options.IDs.MessageID, responseMessageIndex(reasoningStarted), text))
				}
			case "annotation":
				if event.AnnotationData != nil {
					state.Annotations = append(state.Annotations, event.AnnotationData)
					if options.Request.Stream && messageStarted {
						frames = append(frames, FormatSSE("response.output_text.annotation.added", map[string]any{
							"type":             "response.output_text.annotation.added",
							"item_id":          options.IDs.MessageID,
							"output_index":     responseMessageIndex(reasoningStarted),
							"content_index":    0,
							"annotation_index": len(state.Annotations) - 1,
							"annotation":       event.AnnotationData,
						}))
					}
				}
			case "soft_stop":
				break
			}
		}
		if toolCallsEmitted {
			break
		}
	}
	if sieve != nil && !toolCallsEmitted {
		if calls := sieve.Flush(); calls != nil {
			toolItems = buildResponseFunctionCallItems(calls)
			frames = append(frames, emitResponseFunctionCallEvents(toolItems, responseMessageIndex(reasoningStarted))...)
			toolCallsEmitted = true
		}
	}
	for _, ref := range adapter.ImageURLs {
		text, err := resolveImage(ctx, options.Account.Token, ref.URL, ref.ImageID)
		if err != nil {
			continue
		}
		if options.Request.Stream && messageStarted && !toolCallsEmitted {
			frames = append(frames, responseTextDeltaFrame(options.IDs.MessageID, responseMessageIndex(reasoningStarted), text+"\n"))
		}
		if state.Text != "" {
			state.Text += "\n\n"
		}
		state.Text += text
	}
	if refs := adapter.ReferencesSuffix(); refs != "" {
		state.Text += refs
	}
	if len(state.Annotations) == 0 {
		state.Annotations = adapter.AnnotationsList()
	}
	state.SearchSources = adapter.SearchSourcesList()
	if options.Request.Stream && !toolCallsEmitted {
		msgIndex := responseMessageIndex(reasoningStarted)
		if !messageStarted {
			frames = append(frames, responseMessageStartFrames(options.IDs, msgIndex)...)
		}
		frames = append(frames, responseMessageDoneFrames(options.IDs, msgIndex, state)...)
	}
	return responseStreamResult{State: state, ToolItems: toolItems}, frames, nil
}
