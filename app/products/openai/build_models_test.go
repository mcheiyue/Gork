package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/build"
)

type fakeBuildModelsDir struct {
	accounts []buildaccount.Account
	updated  int
	status   map[int64]string
}

func (f *fakeBuildModelsDir) ListActive(context.Context, time.Time) ([]buildaccount.Account, error) {
	return append([]buildaccount.Account{}, f.accounts...), nil
}
func (f *fakeBuildModelsDir) UpdateTokens(_ context.Context, id int64, access, refresh string, exp time.Time) error {
	f.updated++
	for i := range f.accounts {
		if f.accounts[i].ID == id {
			f.accounts[i].AccessToken = access
			f.accounts[i].RefreshToken = refresh
			f.accounts[i].ExpiresAt = exp
		}
	}
	return nil
}
func (f *fakeBuildModelsDir) SetStatus(_ context.Context, id int64, status string, _ string) error {
	if f.status == nil {
		f.status = map[int64]string{}
	}
	f.status[id] = status
	return nil
}

type fakeBuildModelsClient struct {
	idsByToken map[string][]string
	calls      int
	lastToken  string
}

func (f *fakeBuildModelsClient) ListModels(_ context.Context, token string) ([]string, error) {
	f.calls++
	f.lastToken = token
	if f.idsByToken != nil {
		if ids, ok := f.idsByToken[token]; ok {
			return append([]string{}, ids...), nil
		}
		return nil, fmt.Errorf("unauthorized token")
	}
	return nil, fmt.Errorf("no models")
}
func (f *fakeBuildModelsClient) CreateResponse(context.Context, build.RequestMeta, io.Reader) (*http.Response, error) {
	return nil, nil
}

type fakeBuildModelsOAuth struct {
	payload build.TokenPayload
	err     error
	calls   int
}

func (f *fakeBuildModelsOAuth) Refresh(context.Context, string) (build.TokenPayload, error) {
	f.calls++
	return f.payload, f.err
}

func TestListBuildModelSpecsDisabled(t *testing.T) {
	resetBuildModelsCacheForTest()
	prevFeature := buildFeatureEnabled
	prevDir := buildAccountDir
	buildFeatureEnabled = func() bool { return false }
	buildAccountDir = func() buildAccountDirectory {
		return &fakeBuildModelsDir{accounts: []buildaccount.Account{{AccessToken: "t"}}}
	}
	defer func() {
		buildFeatureEnabled = prevFeature
		buildAccountDir = prevDir
	}()
	if got := listBuildModelSpecs(context.Background()); len(got) != 0 {
		t.Fatalf("expected empty when disabled, got %#v", got)
	}
}

func TestListBuildModelSpecsWithActiveAccount(t *testing.T) {
	resetBuildModelsCacheForTest()
	prevFeature := buildFeatureEnabled
	prevDir := buildAccountDir
	prevClient := buildAPIClient
	client := &fakeBuildModelsClient{idsByToken: map[string][]string{
		"tok": {"grok-beta", "grok-4"},
	}}
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory {
		return &fakeBuildModelsDir{accounts: []buildaccount.Account{{
			ID: 1, AccessToken: "tok", Status: buildaccount.StatusActive,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}}}
	}
	buildAPIClient = func() buildHTTPClient { return client }
	defer func() {
		buildFeatureEnabled = prevFeature
		buildAccountDir = prevDir
		buildAPIClient = prevClient
		resetBuildModelsCacheForTest()
	}()

	got := listBuildModelSpecs(context.Background())
	if len(got) != 2 {
		t.Fatalf("got=%#v", got)
	}
	if !strings.HasPrefix(got[0].ModelName, model.BuildModelPrefix) {
		t.Fatalf("names=%s,%s", got[0].ModelName, got[1].ModelName)
	}
	if !got[0].IsBuildChat() {
		t.Fatalf("capability=%v", got[0].Capability)
	}
	_ = listBuildModelSpecs(context.Background())
	if client.calls != 1 {
		t.Fatalf("cache miss, calls=%d", client.calls)
	}
}

