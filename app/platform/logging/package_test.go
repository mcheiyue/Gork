package logging

import (
	"os"
	"strings"
	"testing"
)

func TestPackageDocMarksPythonLoggingPackageBoundary(t *testing.T) {
	doc, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}
	text := string(doc)
	for _, token := range []string{
		"Python app.platform.logging package boundary",
		"app/platform/logging/__init__.py",
	} {
		if !strings.Contains(text, token) {
			t.Fatalf("doc.go missing %q", token)
		}
	}
}
