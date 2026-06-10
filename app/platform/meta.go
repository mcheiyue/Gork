package platform

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	projectMetaOnce sync.Once
	projectMeta     map[string]string
)

// GetProjectMeta returns project metadata sourced from pyproject.toml.
func GetProjectMeta() map[string]string {
	projectMetaOnce.Do(func() {
		projectMeta = map[string]string{"name": "grok2api", "version": "0.0.0"}
		content, err := os.ReadFile(filepath.Join(rootDir(), "pyproject.toml"))
		if err != nil {
			return
		}
		for key, value := range parseProjectToml(content) {
			if strings.TrimSpace(value) != "" {
				projectMeta[key] = strings.TrimSpace(value)
			}
		}
	})

	out := make(map[string]string, len(projectMeta))
	for key, value := range projectMeta {
		out[key] = value
	}
	return out
}

// GetProjectVersion returns the current project version.
func GetProjectVersion() string {
	return GetProjectMeta()["version"]
}

func parseProjectToml(content []byte) map[string]string {
	values := map[string]string{}
	inProject := false
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inProject = stripTomlComment(line) == "[project]"
			continue
		}
		if !inProject {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "name" && key != "version" {
			continue
		}
		values[key] = parseProjectTomlValue(value)
	}
	return values
}

func parseProjectTomlValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if value[0] == '\'' || value[0] == '"' {
		quote := value[0]
		escaped := false
		for i := 1; i < len(value); i++ {
			ch := value[i]
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				return value[1:i]
			}
		}
		return strings.Trim(value, string(quote))
	}
	return stripTomlComment(value)
}

func stripTomlComment(value string) string {
	if commentAt := strings.Index(value, "#"); commentAt >= 0 {
		value = value[:commentAt]
	}
	return strings.TrimSpace(value)
}
