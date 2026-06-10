package anthropic

import (
	"context"
	"strings"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
	productopenai "github.com/jiujiu532/grok2api/app/products/openai"
)

type messagesStreamState struct {
	frames           []string
	adapter          *protocol.StreamAdapter
	sieve            *productopenai.ToolSieve
	textParts        []string
	thinkParts       []string
	annotations      []map[string]any
	blockIndex       int
	thinkStarted     bool
	thinkClosed      bool
	textStarted      bool
	toolCallsEmitted bool
	toolOutputTokens int
}

func messagesStreamResult(ctx context.Context, options MessagesOptions, plan messagesPlan, token string, lines []string) (MessagesResult, error) {
	state := newMessagesStreamState(plan)
	if err := state.consume(ctx, options, plan, token, lines); err != nil {
		return MessagesResult{}, err
	}
	state.finish(ctx, options, plan, token)
	return MessagesResult{IsStream: true, StreamFrames: state.frames}, nil
}

func newMessagesStreamState(plan messagesPlan) *messagesStreamState {
	state := &messagesStreamState{adapter: protocol.NewStreamAdapter(protocol.StreamAdapterOptions{ShowSearchSources: true})}
	state.frames = []string{
		anthropicSSE("message_start", map[string]any{"type": "message_start", "message": messagesStreamStart(plan)}),
		anthropicSSE("ping", map[string]any{"type": "ping"}),
	}
	if len(plan.ToolNames) > 0 {
		state.sieve = productopenai.NewToolSieve(plan.ToolNames)
	}
	return state
}

func (s *messagesStreamState) consume(ctx context.Context, options MessagesOptions, plan messagesPlan, token string, lines []string) error {
	for _, line := range lines {
		eventType, data := protocol.ClassifyLine(line)
		if eventType == "done" {
			break
		}
		if eventType != "data" || data == "" {
			continue
		}
		events, err := s.adapter.Feed(data)
		if err != nil {
			return err
		}
		if s.handleEvents(options, events) {
			break
		}
		_ = ctx
		_ = plan
		_ = token
	}
	s.flushSieve()
	return nil
}

func (s *messagesStreamState) handleEvents(options MessagesOptions, events []protocol.FrameEvent) bool {
	for _, event := range events {
		switch event.Kind {
		case "thinking":
			s.handleThinking(options.EmitThink, event.Content)
		case "text":
			if s.handleText(event.Content) {
				return true
			}
		case "annotation":
			s.annotations = append(s.annotations, event.AnnotationData)
		case "soft_stop":
			return true
		}
	}
	return false
}

func (s *messagesStreamState) handleThinking(emitThink bool, content string) {
	if !emitThink || s.thinkClosed || content == "" {
		return
	}
	if !s.thinkStarted {
		s.thinkStarted = true
		s.frames = append(s.frames, anthropicSSE("content_block_start", map[string]any{"type": "content_block_start", "index": s.blockIndex, "content_block": map[string]any{"type": "thinking", "thinking": ""}}))
	}
	s.thinkParts = append(s.thinkParts, content)
	s.frames = append(s.frames, anthropicSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": s.blockIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": content}}))
}

func (s *messagesStreamState) handleText(content string) bool {
	s.closeThinkingBlock()
	if s.sieve != nil {
		safeText, calls := s.sieve.Feed(content)
		if calls != nil {
			s.emitToolCalls(calls)
			return true
		}
		content = safeText
	}
	s.emitTextDelta(content)
	return false
}

func (s *messagesStreamState) emitTextDelta(content string) {
	if content == "" {
		return
	}
	if !s.textStarted {
		s.textStarted = true
		s.frames = append(s.frames, anthropicSSE("content_block_start", map[string]any{"type": "content_block_start", "index": s.blockIndex, "content_block": map[string]any{"type": "text", "text": ""}}))
	}
	s.textParts = append(s.textParts, content)
	s.frames = append(s.frames, anthropicSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": s.blockIndex, "delta": map[string]any{"type": "text_delta", "text": content}}))
}

func (s *messagesStreamState) closeThinkingBlock() {
	if !s.thinkStarted || s.thinkClosed {
		return
	}
	s.thinkClosed = true
	s.frames = append(s.frames, anthropicSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.blockIndex}))
	s.blockIndex++
}

