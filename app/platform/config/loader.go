package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type LoadConfigOptions struct {
	UserPath  string
	EnvPrefix string
	Env       map[string]string
}

func FlattenConfig(mapping map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for key, value := range mapping {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		if nested, ok := value.(map[string]any); ok {
			for nestedKey, nestedValue := range FlattenConfig(nested, full) {
				out[nestedKey] = nestedValue
			}
			continue
		}
		out[full] = value
	}
	return out
}

func DeepMergeConfig(base, override map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		baseNested, baseOK := result[key].(map[string]any)
		overrideNested, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			result[key] = DeepMergeConfig(baseNested, overrideNested)
			continue
		}
		result[key] = value
	}
	return result
}

func LoadTOML(path string) (map[string]any, error) {
	if path == "" {
		return map[string]any{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	defer file.Close()
	return parseSimpleTOML(file)
}

func LoadConfig(defaultsPath string, options LoadConfigOptions) (map[string]any, error) {
	data, err := LoadTOML(defaultsPath)
	if err != nil {
		return nil, err
	}
	if options.UserPath != "" {
		if user, err := LoadTOML(options.UserPath); err != nil {
			return nil, err
		} else if len(user) > 0 {
			data = DeepMergeConfig(data, user)
		}
	}
	prefix := options.EnvPrefix
	if prefix == "" {
		prefix = "GROK_"
	}
	for key, value := range loadConfigEnv(options.Env) {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		parts := strings.SplitN(strings.ToLower(key[len(prefix):]), "_", 2)
		if len(parts) != 2 {
			continue
		}
		section, item := parts[0], parts[1]
		nested, ok := data[section].(map[string]any)
		if !ok {
			nested = map[string]any{}
			data[section] = nested
		}
		nested[item] = value
	}
	return data, nil
}

func GetNested(data map[string]any, dottedKey string, defaultValue any) any {
	var node any = data
	for _, key := range strings.Split(dottedKey, ".") {
		mapping, ok := node.(map[string]any)
		if !ok {
			return defaultValue
		}
		value, ok := mapping[key]
		if !ok || value == nil {
			return defaultValue
		}
		node = value
	}
	return node
}

func parseSimpleTOML(file *os.File) (map[string]any, error) {
	data := map[string]any{}
	current := data
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := stripTOMLComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = ensureTOMLSection(data, strings.TrimSpace(line[1:len(line)-1]))
			continue
		}
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		current[strings.TrimSpace(key)] = parseTOMLValue(strings.TrimSpace(rawValue))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func ensureTOMLSection(root map[string]any, dotted string) map[string]any {
	current := root
	for _, part := range strings.Split(dotted, ".") {
		part = strings.TrimSpace(part)
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	return current
}

func stripTOMLComment(line string) string {
	quote := rune(0)
	for index, char := range line {
		if quote != 0 {
			if char == quote && (quote != '"' || index == 0 || line[index-1] != '\\') {
				quote = 0
			}
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
			continue
		}
		if char == '#' {
			return strings.TrimSpace(line[:index])
		}
	}
	return line
}

func parseTOMLValue(raw string) any {
	if len(raw) >= 2 && raw[0] == '[' && raw[len(raw)-1] == ']' {
		return parseTOMLArray(raw[1 : len(raw)-1])
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return unquoted
		}
		return raw[1 : len(raw)-1]
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1]
	}
	switch strings.ToLower(raw) {
	case "true":
		return true
	case "false":
		return false
	}
	if strings.ContainsAny(raw, ".eE") {
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			return value
		}
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return value
	}
	return raw
}

func parseTOMLArray(raw string) []any {
	values := []any{}
	for _, item := range splitTOMLArrayItems(raw) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		values = append(values, parseTOMLValue(item))
	}
	return values
}

func splitTOMLArrayItems(raw string) []string {
	items := []string{}
	start := 0
	depth := 0
	quote := byte(0)
	for index := 0; index < len(raw); index++ {
		char := raw[index]
		if quote != 0 {
			if char == quote && (quote != '"' || index == 0 || raw[index-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch char {
		case '"', '\'':
			quote = char
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				items = append(items, raw[start:index])
				start = index + 1
			}
		}
	}
	items = append(items, raw[start:])
	return items
}

func loadConfigEnv(env map[string]string) map[string]string {
	if env != nil {
		return env
	}
	out := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
