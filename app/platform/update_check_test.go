package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSelectLatestReleaseIgnoresDraftsAndRCOrdering(t *testing.T) {
	release := selectLatestRelease([]githubRelease{
		{TagName: "v1.2.3-rc1", Name: "rc"},
		{TagName: "v1.2.2", Name: "old"},
		{TagName: "v9.0.0", Name: "draft", Draft: true},
		{TagName: "v1.2.3", Name: "final"},
		{TagName: "not-a-version", Name: "invalid"},
	})
	if release == nil || release.Name != "final" {
		t.Fatalf("latest release = %#v", release)
	}
	if !isNewerVersion("1.2.3", "1.2.3-rc1") {
		t.Fatalf("final release should be newer than rc")
	}
	if isNewerVersion("1.2.3-rc1", "1.2.3") {
		t.Fatalf("rc should not be newer than final")
	}
}

func TestGetLatestReleaseInfoBuildsPayloadAndCaches(t *testing.T) {
	resetUpdateCheckForTest(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("per_page") != "100" {
			t.Fatalf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.0.1","name":"old","draft":false},
			{"tag_name":"v999.0.0","name":"draft","draft":true},
			{"tag_name":"v99.0.0","name":"v99.0.0","html_url":"https://example.test/r","published_at":"2026-01-01T00:00:00Z","body":"notes","draft":false}
		]`))
	}))
	defer server.Close()
	updateReleasesURL = server.URL
	updateNow = func() time.Time { return time.Date(2026, 6, 6, 1, 2, 3, 0, time.UTC) }

	info := GetLatestReleaseInfo(context.Background(), false)
	if info.Status != "ok" || info.LatestVersion != "99.0.0" || !info.UpdateAvailable {
		t.Fatalf("info = %#v", info)
	}
	if info.ReleaseURL != "https://example.test/r" || info.ReleaseNotes != "notes" || info.CheckedAt != "2026-06-06T01:02:03Z" {
		t.Fatalf("payload details = %#v", info)
	}
	cached := GetLatestReleaseInfo(context.Background(), false)
	if calls != 1 || cached.LatestVersion != info.LatestVersion {
		t.Fatalf("cache calls=%d cached=%#v", calls, cached)
	}
	_ = GetLatestReleaseInfo(context.Background(), true)
	if calls != 2 {
		t.Fatalf("force should bypass cache, calls=%d", calls)
	}
}

func TestBuildUpdatePayloadUsesPythonTimestampFormat(t *testing.T) {
	resetUpdateCheckForTest(t)
	updateNow = func() time.Time { return time.Date(2026, 6, 6, 1, 2, 3, 456000000, time.UTC) }

	info := buildUpdatePayload(nil, "")
	if info.CheckedAt != "2026-06-06T01:02:03.456000Z" {
		t.Fatalf("checked_at = %q", info.CheckedAt)
	}
}

func TestGetLatestReleaseInfoReturnsNormalizedErrorPayload(t *testing.T) {
	resetUpdateCheckForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusForbidden)
	}))
	defer server.Close()
	updateReleasesURL = server.URL

	info := GetLatestReleaseInfo(context.Background(), true)
	if info.Status != "error" || info.Error != "GitHub API rate limit exceeded." {
		t.Fatalf("error info = %#v", info)
	}
}

func TestGetLatestReleaseInfoIgnoresNonObjectReleases(t *testing.T) {
	resetUpdateCheckForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			"not an object",
			{"tag_name":"v7.8.9","name":"valid","draft":false}
		]`))
	}))
	defer server.Close()
	updateReleasesURL = server.URL

	info := GetLatestReleaseInfo(context.Background(), true)
	if info.Status != "ok" || info.LatestVersion != "7.8.9" {
		t.Fatalf("info = %#v", info)
	}
}

func TestNormalizeUpdateErrorMatchesPythonFallback(t *testing.T) {
	if got, want := normalizeUpdateError(""), "Update check failed."; got != want {
		t.Fatalf("normalizeUpdateError(empty) = %q, want %q", got, want)
	}
}

func resetUpdateCheckForTest(t *testing.T) {
	t.Helper()
	oldURL := updateReleasesURL
	oldClient := updateHTTPClient
	oldNow := updateNow
	updateCache = updateCheckCache{}
	updateHTTPClient = http.DefaultClient
	updateNow = func() time.Time { return time.Now().UTC() }
	t.Cleanup(func() {
		updateReleasesURL = oldURL
		updateHTTPClient = oldClient
		updateNow = oldNow
		updateCache = updateCheckCache{}
	})
}
