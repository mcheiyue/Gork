package backends

import (
	"reflect"
	"testing"
)

func TestFlattenSerializesNestedConfigToDottedJSONValues(t *testing.T) {
	flat, err := Flatten(map[string]any{
		"server": map[string]any{
			"host": "0.0.0.0",
			"port": 8000,
		},
		"features": map[string]any{
			"enabled": true,
			"names":   []any{"图像", "video"},
			"html":    "<&>",
		},
	})
	if err != nil {
		t.Fatalf("Flatten returned error: %v", err)
	}

	want := map[string]string{
		"server.host":      `"0.0.0.0"`,
		"server.port":      `8000`,
		"features.enabled": `true`,
		"features.names":   `["图像", "video"]`,
		"features.html":    `"<&>"`,
	}
	if !reflect.DeepEqual(flat, want) {
		t.Fatalf("Flatten() = %#v, want %#v", flat, want)
	}
}

func TestUnflattenParsesJSONValuesAndKeepsInvalidJSONAsString(t *testing.T) {
	got := Unflatten(map[string]string{
		"server.host":      `"0.0.0.0"`,
		"server.port":      `8000`,
		"features.enabled": `true`,
		"features.names":   `["图像","video"]`,
		"raw.bad":          `{not json`,
	})

	want := map[string]any{
		"server": map[string]any{
			"host": "0.0.0.0",
			"port": int64(8000),
		},
		"features": map[string]any{
			"enabled": true,
			"names":   []any{"图像", "video"},
		},
		"raw": map[string]any{
			"bad": "{not json",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Unflatten() = %#v, want %#v", got, want)
	}
}

func TestFlattenAndUnflattenHandleEmptyMaps(t *testing.T) {
	flat, err := Flatten(map[string]any{})
	if err != nil {
		t.Fatalf("Flatten returned error: %v", err)
	}
	if len(flat) != 0 {
		t.Fatalf("Flatten empty length = %d, want 0", len(flat))
	}
	if got := Unflatten(map[string]string{}); len(got) != 0 {
		t.Fatalf("Unflatten empty length = %d, want 0", len(got))
	}
}
