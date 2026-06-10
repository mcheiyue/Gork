package openai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/platform"
)

type VideoCreateOptions struct {
	Model           string
	Prompt          string
	Seconds         int
	Size            string
	ResolutionName  string
	Preset          string
	InputReferences []map[string]any
}

type videoJobOptions struct {
	Size            string
	ResolutionName  string
	Prompt          string
	Seconds         int
	Preset          string
	InputReferences []map[string]any
}

type normalizedVideoCreate struct {
	Prompt  string
	Seconds int
	Size    string
}

const videoJobTTL = time.Hour

var (
	videoJobsMu  sync.Mutex
	videoJobs    = map[string]*VideoJob{}
	videoNowUnix = func() int64 {
		return time.Now().Unix()
	}
	videoID = func() string {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "video_" + strconv.FormatInt(time.Now().UnixNano(), 16)
		}
		return "video_" + hex.EncodeToString(raw[:])
	}
	videoStartJob = func(ctx context.Context, job *VideoJob, options videoJobOptions) {
		go runVideoJob(ctx, job, options)
	}
	videoScheduleExpiration = func(videoID string) {
		go expireVideoJob(videoID, videoJobTTL)
	}
	videoGenerate = defaultVideoGenerate
)

func CreateVideo(ctx context.Context, options VideoCreateOptions) (map[string]any, error) {
	normalized, err := normalizeVideoCreateOptions(options)
	if err != nil {
		return nil, err
	}
	job := &VideoJob{
		ID:        videoID(),
		Model:     options.Model,
		Prompt:    normalized.Prompt,
		Seconds:   strconvItoa(normalized.Seconds),
		Size:      normalized.Size,
		Quality:   videoQuality,
		CreatedAt: videoNowUnix(),
		Status:    "queued",
	}
	putVideoJob(job)
	videoScheduleExpiration(job.ID)
	videoStartJob(ctx, job, videoJobOptions{
		Size:            normalized.Size,
		ResolutionName:  options.ResolutionName,
		Prompt:          normalized.Prompt,
		Seconds:         normalized.Seconds,
		Preset:          options.Preset,
		InputReferences: options.InputReferences,
	})
	return job.ToDict(), nil
}

func normalizeVideoCreateOptions(options VideoCreateOptions) (normalizedVideoCreate, error) {
	spec, ok := model.Get(options.Model)
	if !ok || !spec.Enabled || !spec.IsVideo() {
		return normalizedVideoCreate{}, platform.NewValidationError("Model '"+options.Model+"' is not a video model", "model", "")
	}
	prompt := strings.TrimSpace(options.Prompt)
	if prompt == "" {
		return normalizedVideoCreate{}, platform.NewValidationError("prompt cannot be empty", "prompt", "")
	}
	seconds := options.Seconds
	if seconds == 0 {
		seconds = 6
	}
	if err := ValidateVideoLength(seconds); err != nil {
		return normalizedVideoCreate{}, err
	}
	size := strings.TrimSpace(options.Size)
	if size == "" {
		size = "720x1280"
	}
	_, defaultResolution, err := resolveVideoSize(size)
	if err != nil {
		return normalizedVideoCreate{}, err
	}
	if _, err := resolveVideoResolutionName(options.ResolutionName, defaultResolution); err != nil {
		return normalizedVideoCreate{}, err
	}
	if _, err := resolveVideoPreset(options.Preset, "custom"); err != nil {
		return normalizedVideoCreate{}, err
	}
	return normalizedVideoCreate{Prompt: prompt, Seconds: seconds, Size: size}, nil
}

func RetrieveVideo(videoID string) (map[string]any, error) {
	job, ok := GetVideoJob(videoID)
	if !ok {
		return nil, platform.NewValidationError("Video '"+videoID+"' not found", "video_id", "")
	}
	return job.ToDict(), nil
}

func VideoContentPath(videoID string) (string, error) {
	job, ok := GetVideoJob(videoID)
	if !ok {
		return "", platform.NewValidationError("Video '"+videoID+"' not found", "video_id", "")
	}
	if job.Status != "completed" || job.ContentPath == "" {
		return "", platform.NewAppError("Video content is not ready yet", platform.ErrorKindValidation, "video_not_ready", 409, nil)
	}
	if _, err := os.Stat(job.ContentPath); err != nil {
		return "", platform.NewValidationError("Video content for '"+videoID+"' not found", "video_id", "")
	}
	return job.ContentPath, nil
}

func GetVideoJob(videoID string) (*VideoJob, bool) {
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	job, ok := videoJobs[videoID]
	return job, ok
}

func putVideoJob(job *VideoJob) {
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	videoJobs[job.ID] = job
}

func clearVideoJobs() {
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	videoJobs = map[string]*VideoJob{}
}

func expireVideoJob(videoID string, ttl time.Duration) {
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	<-timer.C
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	delete(videoJobs, videoID)
}

func setVideoJobStatus(job *VideoJob, status string, progress int) {
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	job.Status = status
	if progress >= 0 {
		if progress > 100 {
			progress = 100
		}
		job.Progress = progress
	}
}

func runVideoJob(ctx context.Context, job *VideoJob, options videoJobOptions) {
	setVideoJobStatus(job, "in_progress", 1)
	artifact, err := videoGenerate(ctx, videoGenerateOptions{
		Model:           job.Model,
		Prompt:          options.Prompt,
		Seconds:         options.Seconds,
		Size:            options.Size,
		ResolutionName:  options.ResolutionName,
		Preset:          options.Preset,
		InputReferences: options.InputReferences,
	})
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	if err != nil {
		job.Status = "failed"
		job.Error = map[string]any{"code": "video_generation_failed", "message": err.Error()}
		return
	}
	job.Status = "completed"
	job.Progress = 100
	job.CompletedAt = videoNowUnix()
	job.VideoURL = artifact.VideoURL
	job.ContentPath = artifact.LocalContentFilePath
	job.RemixedFromVideoID = artifact.RemixedFromVideoID
}

func strconvItoa(value int) string {
	return strconv.FormatInt(int64(value), 10)
}
