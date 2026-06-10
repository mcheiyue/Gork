package config

import (
	"os"
	"strings"
	"testing"
)

func TestPackageDocMarksPythonConfigPackageBoundary(t *testing.T) {
	content, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read package doc: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"Python app.platform.config package boundary",
		"empty app/platform/config/__init__.py",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doc.go missing %q in:\n%s", want, text)
		}
	}
}
