package build

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeChatResponseFormat 将 OpenAI chat 的 response_format 转为
// Build Responses 上游的 text.format 对象。
// 对齐 chenyme e376c22/normalize.go：
//   - type=json_schema 时提升 json_schema 内字段到 format 顶层，强制 type=json_schema，
//     跳过 schema 对象内自带 type（防 type:object 覆盖）
//   - 其它 type（如 json_object）原样返回
// 空输入返回 (nil, nil)，调用方不注入 text。
func NormalizeChatResponseFormat(raw any) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return map[string]any{"type": strings.TrimSpace(typed)}, nil
	case map[string]any:
		return normalizeResponseFormatMap(typed)
	case json.RawMessage:
		if len(typed) == 0 || string(typed) == "null" {
			return nil, nil
		}
		var m map[string]any
		if err := json.Unmarshal(typed, &m); err != nil {
			return nil, fmt.Errorf("解析 response_format: %w", err)
		}
		return normalizeResponseFormatMap(m)
	default:
		// 兼容解码后的其它结构：先 round-trip 成 map
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("序列化 response_format: %w", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("解析 response_format: %w", err)
		}
		return normalizeResponseFormatMap(m)
	}
}

func normalizeResponseFormatMap(format map[string]any) (map[string]any, error) {
	if len(format) == 0 {
		return nil, nil
	}
	formatType, _ := format["type"].(string)
	formatType = strings.TrimSpace(formatType)
	nested, hasNested := format["json_schema"]
	if formatType != "json_schema" || !hasNested || nested == nil {
		// 非 json_schema 或无嵌套：浅拷贝返回
		out := make(map[string]any, len(format))
		for k, v := range format {
			out[k] = v
		}
		return out, nil
	}

	schemaMap, err := asStringAnyMap(nested)
	if err != nil {
		return nil, fmt.Errorf("解析 response_format.json_schema: %w", err)
	}
	// type 强制为 json_schema；复制 schema 内字段时跳过 type
	result := make(map[string]any, len(schemaMap)+1)
	result["type"] = "json_schema"
	for key, value := range schemaMap {
		if key == "type" {
			continue
		}
		result[key] = value
	}
	return result, nil
}

func asStringAnyMap(v any) (map[string]any, error) {
	switch typed := v.(type) {
	case map[string]any:
		return typed, nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	}
}
