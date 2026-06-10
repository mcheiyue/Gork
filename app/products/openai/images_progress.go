package openai

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func imageEventKey(event map[string]any, fallbackPrefix string, fallbackCount int) any {
	if key, ok := event["image_id"]; ok && key != nil {
		if text, ok := key.(string); !ok || strings.TrimSpace(text) != "" {
			return key
		}
	}
	return fmt.Sprintf("%s-%d", fallbackPrefix, fallbackCount)
}

type imageProgressTracker struct {
	total        int
	progressMap  map[any]int
	completedIDs map[any]struct{}
	lastProgress int
	reasons      []string
}

func newImageProgressTracker(total int) *imageProgressTracker {
	if total <= 0 {
		total = 1
	}
	return &imageProgressTracker{
		total:        total,
		progressMap:  map[any]int{},
		completedIDs: map[any]struct{}{},
		lastProgress: -1,
	}
}

func (tracker *imageProgressTracker) Record(key any, progress int) (string, bool) {
	progress = clampProgress(progress)
	tracker.progressMap[key] = progress
	if progress >= 100 {
		tracker.completedIDs[key] = struct{}{}
	}
	aggregate := computeProgressPercent(tracker.progressMap, tracker.total)
	if aggregate <= tracker.lastProgress {
		return "", false
	}
	tracker.lastProgress = aggregate
	reason := progressReason("图片", aggregate, len(tracker.completedIDs), tracker.total)
	tracker.reasons = append(tracker.reasons, reason)
	return reason, true
}

func (tracker *imageProgressTracker) ReasoningContent() string {
	return strings.Join(tracker.reasons, "\n")
}

func appendImageThinkingFrame(frames []string, modelName, responseID, reason string) []string {
	return append(frames, formatChatDataFrame(MakeThinkingChunk(ThinkingChunkParams{
		ResponseID: responseID,
		Model:      modelName,
		Content:    reason + "\n",
	})))
}

func imageProgressInt(value any) int {
	if text, ok := value.(string); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			return 0
		}
		return parsed
	}
	return intFromAny(value)
}

func imageResponseData(images []imageOutput, responseFormat string) ([]map[string]any, error) {
	format, err := normalizeImageResponseFormat(responseFormat)
	if err != nil {
		return nil, err
	}
	data := make([]map[string]any, 0, len(images))
	for _, image := range images {
		if format == "b64_json" {
			data = append(data, map[string]any{"b64_json": image.APIValue})
		} else {
			data = append(data, map[string]any{"url": image.APIValue})
		}
	}
	return data, nil
}

func clampProgress(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func computeProgressPercent(progressMap map[any]int, total int) int {
	if total <= 0 || len(progressMap) == 0 {
		return 0
	}
	values := make([]int, 0, len(progressMap))
	for _, value := range progressMap {
		values = append(values, clampProgress(value))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(values)))
	sum := 0
	for i := 0; i < total && i < len(values); i++ {
		sum += values[i]
	}
	return clampProgress(sum / total)
}

func completedItems(progressMap map[any]int) int {
	count := 0
	for _, value := range progressMap {
		if clampProgress(value) >= 100 {
			count++
		}
	}
	return count
}

func progressReason(label string, progress, completed, total int) string {
	reason := fmt.Sprintf("正在生成 %s%d%%", label, clampProgress(progress))
	if total > 0 {
		reason += fmt.Sprintf(" (%d/%d)", completed, total)
	}
	return reason
}

func imageStreamFrames(images []imageOutput, modelName, responseID string, chatFormat bool) []string {
	frames := make([]string, 0, len(images)+2)
	return appendImageStreamFrames(frames, images, modelName, responseID, chatFormat)
}

func appendImageStreamFrames(frames []string, images []imageOutput, modelName, responseID string, chatFormat bool) []string {
	for _, image := range images {
		frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
			ResponseID: responseID,
			Model:      modelName,
			Content:    outputContent(image, chatFormat),
		})))
	}
	frames = append(frames,
		formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
			ResponseID:   responseID,
			Model:        modelName,
			Content:      "",
			IsFinal:      true,
			FinishReason: "stop",
		})),
		"data: [DONE]\n\n",
	)
	return frames
}

func joinImageOutputs(images []imageOutput, chatFormat bool) string {
	parts := make([]string, 0, len(images))
	for _, image := range images {
		parts = append(parts, outputContent(image, chatFormat))
	}
	return strings.Join(parts, "\n\n")
}
