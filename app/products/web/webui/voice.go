package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	dataaccount "github.com/jiujiu532/grok2api/app/dataplane/account"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
	platform "github.com/jiujiu532/grok2api/app/platform"
	appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

type webUIVoiceAccountDirectory interface {
	Reserve(context.Context, []int, int) (*reverse.AccountLease, error)
	Release(context.Context, reverse.AccountLease) error
}

type webUIVoiceOptions struct {
	Voice             string
	Personality       string
	Speed             float64
	CustomInstruction string
}

type webUIVoiceRequest struct {
	Voice       string  `json:"voice"`
	Personality string  `json:"personality"`
	Speed       float64 `json:"speed"`
	Instruction string  `json:"instruction"`
}

type webUIVoiceDataDirectory struct {
	directory *dataaccount.AccountDirectory
}

func handleWebUIVoiceToken(w http.ResponseWriter, r *http.Request) {
	request, err := decodeWebUIVoiceRequest(r)
	if err != nil {
		writeWebUIError(w, platform.NewValidationError("Invalid request body", "body", "invalid_json"))
		return
	}
	lease, release, err := reserveWebUIVoiceAccount(r.Context())
	if err != nil {
		writeWebUIError(w, err)
		return
	}
	defer release()

	data, err := webUIVoiceFetchToken(r.Context(), lease.Token, request.options())
	if err != nil {
		writeWebUIVoiceError(w, err)
		return
	}
	writeWebUIVoiceResponse(w, data)
}

func decodeWebUIVoiceRequest(r *http.Request) (webUIVoiceRequest, error) {
	request := webUIVoiceRequest{Voice: "ara", Personality: "assistant", Speed: 1.0}
	if r.Body == nil {
		return request, nil
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		return webUIVoiceRequest{}, err
	}
	if request.Voice == "" {
		request.Voice = "ara"
	}
	if request.Personality == "" {
		request.Personality = "assistant"
	}
	if request.Speed == 0 {
		request.Speed = 1.0
	}
	request.Instruction = strings.TrimSpace(request.Instruction)
	return request, nil
}

func (request webUIVoiceRequest) options() webUIVoiceOptions {
	return webUIVoiceOptions{
		Voice:             request.Voice,
		Personality:       request.Personality,
		Speed:             request.Speed,
		CustomInstruction: request.Instruction,
	}
}

func reserveWebUIVoiceAccount(ctx context.Context) (reverse.AccountLease, func(), error) {
	directory := webUIVoiceDirectory()
	if directory == nil {
		return reverse.AccountLease{}, func() {}, platform.NewRateLimitError("Account directory not initialised")
	}
	lease, err := directory.Reserve(ctx, []int{1, 2}, int(controlmodel.ModeAuto))
	if err != nil {
		return reverse.AccountLease{}, func() {}, err
	}
	if lease == nil {
		return reverse.AccountLease{}, func() {}, platform.NewRateLimitError("No available tokens for voice mode")
	}
	release := func() { _ = directory.Release(ctx, *lease) }
	return *lease, release, nil
}

func defaultWebUIVoiceDirectory() webUIVoiceAccountDirectory {
	directory, err := dataaccount.GetAccountDirectory(context.Background(), nil)
	if err != nil || directory == nil {
		return nil
	}
	return webUIVoiceDataDirectory{directory: directory}
}

func (d webUIVoiceDataDirectory) Reserve(_ context.Context, poolCandidates []int, modeID int) (*reverse.AccountLease, error) {
	now := int(appruntime.NowS())
	lease, ok := d.directory.Reserve(poolCandidates, modeID, dataaccount.ReserveOptions{NowS: &now})
	if !ok {
		return nil, nil
	}
	return &reverse.AccountLease{Idx: lease.Idx, Token: lease.Token}, nil
}

func (d webUIVoiceDataDirectory) Release(_ context.Context, lease reverse.AccountLease) error {
	d.directory.Release(dataaccount.AccountLease{Idx: lease.Idx, Token: lease.Token})
	return nil
}

func writeWebUIVoiceResponse(w http.ResponseWriter, data map[string]any) {
	token := stringValue(data["token"])
	if token == "" {
		writeWebUIError(w, platform.NewUpstreamError("Upstream returned no voice token", 502, ""))
		return
	}
	writeWebUIJSON(w, http.StatusOK, map[string]any{
		"token":            token,
		"url":              stringValueWithDefault(data["livekitUrl"], protocol.LiveKitWSBase),
		"participant_name": firstVoiceString(data, "participantName", "participant_name", "identity"),
		"room_name":        firstVoiceString(data, "roomName", "room_name", "room"),
	})
}

func firstVoiceString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(data[key]); value != "" {
			return value
		}
	}
	return ""
}

func writeWebUIVoiceError(w http.ResponseWriter, err error) {
	var upstream *platform.UpstreamError
	var rateLimit *platform.RateLimitError
	var validation *platform.ValidationError
	if errors.As(err, &upstream) || errors.As(err, &rateLimit) || errors.As(err, &validation) {
		writeWebUIError(w, err)
		return
	}
	writeWebUIError(w, platform.NewUpstreamError("Voice token error: "+err.Error(), 502, ""))
}

func defaultWebUIVoiceFetchToken(ctx context.Context, token string, options webUIVoiceOptions) (map[string]any, error) {
	return transport.FetchLiveKitToken(ctx, token, transport.LiveKitOptions{
		Voice:             options.Voice,
		Personality:       options.Personality,
		Speed:             options.Speed,
		CustomInstruction: options.CustomInstruction,
	})
}
