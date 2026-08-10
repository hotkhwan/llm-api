package mission

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxProviderReferenceBytes = 16 << 20
	maxProviderVideoBytes     = 512 << 20
	providerFrameCount        = 4
	maxShotRevisions          = 2
)

// CloudVideoPipeline owns one durable First Post job. It always sends the
// original product image to the provider, verifies each shot independently,
// and only assembles clips that pass both product fidelity and cinematography.
type CloudVideoPipeline struct {
	Jobs      VideoGenerationJobStore
	Objects   ObjectStore
	Reader    ObjectReader
	Providers map[string]VideoProvider
	Fidelity  ProductFidelityAnalyzer
	Cinematic VisualQCAnalyzer
	Reviser   ShotRevisionPlanner
	Runner    CommandRunner
	Now       func() time.Time
	WorkerID  string
	TempRoot  string
	Timeout   time.Duration
}

func (q *CloudVideoPipeline) EnqueueVideo(ctx context.Context, request VideoGenerationRequest) (ProcessingJob, error) {
	if q.Providers[request.Provider] == nil {
		return ProcessingJob{}, fmt.Errorf("video provider %q is not configured", request.Provider)
	}
	return q.Jobs.EnqueueVideoJob(ctx, request)
}

func (q *CloudVideoPipeline) GetVideo(ctx context.Context, id string) (ProcessingJob, *VideoGenerationResult, error) {
	return q.Jobs.GetVideoJob(ctx, id)
}

