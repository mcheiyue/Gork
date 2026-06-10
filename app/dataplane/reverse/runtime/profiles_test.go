package runtime

import "testing"

func TestDefaultOperationProfileMatchesPythonDataclassDefaults(t *testing.T) {
	profile := DefaultOperationProfile()

	if profile.TimeoutS != 30.0 || profile.MaxRetries != 0 || profile.RetryDelayS != 1.0 || profile.IdleTimeoutS != 0.0 {
		t.Fatalf("default profile = %#v", profile)
	}
	if len(profile.RetryCodes) != 0 {
		t.Fatalf("default retry codes = %#v, want empty", profile.RetryCodes)
	}
}

func TestOperationProfilesMatchPythonPrebuiltProfiles(t *testing.T) {
	tests := map[string]OperationProfile{
		"chat":       ChatProfile,
		"image":      ImageProfile,
		"image_edit": ImageEditProfile,
		"video":      VideoProfile,
		"voice":      VoiceProfile,
		"asset":      AssetProfile,
		"grpc":       GRPCProfile,
	}
	want := map[string]OperationProfile{
		"chat":       {TimeoutS: 120.0, MaxRetries: 1, RetryCodes: []int{502, 503}, RetryDelayS: 2.0, IdleTimeoutS: 30.0},
		"image":      {TimeoutS: 300.0, MaxRetries: 0, RetryDelayS: 1.0, IdleTimeoutS: 60.0},
		"image_edit": {TimeoutS: 120.0, MaxRetries: 1, RetryCodes: []int{502, 503}, RetryDelayS: 2.0, IdleTimeoutS: 30.0},
		"video":      {TimeoutS: 60.0, MaxRetries: 1, RetryCodes: []int{429, 502, 503}, RetryDelayS: 5.0},
		"voice":      {TimeoutS: 120.0, MaxRetries: 0, RetryDelayS: 1.0, IdleTimeoutS: 15.0},
		"asset":      {TimeoutS: 60.0, MaxRetries: 2, RetryCodes: []int{502, 503}, RetryDelayS: 1.0},
		"grpc":       {TimeoutS: 15.0, MaxRetries: 1, RetryCodes: []int{503}, RetryDelayS: 0.5},
	}

	for name, expected := range want {
		assertProfile(t, name, tests[name], expected)
		assertProfile(t, "Profiles["+name+"]", Profiles[name], expected)
	}
	if len(Profiles) != len(want) {
		t.Fatalf("Profiles length = %d, want %d", len(Profiles), len(want))
	}
}

func TestOperationProfileRetriesStatus(t *testing.T) {
	if !ChatProfile.RetriesStatus(502) || !ChatProfile.RetriesStatus(503) {
		t.Fatalf("chat profile should retry 502 and 503")
	}
	if ChatProfile.RetriesStatus(429) {
		t.Fatalf("chat profile should not retry 429")
	}
	for name, profile := range Profiles {
		for _, code := range profile.RetryCodes {
			if !profile.RetriesStatus(code) {
				t.Fatalf("%s should retry %d", name, code)
			}
		}
	}
	if VideoProfile.RetriesStatus(500) {
		t.Fatalf("video profile should not retry 500")
	}
	if GRPCProfile.RetriesStatus(502) {
		t.Fatalf("grpc profile should not retry 502")
	}
}

func assertProfile(t *testing.T, name string, got, want OperationProfile) {
	t.Helper()
	if got.TimeoutS != want.TimeoutS || got.MaxRetries != want.MaxRetries || got.RetryDelayS != want.RetryDelayS || got.IdleTimeoutS != want.IdleTimeoutS {
		t.Fatalf("%s scalar fields = %#v, want %#v", name, got, want)
	}
	if len(got.RetryCodes) != len(want.RetryCodes) {
		t.Fatalf("%s retry codes = %#v, want %#v", name, got.RetryCodes, want.RetryCodes)
	}
	for i := range want.RetryCodes {
		if got.RetryCodes[i] != want.RetryCodes[i] {
			t.Fatalf("%s retry codes = %#v, want %#v", name, got.RetryCodes, want.RetryCodes)
		}
	}
}
