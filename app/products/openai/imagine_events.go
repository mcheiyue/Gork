package openai

import (
	"context"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
)

type ImagineEventOptions struct {
	AspectRatio string
	Count       int
	EnableNSFW  *bool
	EnablePro   bool
}

func StreamImagineEvents(ctx context.Context, prompt string, options ImagineEventOptions) ([]map[string]any, bool, error) {
	spec, ok := model.Get("grok-imagine-image")
	if !ok {
		return nil, false, nil
	}
	directory := chatDirectoryProvider()
	if directory == nil {
		return nil, false, nil
	}
	account, ok, err := directory.ReserveChatAccount(ctx, spec, nil)
	if err != nil || !ok {
		return nil, ok, err
	}
	defer func() { _ = directory.ReleaseChatAccount(ctx, account) }()

	events, err := imageStreamImages(ctx, account.Token, prompt, transport.ImagineOptions{
		AspectRatio: options.AspectRatio,
		N:           options.Count,
		EnableNSFW:  options.EnableNSFW,
		EnablePro:   options.EnablePro,
	})
	return events, true, err
}
