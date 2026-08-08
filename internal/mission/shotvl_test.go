package mission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func visualTestSpec() ProductionSpec {
	return deterministicProductionSpec(Product{Name: "สินค้า", Description: "ข้อมูลจริง"}, []Shot{{1, "ก่อน"}, {2, "ใช้"}, {3, "หลัง"}})
}

func visualTestShots() []VisualQCShot {
	metric := VisualQCMetric{ShotSize: .8, Composition: .8, CameraAngle: .8, Depth: .8, Lighting: .8, SubjectPlacement: .8, ProductPlacement: .8}
	return []VisualQCShot{{ShotID: "shot01", Metrics: metric}, {ShotID: "shot02", Metrics: metric}, {ShotID: "shot03", Metrics: metric}}
}

func TestOpenAICompatibleVisualQCValidatesStructuredEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer shot-secret" {
			t.Fatal("missing ShotVL bearer secret")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		content, _ := json.Marshal(map[string]any{"score": .82, "passed": true, "shots": visualTestShots()})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
	}))
	defer server.Close()
	frames := []VisualQCFrame{{Evidence: EvidenceFrame{ID: "frame00", StorageKey: "f0"}, JPEG: []byte("jpeg")}}
	client := OpenAICompatibleVisualQC{Endpoint: server.URL + "/v1", Model: "shotvl", ModelRevision: "rev-abc", APIKey: "shot-secret", Threshold: .75, Client: server.Client()}
	report, err := client.Analyze(context.Background(), visualTestSpec(), frames, 2)
	if err != nil || !report.Passed || report.Revision != 2 || report.ModelRevision != "rev-abc" || len(report.EvidenceFrames) != 1 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}

type fakeVisualAnalyzer struct{}

func (fakeVisualAnalyzer) Analyze(_ context.Context, spec ProductionSpec, frames []VisualQCFrame, revision int) (VisualQCReport, error) {
	if len(frames) != visualQCFrameCount {
		return VisualQCReport{}, fmt.Errorf("frames=%d", len(frames))
	}
	return VisualQCReport{Revision: revision, ModelRevision: "test-rev", Threshold: .75, Score: .8, Passed: true, Shots: visualTestShots(), EvidenceFrames: evidenceFromFrames(frames)}, nil
}
func evidenceFromFrames(frames []VisualQCFrame) []EvidenceFrame {
	result := make([]EvidenceFrame, len(frames))
	for i := range frames {
		result[i] = frames[i].Evidence
	}
	return result
}

type fakeKeyframeRunner struct{}

func (fakeKeyframeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "ffmpeg" {
		return nil, fmt.Errorf("unexpected command")
	}
	pattern := args[len(args)-1]
	for index := 1; index <= visualQCFrameCount; index++ {
		if err := os.WriteFile(fmt.Sprintf(pattern, index), []byte(fmt.Sprintf("jpeg-%02d", index)), 0o600); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

type fakeVisualJobs struct {
	mu      sync.Mutex
	job     ProcessingJob
	request VisualQCRequest
	report  *VisualQCReport
}

func (s *fakeVisualJobs) EnqueueVisualQCJob(_ context.Context, request VisualQCRequest) (ProcessingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.ID == "" {
		s.job = ProcessingJob{ID: "vj1", Kind: "visualQc", State: JobQueued, IdempotencyKey: request.IdempotencyKey}
		s.request = request
	}
	return s.job, nil
}
func (s *fakeVisualJobs) ClaimNextVisualQC(_ context.Context, _ string, _, _ time.Time) (ProcessingJob, VisualQCRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.State != JobQueued {
		return ProcessingJob{}, VisualQCRequest{}, false, nil
	}
	s.job.State = JobRunning
	return s.job, s.request, true, nil
}
func (s *fakeVisualJobs) CompleteVisualQC(_ context.Context, _, _ string, report VisualQCReport, at time.Time) (ProcessingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.State = JobSucceeded
	s.job.UpdatedAt = at
	s.report = &report
	return s.job, nil
}
func (s *fakeVisualJobs) FailVisualQC(_ context.Context, _, _, message string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.State = JobFailed
	s.job.LastError = message
	return nil
}
func (s *fakeVisualJobs) GetVisualQCJob(_ context.Context, _ string) (ProcessingJob, *VisualQCReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var report *VisualQCReport
	if s.report != nil {
		copy := *s.report
		report = &copy
	}
	return s.job, report, nil
}

func TestShotVLQueueExtractsDeterministicEvidenceAndCompletes(t *testing.T) {
	store := &fakeMediaStore{inputs: map[string][]byte{"export.mp4": []byte("video")}}
	jobs := &fakeVisualJobs{}
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	queue := &ShotVLQueue{Jobs: jobs, Objects: store, Reader: store, Runner: fakeKeyframeRunner{}, Analyzer: fakeVisualAnalyzer{}, Now: func() time.Time { return now }, WorkerID: "worker", TempRoot: t.TempDir(), Timeout: time.Second}
	job, err := queue.EnqueueVisualQC(context.Background(), VisualQCRequest{MissionID: "m1", IdempotencyKey: "v1", VideoKey: "export.mp4", Spec: visualTestSpec(), Revision: 1})
	if err != nil || job.State != JobQueued {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, report, _ := queue.GetVisualQC(context.Background(), job.ID)
		if job.State == JobSucceeded {
			if report == nil || len(report.EvidenceFrames) != 12 || !strings.HasSuffix(report.EvidenceFrames[0].StorageKey, "frame00.jpg") {
				t.Fatalf("report=%#v", report)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("visual QC job did not complete")
}
