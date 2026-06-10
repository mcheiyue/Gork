package transport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

type imagineSlot struct {
	ImageID  string
	Order    int
	Width    int
	Height   int
	LastBlob string
	LastURL  string
	Done     bool
	Progress int
}

type imagineRoundOptions struct {
	Prompt         string
	AspectRatio    string
	EnableNSFW     bool
	EnablePro      bool
	Needed         int
	StreamTimeout  time.Duration
	RoundTimeout   time.Duration
	InterRoundWait time.Duration
	Now            func() time.Time
}

type imagineRoundResult struct {
	Events   []map[string]any
	WSClosed bool
}

func streamImagineRound(ctx context.Context, ws ImagineWebSocketConnection, options imagineRoundOptions) (imagineRoundResult, error) {
	options = normalizeImagineRoundOptions(options)
	if err := sendImagineRoundStart(ctx, ws, options); err != nil {
		return imagineRoundResult{
			Events:   []map[string]any{{"type": "error", "error_code": "send_failed", "error": err.Error()}},
			WSClosed: true,
		}, nil
	}

	result := imagineRoundResult{}
	slots := map[string]*imagineSlot{}
	completed := 0
	roundStart := options.Now()
	for {
		elapsed := options.Now().Sub(roundStart)
		if elapsed >= options.RoundTimeout {
			result.Events = append(result.Events, imagineTimeoutEvents(slots)...)
			result.WSClosed = false
			return result, nil
		}

		msg, err := ws.Receive(ctx, minImagineDuration(options.StreamTimeout, options.RoundTimeout-elapsed))
		if err != nil {
			if imagineIsReceiveTimeout(err) {
				if len(slots) > 0 && imagineAllSlotsDone(slots) {
					closed, err := probeImagineClosed(ctx, ws, options.InterRoundWait)
					result.WSClosed = closed
					return result, err
				}
				continue
			}
			return result, err
		}

		switch msg.Type {
		case ImagineWebSocketTextMessage:
			stop, err := handleImagineTextMessage(ctx, ws, msg.Data, options, slots, &completed, &result)
			if err != nil || stop {
				return result, err
			}
		case ImagineWebSocketClosedMessage, ImagineWebSocketErrorMessage:
			result.Events = append(result.Events, imagineBestEffortFinals(slots)...)
			result.WSClosed = true
			return result, nil
		}
	}
}

func sendImagineRoundStart(ctx context.Context, ws ImagineWebSocketConnection, options imagineRoundOptions) error {
	if err := ws.SendJSON(ctx, protocol.BuildImagineResetMessage(protocol.ImagineMessageOptions{})); err != nil {
		return err
	}
	enableNSFW := options.EnableNSFW
	return ws.SendJSON(ctx, protocol.BuildImagineRequestMessage(
		newImagineRequestID(),
		options.Prompt,
		protocol.ImagineMessageOptions{
			AspectRatio: options.AspectRatio,
			EnableNSFW:  &enableNSFW,
			EnablePro:   options.EnablePro,
		},
	))
}

func handleImagineTextMessage(ctx context.Context, ws ImagineWebSocketConnection, data string, options imagineRoundOptions, slots map[string]*imagineSlot, completed *int, result *imagineRoundResult) (bool, error) {
	msg := map[string]any{}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return false, nil
	}
	switch imagineString(msg["type"]) {
	case "json":
		return handleImagineJSONFrame(ctx, ws, msg, options, slots, completed, result)
	case "image":
		handleImagineImageFrame(msg, slots, result)
	case "error":
		code := imagineString(msg["err_code"])
		if code == "" {
			code = "upstream_error"
		}
		message := imagineString(msg["err_msg"])
		if message == "" {
			message = data
		}
		result.Events = append(result.Events, map[string]any{"type": "error", "error_code": code, "error": message})
		result.WSClosed = true
		return true, nil
	}
	return false, nil
}

func handleImagineJSONFrame(ctx context.Context, ws ImagineWebSocketConnection, msg map[string]any, options imagineRoundOptions, slots map[string]*imagineSlot, completed *int, result *imagineRoundResult) (bool, error) {
	parsed := protocol.ParseImagineJSONFrame(msg)
	if parsed == nil {
		return false, nil
	}
	imageID := imagineString(parsed["image_id"])
	switch imagineString(parsed["status"]) {
	case "start_stage":
		slot := &imagineSlot{
			ImageID: imageID,
			Order:   imagineInt(parsed["order"]),
			Width:   imagineInt(parsed["width"]),
			Height:  imagineInt(parsed["height"]),
		}
		slots[imageID] = slot
		result.Events = append(result.Events, map[string]any{
			"type": "progress", "image_id": imageID, "order": slot.Order, "progress": 10,
		})
	case "completed":
		slot := slots[imageID]
		if slot == nil || slot.Done {
			return false, nil
		}
		slot.Done = true
		if imagineBool(parsed["moderated"]) {
			result.Events = append(result.Events, map[string]any{"type": "moderated", "image_id": imageID, "order": slot.Order})
		} else {
			result.Events = append(result.Events, imagineFinalEvent(slot, imagineBool(parsed["r_rated"])))
			*completed = *completed + 1
		}
		if len(slots) > 0 && imagineAllSlotsDone(slots) {
			closed, err := probeImagineClosed(ctx, ws, options.InterRoundWait)
			result.WSClosed = closed
			return true, err
		}
		if *completed >= options.Needed {
			result.WSClosed = false
			return true, nil
		}
	}
	return false, nil
}

func handleImagineImageFrame(msg map[string]any, slots map[string]*imagineSlot, result *imagineRoundResult) {
	url := imagineString(msg["url"])
	imageID, _ := protocol.ParseImagineImageURL(url)
	slot := slots[imageID]
	if slot == nil || slot.Done {
		return
	}
	slot.LastBlob = imagineString(msg["blob"])
	slot.LastURL = url
	progress := imagineProgress(msg["percentage_complete"])
	if progress > slot.Progress {
		slot.Progress = progress
		result.Events = append(result.Events, map[string]any{
			"type": "progress", "image_id": imageID, "order": slot.Order, "progress": progress,
		})
	}
}

func imagineFinalEvent(slot *imagineSlot, rRated bool) map[string]any {
	return map[string]any{
		"type": "image", "image_id": slot.ImageID, "order": slot.Order,
		"stage": "final", "blob": slot.LastBlob, "url": slot.LastURL,
		"width": slot.Width, "height": slot.Height, "is_final": true,
		"moderated": false, "r_rated": rRated,
	}
}

func imagineTimeoutEvents(slots map[string]*imagineSlot) []map[string]any {
	events := []map[string]any{}
	for _, slot := range slots {
		if slot.Done {
			continue
		}
		if slot.LastBlob != "" {
			events = append(events, imagineFinalEvent(slot, false))
			continue
		}
		events = append(events, map[string]any{
			"type": "error", "error_code": "slot_incomplete",
			"error": "slot " + imaginePrefix(slot.ImageID, 8) + " timed out",
		})
	}
	return events
}

func imagineBestEffortFinals(slots map[string]*imagineSlot) []map[string]any {
	events := []map[string]any{}
	for _, slot := range slots {
		if !slot.Done && slot.LastBlob != "" {
			events = append(events, imagineFinalEvent(slot, false))
		}
	}
	return events
}
