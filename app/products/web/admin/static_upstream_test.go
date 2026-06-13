package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAccountStaticPortsAutoNSFWAndBatchUI(t *testing.T) {
	html := readAdminStaticFile(t, "admin", "account.html")
	for _, want := range []string{
		`id="import-auto-nsfw"`,
		`id="import-file-auto-nsfw"`,
		`auto_nsfw`,
		`appendQuery(endpoint, 'async=true')`,
		`rowActionNotSupported`,
		`summary?.expired`,
		`summary?.transient`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("account.html missing %q", want)
		}
	}
}

func TestAdminConfigStaticPortsNumberBounds(t *testing.T) {
	html := readAdminStaticFile(t, "admin", "config.html")
	for _, want := range []string{
		`function _getValue(section, key, field)`,
		`function _getCurrentValue(section, key, field)`,
		`_getCurrentValue(section, field.key, field)`,
		`if (field.min != null) attrs.min = field.min`,
		`if (field.max != null) attrs.max = field.max`,
		`if (field.min != null && n < field.min) n = field.min`,
		`if (field.max != null && n > field.max) n = field.max`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("config.html missing %q", want)
		}
	}
}

func TestAdminAccountI18nPortsAutoNSFWKeys(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "statics", "i18n", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no i18n files found")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		account := payload["account"]
		for _, key := range []string{"rowActionNotSupported", "autoNsfwOnImport", "autoNsfwHint"} {
			if _, ok := account[key]; !ok {
				t.Fatalf("%s missing account.%s", filepath.Base(file), key)
			}
		}
	}
}

func readAdminStaticFile(t *testing.T, parts ...string) string {
	t.Helper()
	pathParts := append([]string{"..", "..", "..", "statics"}, parts...)
	raw, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
