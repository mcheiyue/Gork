package account

import (
	"encoding/json"
	"testing"
)

func TestAccountStatusValuesAndParse(t *testing.T) {
	values := map[AccountStatus]string{
		AccountStatusActive:   "active",
		AccountStatusCooling:  "cooling",
		AccountStatusExpired:  "expired",
		AccountStatusDisabled: "disabled",
	}
	for status, want := range values {
		if status.String() != want {
			t.Fatalf("%#v String() = %q, want %q", status, status.String(), want)
		}
		parsed, ok := ParseAccountStatus(want)
		if !ok || parsed != status {
			t.Fatalf("ParseAccountStatus(%q) = %q/%v", want, parsed, ok)
		}
		raw, err := json.Marshal(status)
		if err != nil {
			t.Fatalf("marshal AccountStatus(%q): %v", status, err)
		}
		if string(raw) != `"`+want+`"` {
			t.Fatalf("AccountStatus(%q) json = %s", status, raw)
		}
		var decoded AccountStatus
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal AccountStatus(%s): %v", raw, err)
		}
		if decoded != status {
			t.Fatalf("decoded AccountStatus = %q, want %q", decoded, status)
		}
	}
	if parsed, ok := ParseAccountStatus("bad"); ok || parsed != "" {
		t.Fatalf("invalid status parsed = %q/%v", parsed, ok)
	}
	if _, err := json.Marshal(AccountStatus("bad")); err == nil {
		t.Fatal("invalid status marshaled without error")
	}
	var decoded AccountStatus
	if err := json.Unmarshal([]byte(`"bad"`), &decoded); err == nil {
		t.Fatal("invalid status unmarshaled without error")
	}
}

func TestQuotaSourceValuesMatchPythonIntEnum(t *testing.T) {
	if QuotaSourceDefault != 0 || QuotaSourceReal != 1 || QuotaSourceEstimated != 2 {
		t.Fatalf("quota sources = %d/%d/%d", QuotaSourceDefault, QuotaSourceReal, QuotaSourceEstimated)
	}
	values := map[QuotaSource]string{
		QuotaSourceDefault:   "0",
		QuotaSourceReal:      "1",
		QuotaSourceEstimated: "2",
	}
	for source, want := range values {
		if source.String() != want {
			t.Fatalf("%#v String() = %q, want %q", source, source.String(), want)
		}
		parsed, ok := ParseQuotaSource(int(source))
		if !ok || parsed != source {
			t.Fatalf("ParseQuotaSource(%d) = %d/%v", source, parsed, ok)
		}
		raw, err := json.Marshal(source)
		if err != nil {
			t.Fatalf("marshal QuotaSource(%d): %v", source, err)
		}
		if string(raw) != want {
			t.Fatalf("QuotaSource(%d) json = %s", source, raw)
		}
		var decoded QuotaSource
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal QuotaSource(%s): %v", raw, err)
		}
		if decoded != source {
			t.Fatalf("decoded QuotaSource = %d, want %d", decoded, source)
		}
	}
	if parsed, ok := ParseQuotaSource(3); ok || parsed != QuotaSourceDefault {
		t.Fatalf("invalid quota source parsed = %d/%v", parsed, ok)
	}
	if _, err := json.Marshal(QuotaSource(3)); err == nil {
		t.Fatal("invalid quota source marshaled without error")
	}
	var decoded QuotaSource
	if err := json.Unmarshal([]byte(`3`), &decoded); err == nil {
		t.Fatal("invalid quota source unmarshaled without error")
	}
}

func TestFeedbackKindValuesAndParse(t *testing.T) {
	values := map[FeedbackKind]string{
		FeedbackKindSuccess:      "success",
		FeedbackKindUnauthorized: "unauthorized",
		FeedbackKindForbidden:    "forbidden",
		FeedbackKindRateLimited:  "rate_limited",
		FeedbackKindServerError:  "server_error",
		FeedbackKindDisable:      "disable",
		FeedbackKindDelete:       "delete",
		FeedbackKindRestore:      "restore",
	}
	for kind, want := range values {
		if kind.String() != want {
			t.Fatalf("%#v String() = %q, want %q", kind, kind.String(), want)
		}
		parsed, ok := ParseFeedbackKind(want)
		if !ok || parsed != kind {
			t.Fatalf("ParseFeedbackKind(%q) = %q/%v", kind, parsed, ok)
		}
		raw, err := json.Marshal(kind)
		if err != nil {
			t.Fatalf("marshal FeedbackKind(%q): %v", kind, err)
		}
		if string(raw) != `"`+want+`"` {
			t.Fatalf("FeedbackKind(%q) json = %s", kind, raw)
		}
		var decoded FeedbackKind
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal FeedbackKind(%s): %v", raw, err)
		}
		if decoded != kind {
			t.Fatalf("decoded FeedbackKind = %q, want %q", decoded, kind)
		}
	}
	if parsed, ok := ParseFeedbackKind("bad"); ok || parsed != "" {
		t.Fatalf("invalid feedback parsed = %q/%v", parsed, ok)
	}
	if _, err := json.Marshal(FeedbackKind("bad")); err == nil {
		t.Fatal("invalid feedback kind marshaled without error")
	}
	var decoded FeedbackKind
	if err := json.Unmarshal([]byte(`"bad"`), &decoded); err == nil {
		t.Fatal("invalid feedback kind unmarshaled without error")
	}
}