func (q *CloudVideoPipeline) Run(ctx context.Context) {
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	for {
		if !q.runOne(ctx) {
			select {
			case <-ctx.Done():
				return
			case <-poll.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (q *CloudVideoPipeline) runOne(ctx context.Context) bool {
	timeout := q.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Minute
	}
	now := q.Now().UTC()
	job, request, claimed, err := q.Jobs.ClaimNextVideo(ctx, q.WorkerID, now, now.Add(timeout))
	if err != nil || !claimed {
		return false
	}
	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, previous, _ := q.Jobs.GetVideoJob(jobCtx, job.ID)
	result := VideoGenerationResult{Provider: request.Provider, OutputKey: request.OutputKey}
	if previous != nil {
		result = *previous
	}
	if err := q.process(jobCtx, job.ID, request, &result); err != nil {
		_ = q.Jobs.FailVideo(context.WithoutCancel(ctx), job.ID, q.WorkerID, result, boundedError(err), q.Now().UTC())
		return true
	}
	_, _ = q.Jobs.CompleteVideo(context.WithoutCancel(ctx), job.ID, q.WorkerID, result, q.Now().UTC())
	return true
}

func (q *CloudVideoPipeline) process(ctx context.Context, jobID string, request VideoGenerationRequest, result *VideoGenerationResult) error {
	if request.Provider == "wan" {
		return q.processLocalPreview(ctx, jobID, request, result)
	}
	if q.Fidelity == nil || q.Cinematic == nil || q.Reviser == nil {
		return fmt.Errorf("product fidelity, ShotVL and Qwen shot revision must all be configured")
	}
	provider := q.Providers[request.Provider]
	if provider == nil || provider.Name() != request.Provider {
		return fmt.Errorf("video provider is unavailable")
	}
	reference, err := readObjectBounded(ctx, q.Reader, request.Reference.StorageKey, maxProviderReferenceBytes)
	if err != nil {
		return fmt.Errorf("read product reference: %w", err)
	}
	root, err := os.MkdirTemp(q.TempRoot, "kwanni-provider-video-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	assets := make([]Asset, 0, len(request.Draft.ProductionSpec.Shots))
	for _, shot := range request.Draft.ProductionSpec.Shots {
		existing := generatedShot(result.Shots, shot.ShotID)
		if existing != nil && existing.State == GenerationSucceeded && existing.StorageKey != "" {
			assets = append(assets, Asset{Shot: shot.CaptureShot, StorageKey: existing.StorageKey, ContentType: existing.ContentType, Bytes: existing.Bytes, SHA256: existing.SHA256})
			continue
		}
		completed, metadata, err := q.generateVerifiedShot(ctx, jobID, request, provider, reference, shot, result, root)
		if err != nil {
			return err
		}
		assets = append(assets, Asset{Shot: shot.CaptureShot, StorageKey: completed.StorageKey, ContentType: metadata.ContentType, Bytes: metadata.Bytes, SHA256: metadata.SHA256})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Shot < assets[j].Shot })
	if len(assets) != 3 {
		return fmt.Errorf("provider pipeline requires exactly three verified shots")
	}
	setAllShotState(result, GenerationAssembling, q.Now().UTC())
	_ = q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
	exporter := FFmpegExportQueue{Objects: q.Objects, Reader: q.Reader, Runner: q.Runner, Now: q.Now, TempRoot: q.TempRoot}
	if err := exporter.render(ctx, ExportRequest{MissionID: request.MissionID, Assets: assets, Draft: request.Draft, OutputKey: request.OutputKey}); err != nil {
		return fmt.Errorf("assemble verified shots: %w", err)
	}
	setAllShotState(result, GenerationSucceeded, q.Now().UTC())
	return q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
}

// processLocalPreview renders one five-second portrait clip containing the
// three production beats. It deliberately skips paid fidelity services and
// leaves product/cinematic review to the asynchronous advisory QC flow.
func (q *CloudVideoPipeline) processLocalPreview(ctx context.Context, jobID string, request VideoGenerationRequest, result *VideoGenerationResult) error {
	provider := q.Providers[request.Provider]
	if provider == nil || provider.Name() != "wan" {
		return fmt.Errorf("Wan local preview runtime is unavailable")
	}
	reference, err := readObjectBounded(ctx, q.Reader, request.Reference.StorageKey, maxProviderReferenceBytes)
	if err != nil {
		return fmt.Errorf("read product reference: %w", err)
	}
	shot := GeneratedShot{ShotID: "local-preview", Revision: 1, State: GenerationRendering, UpdatedAt: q.Now().UTC()}
	upsertGeneratedShot(result, shot)
	_ = q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
	video, err := provider.Generate(ctx, ProviderVideoRequest{
		MissionID:     request.MissionID,
		Shot:          ProductionShot{ShotID: "local-preview"},
		Revision:      1,
		Prompt:        wanPreviewPrompt(request.Draft.ProductionSpec),
		Reference:     reference,
		ReferenceType: request.Reference.ContentType,
	})
	if err != nil {
		return fmt.Errorf("Wan local preview: %w", err)
	}
	defer video.Body.Close()
	metadata, err := q.Objects.Put(ctx, request.OutputKey, "video/mp4", io.LimitReader(video.Body, maxProviderVideoBytes+1))
	if err != nil || metadata.Bytes <= 0 || metadata.Bytes > maxProviderVideoBytes {
		return fmt.Errorf("store Wan local preview: %w", err)
	}
	shot.ProviderTask = video.TaskID
	shot.StorageKey = request.OutputKey
	shot.ContentType = metadata.ContentType
	shot.Bytes = metadata.Bytes
	shot.SHA256 = metadata.SHA256
	shot.State = GenerationSucceeded
	shot.UpdatedAt = q.Now().UTC()
	upsertGeneratedShot(result, shot)
	return q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
}

func wanPreviewPrompt(spec ProductionSpec) string {
	product, _ := json.Marshal(spec.Continuity.Product)
	shots, _ := json.Marshal(spec.Shots)
	return fmt.Sprintf("Create one 5-second portrait 9:16 local product preview from the attached reference image. Compress the following three story beats into one fast sequence with hard visual emphasis and no text overlays: %s. The product is an immutable hero asset: preserve exact colors, silhouette, proportions, component count and layout, material, logo, label and every visible character from the reference. Never redesign, replace, multiply, crop away, or morph the product. Product bible: %s.", shots, product)
}

// WanLocalProvider talks only to the private, project-owned Wan runtime. The
// durable Mongo job allows clients to leave the page and resume later.
type WanLocalProvider struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

func (v WanLocalProvider) Name() string { return "wan" }

func (v WanLocalProvider) Generate(ctx context.Context, request ProviderVideoRequest) (ProviderVideo, error) {
	payload := map[string]any{
		"prompt":          request.Prompt,
		"referenceImage":  "data:" + request.ReferenceType + ";base64," + base64.StdEncoding.EncodeToString(request.Reference),
		"durationSeconds": 5,
		"width":           704,
		"height":          1280,
		"frameCount":      121,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ProviderVideo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(v.Endpoint, "/")+"/generate", bytes.NewReader(body))
	if err != nil {
		return ProviderVideo{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.APIKey)
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return ProviderVideo{}, err
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return ProviderVideo{}, fmt.Errorf("runtime returned %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]); contentType != "video/mp4" {
		response.Body.Close()
		return ProviderVideo{}, fmt.Errorf("runtime returned unsupported content type %q", contentType)
	}
	return ProviderVideo{TaskID: response.Header.Get("X-Kwanni-Task-ID"), ContentType: "video/mp4", Body: response.Body}, nil
}

func (q *CloudVideoPipeline) generateVerifiedShot(ctx context.Context, jobID string, request VideoGenerationRequest, provider VideoProvider, reference []byte, shot ProductionShot, result *VideoGenerationResult, root string) (GeneratedShot, ObjectMetadata, error) {
	prompt := providerShotPrompt(request.Draft.ProductionSpec, shot, request.Provider)
	var last GeneratedShot
	for revision := 1; revision <= maxShotRevisions; revision++ {
		last = GeneratedShot{ShotID: shot.ShotID, Revision: revision, State: GenerationRendering, UpdatedAt: q.Now().UTC()}
		upsertGeneratedShot(result, last)
		_ = q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
		referenceURL := ""
		if signer, ok := q.Objects.(ObjectURLSigner); ok {
			referenceURL, _ = signer.DownloadURL(ctx, request.Reference.StorageKey, time.Hour)
		}
		video, err := provider.Generate(ctx, ProviderVideoRequest{MissionID: request.MissionID, Shot: shot, Revision: revision, Prompt: prompt, Reference: reference, ReferenceType: request.Reference.ContentType, ReferenceURL: referenceURL})
		if err != nil {
			return last, ObjectMetadata{}, fmt.Errorf("%s generate %s revision %d: %w", provider.Name(), shot.ShotID, revision, err)
		}
		last.ProviderTask = video.TaskID
		key := fmt.Sprintf("missions/%s/provider/%s/%s/revision-%d.mp4", request.MissionID, provider.Name(), shot.ShotID, revision)
		metadata, err := q.Objects.Put(ctx, key, "video/mp4", io.LimitReader(video.Body, maxProviderVideoBytes+1))
		_ = video.Body.Close()
		if err != nil || metadata.Bytes <= 0 || metadata.Bytes > maxProviderVideoBytes {
			return last, ObjectMetadata{}, fmt.Errorf("store provider shot: %w", err)
		}
		last.StorageKey = key
		last.ContentType = metadata.ContentType
		last.Bytes = metadata.Bytes
		last.SHA256 = metadata.SHA256
		last.State = GenerationVerifying
		last.UpdatedAt = q.Now().UTC()
		upsertGeneratedShot(result, last)
		_ = q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
		frames, err := q.extractFrames(ctx, key, shot, revision, request.MissionID, root)
		if err != nil {
			return last, ObjectMetadata{}, err
		}
		fidelity, err := q.Fidelity.AnalyzeProductFidelity(ctx, request.Reference, reference, shot, frames)
		if err != nil {
			return last, ObjectMetadata{}, fmt.Errorf("product fidelity: %w", err)
		}
		spec := request.Draft.ProductionSpec
		spec.Shots = []ProductionShot{shot}
		cinematic, err := q.Cinematic.Analyze(ctx, spec, frames, revision)
		if err != nil {
			return last, ObjectMetadata{}, fmt.Errorf("ShotVL: %w", err)
		}
		last.Fidelity, last.Cinematic = &fidelity, &cinematic
		last.Defects = append(append([]VisualQCDefect(nil), fidelity.Defects...), cinematic.Shots[0].Defects...)
		if fidelity.Passed && cinematic.Passed {
			last.State = GenerationSucceeded
			last.UpdatedAt = q.Now().UTC()
			upsertGeneratedShot(result, last)
			_ = q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
			return last, metadata, nil
		}
		if revision == maxShotRevisions {
			last.State = GenerationFailed
			last.UpdatedAt = q.Now().UTC()
			upsertGeneratedShot(result, last)
			return last, ObjectMetadata{}, fmt.Errorf("%s failed product fidelity or cinematic quality after %d revisions", shot.ShotID, revision)
		}
		last.State = GenerationRevising
		last.UpdatedAt = q.Now().UTC()
		upsertGeneratedShot(result, last)
		_ = q.Jobs.UpdateVideoProgress(ctx, jobID, *result, q.Now().UTC())
		prompt, err = q.Reviser.ReviseShot(ctx, request.Draft.ProductionSpec, shot, revision+1, last.Defects)
		if err != nil {
			return last, ObjectMetadata{}, fmt.Errorf("Qwen revise %s: %w", shot.ShotID, err)
		}
	}
	return last, ObjectMetadata{}, fmt.Errorf("shot generation exhausted")
}

func (q *CloudVideoPipeline) extractFrames(ctx context.Context, key string, shot ProductionShot, revision int, missionID, root string) ([]VisualQCFrame, error) {
	videoPath := filepath.Join(root, fmt.Sprintf("%s-r%d.mp4", shot.ShotID, revision))
	if err := downloadObject(ctx, q.Reader, key, videoPath, maxProviderVideoBytes); err != nil {
		return nil, err
	}
	pattern := filepath.Join(root, fmt.Sprintf("%s-r%d-frame-%%02d.jpg", shot.ShotID, revision))
	if _, err := q.Runner.Run(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", videoPath, "-vf", fmt.Sprintf("fps=%d/8,scale=768:-2", providerFrameCount), "-frames:v", fmt.Sprint(providerFrameCount), "-q:v", "3", pattern); err != nil {
		return nil, err
	}
	paths, _ := filepath.Glob(filepath.Join(root, fmt.Sprintf("%s-r%d-frame-*.jpg", shot.ShotID, revision)))
	sort.Strings(paths)
	if len(paths) != providerFrameCount {
		return nil, fmt.Errorf("expected %d shot frames, got %d", providerFrameCount, len(paths))
	}
	frames := make([]VisualQCFrame, 0, len(paths))
	for index, value := range paths {
		jpeg, err := os.ReadFile(value)
		if err != nil || len(jpeg) == 0 || len(jpeg) > maxVisualFrameBytes {
			return nil, fmt.Errorf("invalid provider evidence frame")
		}
		id := fmt.Sprintf("%s-r%d-frame%02d", shot.ShotID, revision, index)
		storageKey := fmt.Sprintf("missions/%s/product-fidelity/%s/r%d/%s.jpg", missionID, shot.ShotID, revision, id)
		if _, err := q.Objects.Put(ctx, storageKey, "image/jpeg", bytes.NewReader(jpeg)); err != nil {
			return nil, err
		}
		frames = append(frames, VisualQCFrame{Evidence: EvidenceFrame{ID: id, StorageKey: storageKey, TimestampMS: index * 2000}, JPEG: jpeg})
	}
	return frames, nil
}

func providerShotPrompt(spec ProductionSpec, shot ProductionShot, provider string) string {
	product, _ := json.Marshal(spec.Continuity.Product)
	shotJSON, _ := json.Marshal(shot)
	return fmt.Sprintf("Generate only %s as an 8-second portrait 9:16 product video clip. Use the attached original product image as an immutable asset reference. Preserve exact color, silhouette, proportions, component count and layout, material, logo, label and every visible character. Never redesign or substitute the product. Shot specification: %s. Verified product bible: %s. No subtitles, overlays, invented claims, price or promotion. Provider=%s.", shot.ShotID, shotJSON, product, provider)
}

func generatedShot(shots []GeneratedShot, id string) *GeneratedShot {
	for index := range shots {
		if shots[index].ShotID == id {
			return &shots[index]
		}
	}
	return nil
}

func upsertGeneratedShot(result *VideoGenerationResult, shot GeneratedShot) {
	for index := range result.Shots {
		if result.Shots[index].ShotID == shot.ShotID {
			result.Shots[index] = shot
			return
		}
	}
	result.Shots = append(result.Shots, shot)
}

func setAllShotState(result *VideoGenerationResult, state GenerationState, at time.Time) {
	for index := range result.Shots {
		result.Shots[index].State = state
		result.Shots[index].UpdatedAt = at
	}
}

func readObjectBounded(ctx context.Context, reader ObjectReader, key string, limit int64) ([]byte, error) {
	input, err := reader.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	value, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil || len(value) == 0 || int64(len(value)) > limit {
		return nil, fmt.Errorf("object is empty or exceeds %d bytes", limit)
	}
	return value, nil
}

func downloadObject(ctx context.Context, reader ObjectReader, key, target string, limit int64) error {
	input, err := reader.Get(ctx, key)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || n == 0 || n > limit {
		return fmt.Errorf("downloaded object is empty or exceeds limit")
	}
	return nil
}

// GeminiVeoProvider follows the current Gemini Veo long-running REST
// contract. The API key is only attached as a header and never enters jobs.
type GeminiVeoProvider struct {
	BaseURL      string
	Model        string
	APIKey       string
	Client       *http.Client
	PollInterval time.Duration
}

func (v GeminiVeoProvider) Name() string { return "veo" }

func (v GeminiVeoProvider) Generate(ctx context.Context, request ProviderVideoRequest) (ProviderVideo, error) {
	base := strings.TrimRight(v.BaseURL, "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	model := strings.TrimSpace(v.Model)
	if model == "" {
		model = "veo-3.1-fast-generate-preview"
	}
	if v.APIKey == "" {
		return ProviderVideo{}, fmt.Errorf("Veo API key is required")
	}
	payload := map[string]any{"instances": []any{map[string]any{"prompt": request.Prompt, "referenceImages": []any{map[string]any{"image": map[string]any{"inlineData": map[string]string{"mimeType": request.ReferenceType, "data": base64.StdEncoding.EncodeToString(request.Reference)}}, "referenceType": "asset"}}}}, "parameters": map[string]any{"aspectRatio": "9:16", "resolution": "1080p", "durationSeconds": 8, "sampleCount": 1, "personGeneration": "allow_adult"}}
	var operation struct {
		Name string `json:"name"`
	}
	if err := v.doJSON(ctx, http.MethodPost, base+"/models/"+url.PathEscape(model)+":predictLongRunning", payload, &operation); err != nil {
		return ProviderVideo{}, err
	}
	if operation.Name == "" {
		return ProviderVideo{}, fmt.Errorf("Veo returned no operation name")
	}
	interval := v.PollInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	for {
		var status struct {
			Done  bool `json:"done"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
			Response struct {
				GenerateVideoResponse struct {
					GeneratedSamples []struct {
						Video struct {
							URI string `json:"uri"`
						} `json:"video"`
					} `json:"generatedSamples"`
				} `json:"generateVideoResponse"`
			} `json:"response"`
		}
		if err := v.doJSON(ctx, http.MethodGet, base+"/"+strings.TrimLeft(operation.Name, "/"), nil, &status); err != nil {
			return ProviderVideo{}, err
		}
		if status.Done {
			if status.Error != nil {
				return ProviderVideo{}, fmt.Errorf("Veo operation failed: %s", status.Error.Message)
			}
			if len(status.Response.GenerateVideoResponse.GeneratedSamples) == 0 || status.Response.GenerateVideoResponse.GeneratedSamples[0].Video.URI == "" {
				return ProviderVideo{}, fmt.Errorf("Veo operation returned no video")
			}
			body, err := v.download(ctx, status.Response.GenerateVideoResponse.GeneratedSamples[0].Video.URI)
			return ProviderVideo{TaskID: operation.Name, ContentType: "video/mp4", Body: body}, err
		}
		select {
		case <-ctx.Done():
			return ProviderVideo{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (v GeminiVeoProvider) doJSON(ctx context.Context, method, endpoint string, payload any, output any) error {
	var body io.Reader
	if payload != nil {
		encoded, _ := json.Marshal(payload)
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-goog-api-key", v.APIKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Veo status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(output)
}

func (v GeminiVeoProvider) download(ctx context.Context, endpoint string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-goog-api-key", v.APIKey)
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("Veo download status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// ArkSeedanceProvider follows Volcano Engine Ark's content-generation task
// API. Seedance receives the original product as a data URL reference image.
type ArkSeedanceProvider struct {
	BaseURL      string
	Model        string
	APIKey       string
	Client       *http.Client
	PollInterval time.Duration
}

func (v ArkSeedanceProvider) Name() string { return "seedance" }

func (v ArkSeedanceProvider) Generate(ctx context.Context, request ProviderVideoRequest) (ProviderVideo, error) {
	base := strings.TrimRight(v.BaseURL, "/")
	if base == "" {
		base = "https://ark.cn-beijing.volces.com/api/v3"
	}
	if v.APIKey == "" || strings.TrimSpace(v.Model) == "" {
		return ProviderVideo{}, fmt.Errorf("Seedance API key and model are required")
	}
	referenceURL := strings.TrimSpace(request.ReferenceURL)
	if referenceURL == "" {
		referenceURL = "data:" + request.ReferenceType + ";base64," + base64.StdEncoding.EncodeToString(request.Reference)
	}
	payload := map[string]any{"model": v.Model, "content": []any{
		map[string]any{"type": "text", "text": request.Prompt + " --ratio 9:16 --dur 8"},
		map[string]any{"type": "image_url", "image_url": map[string]string{"url": referenceURL}, "role": "reference_image"},
	}}
	var created map[string]any
	if err := v.doJSON(ctx, http.MethodPost, base+"/contents/generations/tasks", payload, &created); err != nil {
		return ProviderVideo{}, err
	}
	taskID := stringValue(created, "id")
	if taskID == "" {
		return ProviderVideo{}, fmt.Errorf("Seedance returned no task id")
	}
	interval := v.PollInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	for {
		var status map[string]any
		if err := v.doJSON(ctx, http.MethodGet, base+"/contents/generations/tasks/"+url.PathEscape(taskID), nil, &status); err != nil {
			return ProviderVideo{}, err
		}
		state := stringValue(status, "status")
		switch state {
		case "succeeded":
			videoURL := nestedString(status, "content", "video_url")
			if videoURL == "" {
				videoURL = nestedString(status, "output", "video_url")
			}
			if videoURL == "" {
				return ProviderVideo{}, fmt.Errorf("Seedance returned no video URL")
			}
			body, err := v.download(ctx, videoURL)
			return ProviderVideo{TaskID: taskID, ContentType: "video/mp4", Body: body}, err
		case "failed", "cancelled":
			return ProviderVideo{}, fmt.Errorf("Seedance task %s", state)
		}
		select {
		case <-ctx.Done():
			return ProviderVideo{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (v ArkSeedanceProvider) doJSON(ctx context.Context, method, endpoint string, payload any, output any) error {
	var body io.Reader
	if payload != nil {
		encoded, _ := json.Marshal(payload)
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+v.APIKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Seedance status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(output)
}

func (v ArkSeedanceProvider) download(ctx context.Context, endpoint string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("Seedance download status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func stringValue(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return strings.TrimSpace(result)
}
func nestedString(value map[string]any, outer, key string) string {
	nested, _ := value[outer].(map[string]any)
	return stringValue(nested, key)
}

// OpenAIProductFidelity uses a general-purpose VLM as both visual comparator
// and OCR engine. It is intentionally separate from ShotVL.
type OpenAIProductFidelity struct {
	Endpoint      string
	Model         string
	ModelRevision string
	APIKey        string
	Threshold     float64
	Client        *http.Client
}

func (v OpenAIProductFidelity) AnalyzeProductFidelity(ctx context.Context, reference ProductReference, original []byte, shot ProductionShot, frames []VisualQCFrame) (ProductFidelityReport, error) {
	endpoint, err := url.Parse(strings.TrimRight(v.Endpoint, "/") + "/chat/completions")
	if err != nil || endpoint.Host == "" {
		return ProductFidelityReport{}, fmt.Errorf("invalid fidelity VLM endpoint")
	}
	if len(frames) == 0 {
		return ProductFidelityReport{}, fmt.Errorf("fidelity requires evidence frames")
	}
	content := []map[string]any{{"type": "text", "text": "Compare every generated frame to the original product. Perform OCR on the original and generated product. Return strict JSON: score, passed, metrics {color,silhouette,proportions,layout,material,logoText}, referenceText, observedText, defects. A logo, label, component-count or layout mismatch is critical and must fail. Do not judge cinematography. shotId=" + shot.ShotID}, {"type": "text", "text": "ORIGINAL_PRODUCT sha256=" + reference.SHA256}, {"type": "image_url", "image_url": map[string]string{"url": "data:" + reference.ContentType + ";base64," + base64.StdEncoding.EncodeToString(original)}}}
	frameIDs := map[string]bool{}
	for _, frame := range frames {
		frameIDs[frame.Evidence.ID] = true
		content = append(content, map[string]any{"type": "text", "text": "evidenceFrameId=" + frame.Evidence.ID}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(frame.JPEG)}})
	}
	payload := map[string]any{"model": v.Model, "temperature": 0, "max_tokens": 1200, "response_format": map[string]any{"type": "json_object"}, "messages": []map[string]any{{"role": "system", "content": "You are a product fidelity inspector and OCR engine. Exact physical identity is mandatory."}, {"role": "user", "content": content}}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return ProductFidelityReport{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.APIKey)
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return ProductFidelityReport{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProductFidelityReport{}, fmt.Errorf("fidelity VLM status %d", resp.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&completion) != nil || len(completion.Choices) == 0 {
		return ProductFidelityReport{}, fmt.Errorf("invalid fidelity completion")
	}
	var report ProductFidelityReport
	if err := json.Unmarshal([]byte(completion.Choices[0].Message.Content), &report); err != nil {
		return ProductFidelityReport{}, fmt.Errorf("decode fidelity report: %w", err)
	}
	report.ModelRevision = strings.TrimSpace(v.ModelRevision)
	if report.ModelRevision == "" {
		report.ModelRevision = v.Model
	}
	report.Threshold = v.Threshold
	report.Passed = report.Passed && report.Score >= v.Threshold
	required := []string{"color", "silhouette", "proportions", "layout", "material", "logoText"}
	for _, key := range required {
		score, ok := report.Metrics[key]
		if !ok || score < 0 || score > 1 {
			return ProductFidelityReport{}, fmt.Errorf("fidelity report missing metric %s", key)
		}
		if key == "logoText" && score < v.Threshold {
			report.Passed = false
		}
	}
	for _, defect := range report.Defects {
		if defect.Severity == "critical" {
			report.Passed = false
		}
		for _, id := range defect.EvidenceFrameIDs {
			if !frameIDs[id] {
				return ProductFidelityReport{}, fmt.Errorf("fidelity defect cites unknown evidence")
			}
		}
	}
	observed := normalizeOCR(strings.Join(report.ObservedText, " "))
	for _, expected := range report.ReferenceText {
		normalized := normalizeOCR(expected)
		if normalized != "" && !strings.Contains(observed, normalized) {
			report.Passed = false
		}
	}
	return report, nil
}

func normalizeOCR(value string) string {
	return strings.ToLower(strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '\u0e00' && r <= '\u0e7f') && !(r >= '\u4e00' && r <= '\u9fff')
	}), ""))
}

type OpenAIShotReviser struct {
	Endpoint, Model, APIKey string
	Client                  *http.Client
}

func (v OpenAIShotReviser) ReviseShot(ctx context.Context, spec ProductionSpec, shot ProductionShot, revision int, defects []VisualQCDefect) (string, error) {
	endpoint, err := url.Parse(strings.TrimRight(v.Endpoint, "/") + "/chat/completions")
	if err != nil || endpoint.Host == "" {
		return "", fmt.Errorf("invalid Qwen endpoint")
	}
	specJSON, _ := json.Marshal(spec.Continuity.Product)
	shotJSON, _ := json.Marshal(shot)
	defectJSON, _ := json.Marshal(defects)
	prompt := fmt.Sprintf("Rewrite only the provider prompt for %s revision %d. Preserve the immutable product bible exactly. Correct only cited defects. Return JSON {\"prompt\":\"...\"}. Product=%s Shot=%s Defects=%s", shot.ShotID, revision, specJSON, shotJSON, defectJSON)
	payload := map[string]any{"model": v.Model, "temperature": 0.2, "max_tokens": 900, "response_format": map[string]any{"type": "json_object"}, "messages": []map[string]string{{"role": "system", "content": "You are KWANNI shot repair. Never alter verified product identity or unrelated shots."}, {"role": "user", "content": prompt}}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.APIKey)
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Qwen revision status %d", resp.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&completion) != nil || len(completion.Choices) == 0 {
		return "", fmt.Errorf("invalid Qwen revision")
	}
	var out struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal([]byte(completion.Choices[0].Message.Content), &out) != nil || strings.TrimSpace(out.Prompt) == "" {
		return "", fmt.Errorf("Qwen returned no revised prompt")
	}
	return strings.TrimSpace(out.Prompt), nil
}
