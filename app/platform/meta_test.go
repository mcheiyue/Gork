package platform

import (
	"strings"
	"testing"
)

func TestGetProjectMetaReturnsGoRuntimeMetadata(t *testing.T) {
	meta := GetProjectMeta()

	if got := meta["name"]; got != ProjectName {
		t.Fatalf("project name = %q, want %q", got, ProjectName)
	}
	if got := meta["version"]; got != ProjectVersion {
		t.Fatalf("project version = %q, want %q", got, ProjectVersion)
	}
	if got := GetProjectVersion(); got != ProjectVersion {
		t.Fatalf("GetProjectVersion() = %q, want %q", got, ProjectVersion)
	}
}

func TestGetProjectMetaReturnsCopy(t *testing.T) {
	meta := GetProjectMeta()
	originalVersion := meta["version"]
	meta["version"] = "mutated"

	if got := GetProjectMeta()["version"]; got != originalVersion {
		t.Fatalf("cached project version was mutated through returned map: got %q, want %q", got, originalVersion)
	}
}

func TestParseProjectTomlHandlesTomllibStringForms(t *testing.T) {
	values := parseProjectToml([]byte(`
[tool.other]
name = "ignored"
version = "ignored"

[project]
name = '  grok2api-en  '
version = "  1.2.3  " # inline comment
description = "ignored"

[project.optional-dependencies]
version = "ignored"
`))

	if got, want := values["name"], "  grok2api-en  "; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := values["version"], "  1.2.3  "; got != want {
		t.Fatalf("version = %q, want %q", got, want)
	}
	if _, ok := values["description"]; ok {
		t.Fatalf("description should not be captured: %#v", values)
	}
}

func findTomlStringValue(t *testing.T, content, key string) string {
	t.Helper()
	inProject := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "[project]" {
			inProject = true
			continue
		}
		if strings.HasPrefix(line, "[") && line != "[project]" {
			inProject = false
		}
		if !inProject || !strings.HasPrefix(line, key+" ") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid TOML line for %s: %q", key, line)
		}
		return strings.Trim(strings.TrimSpace(parts[1]), "\"")
	}
	t.Fatalf("missing [project].%s in pyproject.toml", key)
	return ""
}
