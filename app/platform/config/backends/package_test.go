package backends

import (
	"os"
	"strings"
	"testing"
)

func TestPackageDocMarksPythonBackendsPackageExports(t *testing.T) {
	content, err := os.ReadFile("package.go")
	if err != nil {
		t.Fatalf("read package doc: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"Python app.platform.config.backends package boundary",
		"app/platform/config/backends/__init__.py",
		"ConfigBackend",
		"create_config_backend",
		"get_config_backend_name",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("package.go missing %q in:\n%s", want, text)
		}
	}
}
