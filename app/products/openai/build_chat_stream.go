package openai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
)

// sessionSeedFromMessages 用末条用户文本作 prompt cache 会话种子（无显式键时）。
func sessionSeedFromMessages(msgs []build.ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(msgs[i].Role), "user") {
			if s := strings.TrimSpace(msgs[i].Content); s != "" {
				if len(s) > 256 {
					return s[:256]
				}
				return s
			}
		}
	}
	return ""
}

// runBuildCompletion 在已选定账号列表上尝试推理（非流/流式共用选号循环）。
func runBuildCompletion(
	ctx context.Context,
	options chatCompletionOptions,
	upstream string,
	stream bool,
	accounts []buildaccount.Account,
	dir buildAccountDirectory,
	client buildHTTPClient,
	oauth buildTokenRefresher,
) (chatCompletionResult, error) {
	msgs := build.ExtractChatMessages(options.Messages)
	cacheKey := build.ResolvePromptCacheKey(
		build.PromptCacheKeyFromOverrides(options.RequestOverrides),
		sessionSeedFromMessages(msgs),
		upstream,
	)
	body, err := build.BuildResponsesBodyOpts(build.ResponsesBodyOptions{
		Model:          upstream,
		Messages:       msgs,
		Stream:         stream,
		Tools:          options.Tools,
		ToolChoice:     options.ToolChoice,
		PromptCacheKey: cacheKey,
		ResponseFormat: options.ResponseFormat,
	})
	if err != nil {
		return chatCompletionResult{}, platform.NewUpstreamError(err.Error(), 400, "")
	}

	var lastErr error
	for _, acc := range accounts {
		access, err := ensureBuildAccessToken(ctx, dir, oauth, acc)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := invokeBuildOnce(ctx, options.Model, upstream, stream, body, acc, access, dir, client, oauth)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return chatCompletionResult{}, lastErr
	}
	return chatCompletionResult{}, platform.NewRateLimitError("No available Build accounts")
}

func invokeBuildOnce(
	ctx context.Context,
	modelName, upstream string,
	stream bool,
	body []byte,
	acc buildaccount.Account,
	access string,
	dir buildAccountDirectory,
	client buildHTTPClient,
	oauth buildTokenRefresher,
) (chatCompletionResult, error) {
	resp, err := client.CreateResponse(ctx, build.RequestMeta{
		AccessToken: access,
		UserID:      acc.UserID,
		Model:       upstream,
		Stream:      stream,
	}, bytes.NewReader(body))
	if err != nil {
		return chatCompletionResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && acc.RefreshToken != "" {
		if tok, rerr := oauth.Refresh(ctx, acc.RefreshToken); rerr == nil {
			_ = dir.UpdateTokens(ctx, acc.ID, tok.AccessToken, firstNonEmptyStr(tok.RefreshToken, acc.RefreshToken), tok.ExpiresAt)
			resp2, err2 := client.CreateResponse(ctx, build.RequestMeta{
				AccessToken: tok.AccessToken,
				UserID:      acc.UserID,
				Model:       upstream,
				Stream:      stream,
			}, bytes.NewReader(body))
			if err2 != nil {
				return chatCompletionResult{}, err2
			}
			defer resp2.Body.Close()
			return readBuildResponse(modelName, stream, resp2)
		} else if build.IsPermanentRefresh(rerr) {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusExpired, "refresh permanent failure")
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		upErr := &build.UpstreamError{Status: resp.StatusCode, Body: string(raw), Op: "create_response"}
		if build.IsRateLimited(upErr) {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusCooling, fmt.Sprintf("upstream %d", resp.StatusCode))
		}
		return chatCompletionResult{}, upErr
	}
	return readBuildResponse(modelName, stream, resp)
}

func readBuildResponse(modelName string, stream bool, resp *http.Response) (chatCompletionResult, error) {
	if stream {
		frames, err := build.ChatStreamFramesFromResponsesSSE(modelName, chatResponseID(), resp.Body)
		if err != nil {
			return chatCompletionResult{}, err
		}
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return chatCompletionResult{}, err
	}
	return finishBuildChat(modelName, raw)
}
