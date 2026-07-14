package openai

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/build"
)

type fakeBuildModelsDir struct {
	accounts []buildaccount.Account
}

func (f *fakeBuildModelsDir) ListActive(context.Context, time.Time) ([]buildaccount.Account, error) {
	return append([]buildaccount.Account{}, f.accounts...), nil
}
func (f *fakeBuildModelsDir) UpdateTokens(context.Context, int64, string, string, time.Time) error {
	return nil
}
func (f *fakeBuildModelsDir) SetStatus(context.Context, int64, string, string) error { return nil }

type fakeBuildModelsClient struct {
	ids   []string
	calls int
}

func (f *fakeBuildModelsClient) ListModels(context.Context, string) ([]string, error) {
	f.calls++
	return append([]string{}, f.ids...), nil
}
func (f *fakeBuildModelsClient) CreateResponse(context.Context, build.RequestMeta, io.Reader) (*http.Response, error) {
	return nil, nil
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
	client := &fakeBuildModelsClient{ids: []string{"grok-beta", "grok-4"}}
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory {
		return &fakeBuildModelsDir{accounts: []buildaccount.Account{{
			ID: 1, AccessToken: "tok", Status: buildaccount.StatusActive,
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
	if got[0].ModelName != model.BuildModelPrefix+"grok-4" && got[0].ModelName != model.BuildModelPrefix+"grok-beta" {
		// sorted: grok-4 then grok-beta? "grok-4" < "grok-beta"
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
