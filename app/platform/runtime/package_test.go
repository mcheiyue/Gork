package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestPackageDocMarksPythonRuntimePackageBoundary(t *testing.T) {
	content, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}
	doc := string(content)

	for _, want := range []string{
		"Python app.platform.runtime package boundary",
		"empty app/platform/runtime/__init__.py marker",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("doc.go missing %q", want)
		}
	}
}
