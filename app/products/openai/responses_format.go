package openai

import (
	"fmt"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func buildResponseFunctionCallItems(calls []protocol.ParsedToolCall) []map[string]any {
	items := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		items = append(items, map[string]any{
			"id":        MakeRespID("fc"),
			"type":      "function_call",
			"call_id":   call.CallID,
			"name":      call.Name,
			"arguments": call.Arguments,
			"status":    "completed",
		})
	}
	return items
}

func emitResponseFunctionCallEvents(items []map[string]any, baseIndex int) []string {
	frames := []string{}
	for i, item := range items {
		outputIndex := baseIndex + i
		itemID := stringValue(item["id"], "")
		frames = append(frames,
			FormatSSE("response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": outputIndex,
				"item": map[string]any{
					"id":        itemID,
					"type":      "function_call",
					"call_id":   item["call_id"],
					"name":      item["name"],
					"arguments": "",
					"status":    "in_progress",
				},
			}),
			FormatSSE("response.function_call_arguments.delta", map[string]any{
				"type":         "response.function_call_arguments.delta",
				"item_id":      itemID,
				"output_index": outputIndex,
				"delta":        item["arguments"],
			}),
			FormatSSE("response.function_call_arguments.done", map[string]any{
				"type":         "response.function_call_arguments.done",
				"item_id":      itemID,
				"output_index": outputIndex,
				"arguments":    item["arguments"],
			}),
			FormatSSE("response.output_item.done", map[string]any{
				"type":         "response.output_item.done",
				"output_index": outputIndex,
				"item":         item,
			}),
		)
	}
	return frames
}

func responseOutputItems(ids responseIDs, state chatCompletionState, toolNames []string, toolItems []map[string]any) []map[string]any {
	output := []map[string]any{}
	if state.Thinking != "" {
		output = append(output, responseReasoningItem(ids.ReasoningID, state.Thinking))
	}
	if len(toolItems) > 0 {
		return append(output, toolItems...)
	}
	if len(toolNames) > 0 {
		parsed := protocol.ParseToolCalls(state.Text, toolNames)
		if len(parsed.Calls) > 0 {
			return append(output, buildResponseFunctionCallItems(parsed.Calls)...)
		}
	}
	output = append(output, responseMessageItem(ids.MessageID, state))
	return output
}

func responseReasoningItem(id, text string) map[string]any {
	return map[string]any{
		"id":      id,
		"type":    "reasoning",
		"summary": []map[string]any{{"type": "summary_text", "text": text}},
		"status":  "completed",
	}
}

func responseMessageItem(id string, state chatCompletionState) map[string]any {
	item := map[string]any{
		"id":     id,
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{{
			"type":        "output_text",
			"text":        state.Text,
			"annotations": state.Annotations,
		}},
	}
	if len(state.SearchSources) > 0 {
		item["search_sources"] = state.SearchSources
	}
	return item
}

func responseInitialFrames(options responseAttemptOptions) []string {
	if !options.Request.Stream {
		return nil
	}
	return []string{FormatSSE("response.created", map[string]any{
		"type": "response.created",
		"response": MakeRespObject(RespObjectParams{
			ResponseID: options.IDs.ResponseID,
			Model:      options.Request.Model,
			Status:     "in_progress",
			Output:     []map[string]any{},
		}),
	})}
}

func responseReasoningStartFrames(ids responseIDs) []string {
	return []string{
		FormatSSE("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": 0,
			"item": map[string]any{
				"id":      ids.ReasoningID,
				"type":    "reasoning",
				"summary": []any{},
				"status":  "in_progress",
			},
		}),
		FormatSSE("response.reasoning_summary_part.added", map[string]any{
			"type":          "response.reasoning_summary_part.added",
			"item_id":       ids.ReasoningID,
			"output_index":  0,
			"summary_index": 0,
			"part":          map[string]any{"type": "summary_text", "text": ""},
		}),
	}
}

func responseReasoningDoneFrames(ids responseIDs, text string) []string {
	return []string{
		FormatSSE("response.reasoning_summary_text.done", map[string]any{
			"type":          "response.reasoning_summary_text.done",
			"item_id":       ids.ReasoningID,
			"output_index":  0,
			"summary_index": 0,
			"text":          text,
		}),
		FormatSSE("response.reasoning_summary_part.done", map[string]any{
			"type":          "response.reasoning_summary_part.done",
			"item_id":       ids.ReasoningID,
			"output_index":  0,
			"summary_index": 0,
			"part":          map[string]any{"type": "summary_text", "text": text},
		}),
		FormatSSE("response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": 0,
			"item":         responseReasoningItem(ids.ReasoningID, text),
		}),
	}
}

func responseMessageStartFrames(ids responseIDs, outputIndex int) []string {
	return []string{
		FormatSSE("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": outputIndex,
			"item": map[string]any{
				"id":      ids.MessageID,
				"type":    "message",
				"role":    "assistant",
				"content": []any{},
				"status":  "in_progress",
			},
		}),
		FormatSSE("response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"item_id":       ids.MessageID,
			"output_index":  outputIndex,
			"content_index": 0,
			"part":          map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
		}),
	}
}

func responseTextDeltaFrame(messageID string, outputIndex int, delta string) string {
	return FormatSSE("response.output_text.delta", map[string]any{
		"type":          "response.output_text.delta",
		"item_id":       messageID,
		"output_index":  outputIndex,
		"content_index": 0,
		"delta":         delta,
	})
}

func responseMessageDoneFrames(ids responseIDs, outputIndex int, state chatCompletionState) []string {
	item := responseMessageItem(ids.MessageID, state)
	return []string{
		FormatSSE("response.output_text.done", map[string]any{
			"type":          "response.output_text.done",
			"item_id":       ids.MessageID,
			"output_index":  outputIndex,
			"content_index": 0,
			"text":          state.Text,
		}),
		FormatSSE("response.content_part.done", map[string]any{
			"type":          "response.content_part.done",
			"item_id":       ids.MessageID,
			"output_index":  outputIndex,
			"content_index": 0,
			"part":          map[string]any{"type": "output_text", "text": state.Text, "annotations": state.Annotations},
		}),
		FormatSSE("response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": outputIndex,
			"item":         item,
		}),
	}
}

func responseMessageIndex(reasoningStarted bool) int {
	if reasoningStarted {
		return 1
	}
	return 0
}

func estimateToolCallTokens(items []map[string]any) int {
	total := 0
	for _, item := range items {
		if stringValue(item["type"], "") != "function_call" {
			continue
		}
		total += platform.EstimateTokens(fmt.Sprint(item["name"])) + platform.EstimateTokens(fmt.Sprint(item["arguments"]))
	}
	return total
}

func estimateResponseOutputTokens(output []map[string]any, state chatCompletionState) int {
	if estimate := estimateToolCallTokens(output); estimate > 0 {
		return estimate
	}
	return platform.EstimateTokens(state.Text)
}
