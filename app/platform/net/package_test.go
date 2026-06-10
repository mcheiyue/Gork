package net

import (
	"os"
	"strings"
	"testing"
)

func TestPackageDocMarksPythonNetPackageBoundary(t *testing.T) {
	content, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}
	doc := string(content)

	for _, want := range []string{
		"Python app.platform.net package boundary",
		"empty app/platform/net/__init__.py marker",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("doc.go missing %q", want)
		}
	}
}
