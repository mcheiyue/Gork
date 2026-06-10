package backends

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Flatten recursively flattens nested config to dotted JSON values.
func Flatten(nested map[string]any) (map[string]string, error) {
	out := map[string]string{}
	if err := flattenInto(out, nested, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func flattenInto(out map[string]string, nested map[string]any, prefix string) error {
	for key, value := range nested {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		if child, ok := value.(map[string]any); ok {
			if err := flattenInto(out, child, full); err != nil {
				return err
			}
			continue
		}
		encoded, err := encodePythonJSON(value)
		if err != nil {
			return err
		}
		out[full] = encoded
	}
	return nil
}

func encodePythonJSON(value any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	encoded := strings.TrimSuffix(buf.String(), "\n")
	return addPythonJSONSeparatorSpaces(encoded), nil
}

func addPythonJSONSeparatorSpaces(encoded string) string {
	var out strings.Builder
	out.Grow(len(encoded))
	inString := false
	escaped := false
	for i := 0; i < len(encoded); i++ {
		ch := encoded[i]
		out.WriteByte(ch)
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case ',', ':':
			out.WriteByte(' ')
		}
	}
	return out.String()
}

// Unflatten rebuilds a nested config map from dotted JSON values.
func Unflatten(flat map[string]string) map[string]any {
	result := map[string]any{}
	for dotted, raw := range flat {
		parts := strings.Split(dotted, ".")
		node := result
		for _, part := range parts[:len(parts)-1] {
			next, ok := node[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[part] = next
			}
			node = next
		}
		node[parts[len(parts)-1]] = decodeJSONValue(raw)
	}
	return result
}

func decodeJSONValue(raw string) any {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return raw
	}
	return normalizeJSONNumbers(value)
}

func normalizeJSONNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			return i
		}
		if f, err := typed.Float64(); err == nil {
			return f
		}
		return typed.String()
	case []any:
		for i, item := range typed {
			typed[i] = normalizeJSONNumbers(item)
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeJSONNumbers(item)
		}
		return typed
	default:
		return value
	}
}
