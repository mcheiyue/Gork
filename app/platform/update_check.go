package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	updateCacheTTL = 24 * time.Hour
	updateErrorTTL = 5 * time.Minute
)

var (
	updateReleasesURL = "https://api.github.com/repos/jiujiu532/grok2api/releases"
	updateHTTPClient  = &http.Client{Timeout: 10 * time.Second}
	updateNow         = time.Now
	updateMutex       sync.Mutex
	updateCache       updateCheckCache
	versionPattern    = regexp.MustCompile(`(?i)^(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:(?:\.|-)?rc(\d+))?$`)
	statusPattern     = regexp.MustCompile(`GitHub release query failed:\s*(\d{3})`)
)

type UpdateInfo struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	ReleaseName     string `json:"release_name"`
	ReleaseURL      string `json:"release_url"`
	PublishedAt     string `json:"published_at"`
	ReleaseNotes    string `json:"release_notes"`
	UpdateAvailable bool   `json:"update_available"`
	CheckedAt       string `json:"checked_at"`
	Status          string `json:"status"`
	Error           string `json:"error"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Draft       bool   `json:"draft"`
}

type updateCheckCache struct {
	Payload   UpdateInfo
	ExpiresAt time.Time
	Valid     bool
}

func GetLatestReleaseInfo(ctx context.Context, force bool) UpdateInfo {
	now := updateNow()
	if !force && updateCache.Valid && updateCache.ExpiresAt.After(now) {
		return updateCache.Payload
	}

	updateMutex.Lock()
	defer updateMutex.Unlock()

	now = updateNow()
	if !force && updateCache.Valid && updateCache.ExpiresAt.After(now) {
		return updateCache.Payload
	}

	release, err := fetchLatestRelease(ctx)
	ttl := updateCacheTTL
	payload := UpdateInfo{}
	if err != nil {
		payload = buildUpdatePayload(nil, err.Error())
		ttl = updateErrorTTL
	} else {
		payload = buildUpdatePayload(release, "")
	}
	updateCache = updateCheckCache{Payload: payload, ExpiresAt: now.Add(ttl), Valid: true}
	return payload
}

func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "grok2api-update-check")
	query := req.URL.Query()
	query.Set("per_page", "100")
	req.URL.RawQuery = query.Encode()

	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		detail := strings.TrimSpace(string(body))
		return nil, fmt.Errorf("GitHub release query failed: %d %s", resp.StatusCode, detail)
	}
	releases, err := decodeGithubReleases(resp.Body)
	if err != nil {
		return nil, err
	}
	release := selectLatestRelease(releases)
	if release == nil {
		return nil, errors.New("No valid GitHub releases found")
	}
	return release, nil
}

func buildUpdatePayload(release *githubRelease, errText string) UpdateInfo {
	current := GetProjectVersion()
	payload := UpdateInfo{
		CurrentVersion: current,
		CheckedAt:      utcNowISO(updateNow()),
		Status:         "ok",
		Error:          normalizeUpdateError(errText),
	}
	if errText != "" {
		payload.Status = "error"
		return payload
	}
	if release == nil {
		return payload
	}
	payload.LatestVersion = normalizeVersion(firstNonEmpty(release.TagName, release.Name))
	payload.ReleaseName = strings.TrimSpace(release.Name)
	payload.ReleaseURL = strings.TrimSpace(release.HTMLURL)
	payload.PublishedAt = strings.TrimSpace(release.PublishedAt)
	payload.ReleaseNotes = strings.TrimSpace(release.Body)
	payload.UpdateAvailable = payload.LatestVersion != "" && isNewerVersion(payload.LatestVersion, current)
	return payload
}

func selectLatestRelease(releases []githubRelease) *githubRelease {
	type candidate struct {
		key     versionKey
		release githubRelease
	}
	candidates := []candidate{}
	for _, release := range releases {
		if release.Draft {
			continue
		}
		key, ok := releaseVersionKey(release)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{key: key, release: release})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return compareVersionKeys(candidates[i].key, candidates[j].key) > 0
	})
	return &candidates[0].release
}

type versionKey [5]int

func releaseVersionKey(release githubRelease) (versionKey, bool) {
	return parseVersion(firstNonEmpty(release.TagName, release.Name))
}

func isNewerVersion(latest, current string) bool {
	latestKey, latestOK := parseVersion(latest)
	currentKey, currentOK := parseVersion(current)
	if latestOK && currentOK {
		return compareVersionKeys(latestKey, currentKey) > 0
	}
	return normalizeVersion(latest) > normalizeVersion(current)
}

func parseVersion(value string) (versionKey, bool) {
	match := versionPattern.FindStringSubmatch(normalizeVersion(value))
	if match == nil {
		return versionKey{}, false
	}
	key := versionKey{}
	for i := 1; i <= 3; i++ {
		if match[i] == "" {
			continue
		}
		parsed, _ := strconv.Atoi(match[i])
		key[i-1] = parsed
	}
	key[3] = 1
	if match[4] != "" {
		key[3] = 0
		key[4], _ = strconv.Atoi(match[4])
	}
	return key, true
}

func compareVersionKeys(left, right versionKey) int {
	for i := range left {
		if left[i] > right[i] {
			return 1
		}
		if left[i] < right[i] {
			return -1
		}
	}
	return 0
}

func normalizeVersion(value string) string {
	text := strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(text), "v") {
		return text[1:]
	}
	return text
}

func normalizeUpdateError(value string) string {
	text := strings.TrimSpace(value)
	lowered := strings.ToLower(text)
	if strings.Contains(lowered, "rate limit exceeded") {
		return "GitHub API rate limit exceeded."
	}
	if strings.HasPrefix(text, "GitHub release query failed:") {
		if match := statusPattern.FindStringSubmatch(text); match != nil {
			return fmt.Sprintf("GitHub release query failed (%s).", match[1])
		}
		return "GitHub release query failed."
	}
	switch text {
	case "":
		return "Update check failed."
	case "GitHub releases response invalid", "No valid GitHub releases found":
		return text
	default:
		return text
	}
}

func utcNowISO(value time.Time) string {
	utc := value.UTC().Truncate(time.Microsecond)
	if utc.Nanosecond() == 0 {
		return utc.Format(time.RFC3339)
	}
	return utc.Format("2006-01-02T15:04:05.000000Z")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