func (s *messagesStreamState) flushSieve() {
	if s.sieve == nil || s.toolCallsEmitted {
		return
	}
	if calls := s.sieve.Flush(); len(calls) > 0 {
		s.closeTextBlock()
		s.emitToolCalls(calls)
	}
}

func (s *messagesStreamState) emitToolCalls(calls []protocol.ParsedToolCall) {
	for _, call := range calls {
		s.frames = append(s.frames,
			anthropicSSE("content_block_start", map[string]any{"type": "content_block_start", "index": s.blockIndex, "content_block": map[string]any{"type": "tool_use", "id": call.CallID, "name": call.Name, "input": map[string]any{}}}),
			anthropicSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": s.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": call.Arguments}}),
			anthropicSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.blockIndex}),
		)
		s.blockIndex++
	}
	s.toolOutputTokens = messagesToolCallTokens(calls)
	s.toolCallsEmitted = true
}

func (s *messagesStreamState) closeTextBlock() {
	if !s.textStarted {
		return
	}
	s.frames = append(s.frames, anthropicSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.blockIndex}))
	s.textStarted = false
	s.blockIndex++
}

func (s *messagesStreamState) finish(ctx context.Context, options MessagesOptions, plan messagesPlan, token string) {
	if s.toolCallsEmitted {
		s.finishToolStream()
		return
	}
	s.appendImagesAndReferences(ctx, token)
	s.closeThinkingBlock()
	if s.textStarted {
		s.frames = append(s.frames, anthropicSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.blockIndex}))
	}
	outputTokens := platform.EstimateTokens(strings.Join(s.textParts, "")) + platform.EstimateTokens(strings.Join(s.thinkParts, ""))
	s.frames = append(s.frames, anthropicSSE("message_delta", map[string]any{"type": "message_delta", "delta": s.finalDelta(), "usage": map[string]any{"output_tokens": outputTokens}}))
	s.frames = append(s.frames, anthropicSSE("message_stop", map[string]any{"type": "message_stop"}), "data: [DONE]\n\n")
	_ = options
	_ = plan
}

func (s *messagesStreamState) appendImagesAndReferences(ctx context.Context, token string) {
	for _, image := range s.adapter.ImageURLs {
		if resolved, err := messagesImageResolver(ctx, token, image.URL, image.ImageID); err == nil && resolved != "" {
			s.emitTextDelta(resolved + "\n")
		}
	}
	if references := s.adapter.ReferencesSuffix(); references != "" {
		s.emitTextDelta(references)
	}
}

func (s *messagesStreamState) finalDelta() map[string]any {
	delta := map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}
	if sources := s.adapter.SearchSourcesList(); len(sources) > 0 {
		delta["search_sources"] = sources
	}
	if len(s.annotations) > 0 {
		delta["annotations"] = s.annotations
	}
	return delta
}

func (s *messagesStreamState) finishToolStream() {
	delta := map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}
	if sources := s.adapter.SearchSourcesList(); len(sources) > 0 {
		delta["search_sources"] = sources
	}
	s.frames = append(s.frames, anthropicSSE("message_delta", map[string]any{"type": "message_delta", "delta": delta, "usage": map[string]any{"output_tokens": s.toolOutputTokens}}))
	s.frames = append(s.frames, anthropicSSE("message_stop", map[string]any{"type": "message_stop"}), "data: [DONE]\n\n")
}

func messagesStreamStart(plan messagesPlan) map[string]any {
	return map[string]any{
		"id": plan.MessageID, "type": "message", "role": "assistant", "model": plan.Spec.ModelName,
		"content": []any{}, "stop_reason": nil,
		"usage": map[string]any{"input_tokens": platform.EstimatePromptTokens(plan.Message, platform.PromptOverhead), "output_tokens": 0},
	}
}
