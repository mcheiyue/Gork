package anthropic

import (
	"context"
	"errors"
	"strings"

	controlaccount "github.com/jiujiu532/grok2api/app/control/account"
	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func Messages(ctx context.Context, options MessagesOptions) (MessagesResult, error) {
	plan, err := prepareMessages(options)
	if err != nil {
		return MessagesResult{}, err
	}
	if plan.Spec.IsConsoleChat() {
		return messagesConsole(ctx, options, plan)
	}
	directory := messagesDirectoryProvider()
	if directory == nil {
		return MessagesResult{}, platform.NewRateLimitError("Account directory not initialised")
	}
	return runMessagesWithRetries(ctx, options, plan, directory)
}

func prepareMessages(options MessagesOptions) (messagesPlan, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return messagesPlan{}, err
	}
	internal := parseAnthropicMessages(options.Messages, options.System)
	message, files := extractAnthropicMessage(internal)
	if strings.TrimSpace(message) == "" {
		return messagesPlan{}, platform.NewUpstreamError("Empty message after extraction", 400, "")
	}
	return buildMessagesPlan(options, spec, internal, message, files), nil
}

func buildMessagesPlan(options MessagesOptions, spec model.ModelSpec, internal []map[string]any, message string, files []string) messagesPlan {
	toolNames := []string{}
	if len(options.Tools) > 0 {
		chatTools := convertAnthropicTools(options.Tools)
		toolNames = protocol.ExtractToolNames(chatTools)
		toolPrompt := protocol.BuildToolSystemPrompt(chatTools, convertAnthropicToolChoice(options.ToolChoice))
		message = protocol.InjectIntoMessage(message, toolPrompt)
	}
	messageID := options.MessageID
	if messageID == "" {
		messageID = makeAnthropicMessageID()
	}
	return messagesPlan{Spec: spec, IsStream: options.Stream, EmitThink: options.EmitThink, Internal: internal,
		Message: message, Files: files, ToolNames: toolNames, MessageID: messageID,
		MaxRetries: messagesMaxRetries(), RetryCodes: messagesRetryCodes(), TimeoutSeconds: messagesTimeoutSeconds()}
}

func messagesConsole(ctx context.Context, options MessagesOptions, plan messagesPlan) (MessagesResult, error) {
	result, err := ConsoleMessages(ctx, ConsoleMessagesOptions{
		Model: options.Model, Messages: plan.Internal, Stream: options.Stream, EmitThink: options.EmitThink,
		Temperature: options.Temperature, TopP: options.TopP, MessageID: plan.MessageID,
	})
	if err != nil {
		return MessagesResult{}, err
	}
	return MessagesResult(result), nil
}

func runMessagesWithRetries(ctx context.Context, options MessagesOptions, plan messagesPlan, directory messagesDirectory) (MessagesResult, error) {
	excluded := []string{}
	for attempt := 0; attempt <= plan.MaxRetries; attempt++ {
		account, ok, err := directory.ReserveMessagesAccount(ctx, plan.Spec, excluded)
		if err != nil {
			return MessagesResult{}, err
		}
		if !ok {
			return MessagesResult{}, platform.NewRateLimitError("No available accounts for this model tier")
		}
		result, retry, err := runMessagesAttempt(ctx, options, plan, account, directory)
		if err == nil {
			return result, nil
		}
		if retry && attempt < plan.MaxRetries {
			excluded = append(excluded, account.Token)
			continue
		}
		return MessagesResult{}, err
	}
	return MessagesResult{}, platform.NewRateLimitError("No available accounts after retries")
}

func runMessagesAttempt(ctx context.Context, options MessagesOptions, plan messagesPlan, account messagesAccount, directory messagesDirectory) (MessagesResult, bool, error) {
	success := false
	var failErr error
	defer func() {
		_ = directory.ReleaseMessagesAccount(ctx, account)
		kind := messagesFeedbackForError(failErr)
		if success {
			kind = messagesFeedbackSuccess
		}
		_ = directory.FeedbackMessagesAccount(ctx, messagesFeedback{Token: account.Token, Kind: kind, ModeID: account.ModeID})
		if success {
			messagesQuotaSync(ctx, account.Token, int(account.ModeID))
		} else {
			messagesFailSync(ctx, account.Token, int(account.ModeID), failErr)
		}
	}()
	result, err := messagesFromStream(ctx, options, plan, account)
	if err != nil {
		failErr = err
		return MessagesResult{}, shouldRetryMessages(err, plan.RetryCodes), err
	}
	success = true
	return result, false, nil
}

func shouldRetryMessages(err error, retryCodes map[int]struct{}) bool {
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream == nil {
		return false
	}
	_, ok := retryCodes[upstream.Status]
	return ok
}

func messagesFeedbackForError(err error) messagesFeedbackKind {
	return messagesFeedbackKind(controlaccount.FeedbackKindForError(err))
}
