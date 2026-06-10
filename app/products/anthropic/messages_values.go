package anthropic

func anthropicMap(value any) map[string]any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	return map[string]any{}
}

func anthropicMapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				result = append(result, mapped)
			}
		}
		return result
	default:
		return nil
	}
}

func filterAnthropicBlocks(blocks []map[string]any, blockType string) []map[string]any {
	result := []map[string]any{}
	for _, block := range blocks {
		if anthropicString(block["type"], "") == blockType {
			result = append(result, block)
		}
	}
	return result
}

func anthropicString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func valueOrEmptyMap(value any) any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
