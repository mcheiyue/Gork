package openai

import "encoding/json"

// MessageItem is one OpenAI-compatible chat message.
type MessageItem struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []map[string]any `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

// ImageConfig carries chat image generation/edit options.
type ImageConfig struct {
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

func (c *ImageConfig) UnmarshalJSON(raw []byte) error {
	type alias ImageConfig
	value := alias{N: 1, Size: "1024x1024"}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*c = ImageConfig(value)
	return nil
}

// VideoConfig carries chat video generation options.
type VideoConfig struct {
	Seconds        int    `json:"seconds,omitempty"`
	Size           string `json:"size,omitempty"`
	ResolutionName string `json:"resolution_name,omitempty"`
	Preset         string `json:"preset,omitempty"`
}

func (c *VideoConfig) UnmarshalJSON(raw []byte) error {
	type alias VideoConfig
	value := alias{Seconds: 6, Size: "720x1280"}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*c = VideoConfig(value)
	return nil
}

// ChatCompletionRequest is the /v1/chat/completions request body.
type ChatCompletionRequest struct {
	Model             string           `json:"model"`
	Messages          []MessageItem    `json:"messages"`
	Stream            *bool            `json:"stream,omitempty"`
	ReasoningEffort   *string          `json:"reasoning_effort,omitempty"`
	Temperature       *float64         `json:"temperature,omitempty"`
	TopP              *float64         `json:"top_p,omitempty"`
	ImageConfig       *ImageConfig     `json:"image_config,omitempty"`
	VideoConfig       *VideoConfig     `json:"video_config,omitempty"`
	Tools             []map[string]any `json:"tools,omitempty"`
	ToolChoice        any              `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool            `json:"parallel_tool_calls,omitempty"`
	MaxTokens         *int             `json:"max_tokens,omitempty"`
}

func (r *ChatCompletionRequest) UnmarshalJSON(raw []byte) error {
	type alias ChatCompletionRequest
	temperature := 0.8
	topP := 0.95
	parallelToolCalls := true
	value := alias{
		Temperature:       &temperature,
		TopP:              &topP,
		ParallelToolCalls: &parallelToolCalls,
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*r = ChatCompletionRequest(value)
	return nil
}

// ImageGenerationRequest is the /v1/images/generations JSON body.
type ImageGenerationRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

func (r *ImageGenerationRequest) UnmarshalJSON(raw []byte) error {
	type alias ImageGenerationRequest
	value := alias{N: 1, Size: "1024x1024", ResponseFormat: "url"}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*r = ImageGenerationRequest(value)
	return nil
}

// ImageEditRequest mirrors the JSON schema used by non-multipart callers.
type ImageEditRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	Image          any    `json:"image"`
	Mask           string `json:"mask,omitempty"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

func (r *ImageEditRequest) UnmarshalJSON(raw []byte) error {
	type alias ImageEditRequest
	value := alias{N: 1, Size: "1024x1024", ResponseFormat: "url"}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*r = ImageEditRequest(value)
	return nil
}

// ResponsesCreateRequest is the /v1/responses request body.
type ResponsesCreateRequest struct {
	Model              string         `json:"model"`
	Input              any            `json:"input"`
	Instructions       string         `json:"instructions,omitempty"`
	Stream             *bool          `json:"stream,omitempty"`
	Reasoning          map[string]any `json:"reasoning,omitempty"`
	Temperature        *float64       `json:"temperature,omitempty"`
	TopP               *float64       `json:"top_p,omitempty"`
	MaxOutputTokens    *int           `json:"max_output_tokens,omitempty"`
	Tools              []any          `json:"tools,omitempty"`
	ToolChoice         any            `json:"tool_choice,omitempty"`
	PreviousResponseID string         `json:"previous_response_id,omitempty"`
	Store              *bool          `json:"store,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	Truncation         string         `json:"truncation,omitempty"`
	ParallelToolCalls  *bool          `json:"parallel_tool_calls,omitempty"`
	Include            []string       `json:"include,omitempty"`
	Background         *bool          `json:"background,omitempty"`
}