func TestListBuildModelSpecsRefreshesExpiredAccess(t *testing.T) {
	resetBuildModelsCacheForTest()
	prevFeature := buildFeatureEnabled
	prevDir := buildAccountDir
	prevClient := buildAPIClient
	prevOAuth := buildOAuthClient
	dir := &fakeBuildModelsDir{accounts: []buildaccount.Account{{
		ID: 1, AccessToken: "expired-tok", RefreshToken: "rt",
		Status: buildaccount.StatusActive, ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}}}
	client := &fakeBuildModelsClient{idsByToken: map[string][]string{
		"fresh-tok": {"grok-4.5"},
	}}
	oauth := &fakeBuildModelsOAuth{payload: build.TokenPayload{
		AccessToken: "fresh-tok", RefreshToken: "rt2", ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory { return dir }
	buildAPIClient = func() buildHTTPClient { return client }
	buildOAuthClient = func() buildTokenRefresher { return oauth }
	defer func() {
		buildFeatureEnabled = prevFeature
		buildAccountDir = prevDir
		buildAPIClient = prevClient
		buildOAuthClient = prevOAuth
		resetBuildModelsCacheForTest()
	}()

	got := listBuildModelSpecs(context.Background())
	if len(got) != 1 || got[0].ModelName != model.BuildModelPrefix+"grok-4.5" {
		t.Fatalf("got=%#v oauth.calls=%d client.last=%q", got, oauth.calls, client.lastToken)
	}
	if oauth.calls != 1 || dir.updated != 1 {
		t.Fatalf("expected refresh+update, oauth=%d updated=%d", oauth.calls, dir.updated)
	}
	if client.lastToken != "fresh-tok" {
		t.Fatalf("ListModels token=%q", client.lastToken)
	}
}

func TestListBuildModelSpecsSkipsPermanentRefreshFailure(t *testing.T) {
	resetBuildModelsCacheForTest()
	prevFeature := buildFeatureEnabled
	prevDir := buildAccountDir
	prevClient := buildAPIClient
	prevOAuth := buildOAuthClient
	dir := &fakeBuildModelsDir{accounts: []buildaccount.Account{
		{ID: 1, AccessToken: "old1", RefreshToken: "bad", Status: buildaccount.StatusActive, ExpiresAt: time.Now().UTC().Add(-time.Hour)},
		{ID: 2, AccessToken: "good", RefreshToken: "rt", Status: buildaccount.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)},
	}}
	client := &fakeBuildModelsClient{idsByToken: map[string][]string{"good": {"grok-4.5"}}}
	oauth := &fakeBuildModelsOAuth{err: &build.RefreshError{Code: "invalid_grant", Permanent: true}}
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory { return dir }
	buildAPIClient = func() buildHTTPClient { return client }
	buildOAuthClient = func() buildTokenRefresher { return oauth }
	defer func() {
		buildFeatureEnabled = prevFeature
		buildAccountDir = prevDir
		buildAPIClient = prevClient
		buildOAuthClient = prevOAuth
		resetBuildModelsCacheForTest()
	}()

	got := listBuildModelSpecs(context.Background())
	if len(got) != 1 || got[0].ModelName != model.BuildModelPrefix+"grok-4.5" {
		t.Fatalf("got=%#v", got)
	}
	if dir.status[1] != buildaccount.StatusExpired {
		t.Fatalf("status=%#v", dir.status)
	}
}

func TestListBuildModelSpecsNoAccounts(t *testing.T) {
	resetBuildModelsCacheForTest()
	prevFeature := buildFeatureEnabled
	prevDir := buildAccountDir
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory { return &fakeBuildModelsDir{} }
	defer func() {
		buildFeatureEnabled = prevFeature
		buildAccountDir = prevDir
	}()
	if got := listBuildModelSpecs(context.Background()); len(got) != 0 {
		t.Fatalf("expected empty, got %#v", got)
	}
}
