package mission

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGeminiVeoProviderSendsExactReferenceAndDownloadsResult(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "veo-secret" {
			t.Errorf("missing API key header")
		}
		switch r.URL.Path {
		case "/models/veo-3.1-generate-preview:predictLongRunning":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"name":"operations/123"}`)
		case "/operations/123":
			_, _ = io.WriteString(w, `{"done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"`+serverURL(r)+`/video.mp4"}}]}}}`)
		case "/video.mp4":
			_, _ = w.Write([]byte("mp4-video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := GeminiVeoProvider{BaseURL: server.URL, Model: "veo-3.1-generate-preview", APIKey: "veo-secret", Client: server.Client(), PollInterval: time.Millisecond}
	video, err := provider.Generate(context.Background(), ProviderVideoRequest{Prompt: "move product", Reference: []byte("exact-product-image"), ReferenceType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	defer video.Body.Close()
	value, _ := io.ReadAll(video.Body)
	if string(value) != "mp4-video" || video.TaskID != "operations/123" {
		t.Fatalf("unexpected video: %#v %q", video, value)
	}
	encoded, _ := json.Marshal(received)
	body := string(encoded)
	if !strings.Contains(body, "referenceImages") || !strings.Contains(body, "ZXhhY3QtcHJvZHVjdC1pbWFnZQ==") || !strings.Contains(body, `"durationSeconds":8`) {
		t.Fatalf("request did not bind exact reference and 8-second contract: %s", body)
	}
	if strings.Contains(body, "veo-secret") {
		t.Fatal("API key leaked into provider payload")
	}
}

func TestArkSeedanceProviderSendsReferenceImageAndDownloadsResult(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seed.mp4" && r.Header.Get("Authorization") != "Bearer seed-secret" {
			t.Errorf("missing bearer header")
		}
		switch r.URL.Path {
		case "/contents/generations/tasks":
			_ = json.NewDecoder(r.Body).Decode(&received)
			_, _ = io.WriteString(w, `{"id":"task-1"}`)
		case "/contents/generations/tasks/task-1":
			_, _ = io.WriteString(w, `{"status":"succeeded","content":{"video_url":"`+serverURL(r)+`/seed.mp4"}}`)
		case "/seed.mp4":
			_, _ = w.Write([]byte("seed-video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider := ArkSeedanceProvider{BaseURL: server.URL, Model: "seedance-model", APIKey: "seed-secret", Client: server.Client(), PollInterval: time.Millisecond}
	video, err := provider.Generate(context.Background(), ProviderVideoRequest{Prompt: "show product", Reference: []byte("image"), ReferenceType: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	defer video.Body.Close()
	value, _ := io.ReadAll(video.Body)
	if string(value) != "seed-video" {
		t.Fatalf("unexpected video %q", value)
	}
	encoded, _ := json.Marshal(received)
	body := string(encoded)
	if !strings.Contains(body, "reference_image") || !strings.Contains(body, "data:image/jpeg;base64,aW1hZ2U=") || !strings.Contains(body, "--ratio 9:16 --dur 8") {
		t.Fatalf("missing Seedance image contract: %s", body)
	}
	if strings.Contains(body, "seed-secret") {
		t.Fatal("API key leaked into payload")
	}
}

func TestOpenAIProductFidelityFailsClosedOnLogoOCRMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"score\":0.92,\"passed\":true,\"metrics\":{\"color\":0.99,\"silhouette\":0.99,\"proportions\":0.99,\"layout\":0.99,\"material\":0.99,\"logoText\":0.4},\"referenceText\":[\"BUNNY\"],\"observedText\":[\"BUNNV\"],\"defects\":[]}"}}]}`)
	}))
	defer server.Close()
	analyzer := OpenAIProductFidelity{Endpoint: server.URL, Model: "vlm", ModelRevision: "vlm@sha", Threshold: 0.88, Client: server.Client()}
	report, err := analyzer.AnalyzeProductFidelity(context.Background(), ProductReference{ContentType: "image/png", SHA256: strings.Repeat("a", 64)}, []byte("original"), ProductionShot{ShotID: "shot01"}, []VisualQCFrame{{Evidence: EvidenceFrame{ID: "frame01"}, JPEG: []byte("jpeg")}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("logo/OCR mismatch must fail closed")
	}
}

func TestProviderShotPromptNeverDropsProductIdentity(t *testing.T) {
	spec := ProductionSpec{Continuity: ContinuityBible{Product: ProductBible{Name: "Clicker", VerifiedFacts: []string{"four grids"}, ForbiddenChanges: []string{"logo"}}}}
	prompt := providerShotPrompt(spec, ProductionShot{ShotID: "shot02", SubjectAction: "press"}, "veo")
	for _, required := range []string{"attached original product image", "color, silhouette, proportions, component count and layout", "Clicker", "four grids", "shot02"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing %q in %s", required, prompt)
		}
	}
}

func TestWanLocalProviderSendsExactReferenceAndFiveSecondContract(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate" || r.Header.Get("Authorization") != "Bearer wan-secret" {
			t.Errorf("unexpected private runtime request")
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("X-Kwanni-Task-ID", "wan-1")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("local-preview"))
	}))
	defer server.Close()
	provider := WanLocalProvider{Endpoint: server.URL, APIKey: "wan-secret", Client: server.Client()}
	video, err := provider.Generate(context.Background(), ProviderVideoRequest{Prompt: "three fast beats", Reference: []byte("exact-image"), ReferenceType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	defer video.Body.Close()
	encoded, _ := json.Marshal(received)
	body := string(encoded)
	if video.TaskID != "wan-1" || !strings.Contains(body, "data:image/png;base64,ZXhhY3QtaW1hZ2U=") || !strings.Contains(body, `"durationSeconds":5`) || !strings.Contains(body, `"frameCount":121`) {
		t.Fatalf("invalid Wan contract: task=%q body=%s", video.TaskID, body)
	}
	if strings.Contains(body, "wan-secret") {
		t.Fatal("Wan credential leaked into body")
	}
}

func TestWanPreviewPromptPreservesImmutableProductAndThreeBeats(t *testing.T) {
	spec := ProductionSpec{Continuity: ContinuityBible{Product: ProductBible{Name: "Clicker", VerifiedFacts: []string{"four colored grids"}}}, Shots: []ProductionShot{{ShotID: "shot01"}, {ShotID: "shot02"}, {ShotID: "shot03"}}}
	prompt := wanPreviewPrompt(spec)
	for _, required := range []string{"5-second portrait", "three story beats", "immutable hero asset", "component count and layout", "Clicker", "shot01", "shot03"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing %q in %s", required, prompt)
		}
	}
}

func TestWanPipelineCreatesOnePreviewWithoutPaidFidelityDependencies(t *testing.T) {
	now := time.Date(2026, 8, 10, 1, 30, 0, 0, time.UTC)
	objects := readableMemoryObjectStore{objects: map[string][]byte{"product.png": []byte("exact-reference")}}
	jobs := &progressOnlyVideoStore{}
	q := CloudVideoPipeline{Jobs: jobs, Objects: objects, Reader: objects, Providers: map[string]VideoProvider{"wan": staticWanProvider{}}, Now: func() time.Time { return now }}
	draft := Draft{ProductionSpec: ProductionSpec{Continuity: ContinuityBible{Product: ProductBible{Name: "clicker"}}, Shots: []ProductionShot{{ShotID: "shot01"}, {ShotID: "shot02"}, {ShotID: "shot03"}}}}
	result := VideoGenerationResult{Provider: "wan", OutputKey: "missions/m1/exports/preview.mp4"}
	if err := q.process(context.Background(), "job-wan", VideoGenerationRequest{MissionID: "m1", Provider: "wan", Reference: ProductReference{StorageKey: "product.png", ContentType: "image/png"}, Draft: draft, OutputKey: result.OutputKey}, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Shots) != 1 || result.Shots[0].ShotID != "local-preview" || result.Shots[0].State != GenerationSucceeded || string(objects.objects[result.OutputKey]) != "wan-mp4" {
		t.Fatalf("unexpected local preview: %#v", result)
	}
}

type staticWanProvider struct{}

func (staticWanProvider) Name() string { return "wan" }
func (staticWanProvider) Generate(context.Context, ProviderVideoRequest) (ProviderVideo, error) {
	return ProviderVideo{TaskID: "wan-local-1", ContentType: "video/mp4", Body: io.NopCloser(strings.NewReader("wan-mp4"))}, nil
}

func TestCloudPipelineRepairsOnlyFailedShotAndAssembles(t *testing.T) {
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	objects := readableMemoryObjectStore{objects: map[string][]byte{"product.png": []byte("exact-reference")}}
	provider := &fakeVideoProvider{calls: map[string]int{}}
	fidelity := &fakeFidelityAnalyzer{calls: map[string]int{}}
	reviser := &fakeShotReviser{}
	jobs := &progressOnlyVideoStore{}
	q := CloudVideoPipeline{Jobs: jobs, Objects: objects, Reader: objects, Providers: map[string]VideoProvider{"veo": provider}, Fidelity: fidelity, Cinematic: passingCinematic{}, Reviser: reviser, Runner: materializingRunner{}, Now: func() time.Time { return now }, TempRoot: t.TempDir()}
	draft := Draft{ProductionSpec: ProductionSpec{DurationSeconds: 8, Continuity: ContinuityBible{Product: ProductBible{Name: "clicker"}}, Shots: []ProductionShot{{ShotID: "shot01", CaptureShot: 1, DurationSeconds: 2}, {ShotID: "shot02", CaptureShot: 2, DurationSeconds: 3}, {ShotID: "shot03", CaptureShot: 3, DurationSeconds: 3}}}}
	result := VideoGenerationResult{Provider: "veo", OutputKey: "missions/m1/exports/final.mp4"}
	err := q.process(context.Background(), "job-1", VideoGenerationRequest{MissionID: "m1", Provider: "veo", Reference: ProductReference{StorageKey: "product.png", ContentType: "image/png", SHA256: strings.Repeat("a", 64)}, Draft: draft, OutputKey: result.OutputKey}, &result)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls["shot01"] != 1 || provider.calls["shot02"] != 2 || provider.calls["shot03"] != 1 {
		t.Fatalf("unexpected provider calls: %#v", provider.calls)
	}
	if reviser.calls != 1 {
		t.Fatalf("Qwen should revise only one failed shot, got %d", reviser.calls)
	}
	if len(result.Shots) != 3 || result.Shots[1].Revision != 2 || result.Shots[1].State != GenerationSucceeded {
		t.Fatalf("unexpected shot results: %#v", result.Shots)
	}
	if len(objects.objects[result.OutputKey]) == 0 {
		t.Fatal("final assembled video was not stored")
	}
}

func serverURL(r *http.Request) string { return "http://" + r.Host }

type completedVideoQueue struct {
	job    ProcessingJob
	result VideoGenerationResult
}

func (q completedVideoQueue) EnqueueVideo(context.Context, VideoGenerationRequest) (ProcessingJob, error) {
	return q.job, nil
}
func (q completedVideoQueue) GetVideo(context.Context, string) (ProcessingJob, *VideoGenerationResult, error) {
	result := q.result
	return q.job, &result, nil
}

func TestServicePromotesVerifiedProviderOutputWithoutLegacyAssets(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	store := readableMemoryObjectStore{objects: map[string][]byte{}}
	service := NewServiceWithOptions(repo, store, CaptionBackedPlanner{Captions: FallbackCaptioner{}}, NewMemoryJobQueue(func() time.Time { return now }, func() string { return "export-job" }), nil, &MemoryLedger{}, func() time.Time { return now }, func() string { return "mission-1" }, 8<<20)
	created, err := service.Create(context.Background(), "u1", Product{Name: "p", Description: "d"})
	if err != nil {
		t.Fatal(err)
	}
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, make([]byte, 512)...)
	_, err = service.UploadProductReference(context.Background(), created.ID, 1, "image/jpeg", jpeg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.GenerateDraft(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	job := ProcessingJob{ID: "provider-job", Kind: "providerVideo", State: JobSucceeded, IdempotencyKey: "provider-video:mission-1:veo", UpdatedAt: now}
	service.SetVideoGenerationQueue(completedVideoQueue{job: job, result: VideoGenerationResult{Provider: "veo", OutputKey: "missions/mission-1/exports/first-post-veo.mp4", Shots: []GeneratedShot{{ShotID: "shot01", State: GenerationSucceeded}}}})
	queued, err := service.GenerateVideo(context.Background(), created.ID, "veo")
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != StateVideoGenerating {
		t.Fatalf("state=%s", queued.State)
	}
	finished, err := service.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != StateExported || finished.Export == nil || finished.Export.StorageKey == "" {
		t.Fatalf("not promoted: %#v", finished)
	}
}

func TestServiceQueuesWanAsDurableLocalPreviewWithConservativeETA(t *testing.T) {
	now := time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	store := readableMemoryObjectStore{objects: map[string][]byte{}}
	service := NewServiceWithOptions(repo, store, CaptionBackedPlanner{Captions: FallbackCaptioner{}}, NewMemoryJobQueue(func() time.Time { return now }, func() string { return "export-job" }), nil, &MemoryLedger{}, func() time.Time { return now }, func() string { return "mission-wan" }, 8<<20)
	created, _ := service.Create(context.Background(), "u1", Product{Name: "p", Description: "d"})
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, make([]byte, 512)...)
	_, _ = service.UploadProductReference(context.Background(), created.ID, 1, "image/jpeg", jpeg)
	_, _ = service.GenerateDraft(context.Background(), created.ID)
	job := ProcessingJob{ID: "wan-job", Kind: "providerVideo", State: JobQueued, IdempotencyKey: "provider-video:mission-wan:wan", UpdatedAt: now}
	service.SetVideoGenerationQueue(completedVideoQueue{job: job, result: VideoGenerationResult{Provider: "wan"}})
	queued, err := service.GenerateVideo(context.Background(), created.ID, "wan")
	if err != nil {
		t.Fatal(err)
	}
	if queued.VideoGeneration == nil || queued.VideoGeneration.Mode != "localPreview" || queued.VideoGeneration.EstimateSeconds != 1800 || !queued.VideoGeneration.EstimatedReadyAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("unexpected local-preview receipt: %#v", queued.VideoGeneration)
	}
}

type readableMemoryObjectStore struct{ objects map[string][]byte }

func (s readableMemoryObjectStore) Put(_ context.Context, key, contentType string, reader io.Reader) (ObjectMetadata, error) {
	value, _ := io.ReadAll(reader)
	s.objects[key] = value
	hash := sha256.Sum256(value)
	return ObjectMetadata{Key: key, ContentType: contentType, Bytes: int64(len(value)), SHA256: hex.EncodeToString(hash[:])}, nil
}
func (s readableMemoryObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.objects[key])), nil
}
func (s readableMemoryObjectStore) DownloadURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://s3.example/" + key, nil
}

type fakeVideoProvider struct{ calls map[string]int }

func (p *fakeVideoProvider) Name() string { return "veo" }
func (p *fakeVideoProvider) Generate(_ context.Context, request ProviderVideoRequest) (ProviderVideo, error) {
	p.calls[request.Shot.ShotID]++
	return ProviderVideo{TaskID: request.Shot.ShotID, ContentType: "video/mp4", Body: io.NopCloser(strings.NewReader("provider-video-" + request.Shot.ShotID))}, nil
}

type fakeFidelityAnalyzer struct{ calls map[string]int }

func (a *fakeFidelityAnalyzer) AnalyzeProductFidelity(_ context.Context, _ ProductReference, _ []byte, shot ProductionShot, frames []VisualQCFrame) (ProductFidelityReport, error) {
	a.calls[shot.ShotID]++
	passed := !(shot.ShotID == "shot02" && a.calls[shot.ShotID] == 1)
	report := ProductFidelityReport{ModelRevision: "fidelity@1", Score: .99, Threshold: .88, Passed: passed, Metrics: map[string]float64{"color": 1, "silhouette": 1, "proportions": 1, "layout": 1, "material": 1, "logoText": 1}}
	if !passed {
		report.Defects = []VisualQCDefect{{Code: "logo", Severity: "critical", Message: "logo mismatch", EvidenceFrameIDs: []string{frames[0].Evidence.ID}}}
	}
	return report, nil
}

type passingCinematic struct{}

func (passingCinematic) Analyze(_ context.Context, spec ProductionSpec, frames []VisualQCFrame, revision int) (VisualQCReport, error) {
	return VisualQCReport{Revision: revision, ModelRevision: "shotvl@1", Threshold: .75, Score: .95, Passed: true, EvidenceFrames: []EvidenceFrame{frames[0].Evidence}, Shots: []VisualQCShot{{ShotID: spec.Shots[0].ShotID, Metrics: VisualQCMetric{ShotSize: 1, Composition: 1, CameraAngle: 1, Depth: 1, Lighting: 1, SubjectPlacement: 1, ProductPlacement: 1}}}}, nil
}

type fakeShotReviser struct{ calls int }

func (r *fakeShotReviser) ReviseShot(_ context.Context, _ ProductionSpec, shot ProductionShot, _ int, _ []VisualQCDefect) (string, error) {
	r.calls++
	return "repair only " + shot.ShotID, nil
}

type progressOnlyVideoStore struct{}

func (*progressOnlyVideoStore) EnqueueVideoJob(context.Context, VideoGenerationRequest) (ProcessingJob, error) {
	panic("not used")
}
func (*progressOnlyVideoStore) ClaimNextVideo(context.Context, string, time.Time, time.Time) (ProcessingJob, VideoGenerationRequest, bool, error) {
	panic("not used")
}
func (*progressOnlyVideoStore) UpdateVideoProgress(context.Context, string, VideoGenerationResult, time.Time) error {
	return nil
}
func (*progressOnlyVideoStore) CompleteVideo(context.Context, string, string, VideoGenerationResult, time.Time) (ProcessingJob, error) {
	panic("not used")
}
func (*progressOnlyVideoStore) FailVideo(context.Context, string, string, VideoGenerationResult, string, time.Time) error {
	panic("not used")
}
func (*progressOnlyVideoStore) GetVideoJob(context.Context, string) (ProcessingJob, *VideoGenerationResult, error) {
	panic("not used")
}

type materializingRunner struct{}

func (materializingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "ffprobe" {
		return []byte(`{"streams":[{"codec_type":"video","width":1080,"height":1920}]}`), nil
	}
	target := args[len(args)-1]
	if strings.Contains(target, "%02d") {
		for index := 1; index <= providerFrameCount; index++ {
			if err := os.WriteFile(fmt.Sprintf(target, index), []byte("jpeg-frame"), 0o600); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	if filepath.Ext(target) == ".mp4" {
		if err := os.WriteFile(target, []byte("verified-mp4"), 0o600); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
