package mission

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

type fakeMediaStore struct {
	mu     sync.Mutex
	inputs map[string][]byte
	output []byte
}

func (s *fakeMediaStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.inputs[key])), nil
}
func (s *fakeMediaStore) Put(_ context.Context, key, contentType string, reader io.Reader) (ObjectMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output, _ = io.ReadAll(reader)
	hash := sha256.Sum256(s.output)
	return ObjectMetadata{Key: key, ContentType: contentType, Bytes: int64(len(s.output)), SHA256: hex.EncodeToString(hash[:])}, nil
}
func (s *fakeMediaStore) outputLen() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.output) }

type fakeJobStore struct {
	mu      sync.Mutex
	job     ProcessingJob
	request ExportRequest
}

func (s *fakeJobStore) ClaimExport(_ context.Context, request ExportRequest) (ProcessingJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.ID != "" {
		return s.job, false, nil
	}
	s.job = ProcessingJob{ID: "job-1", State: JobQueued, Kind: "ffmpegExport", IdempotencyKey: request.IdempotencyKey}
	s.request = request
	return s.job, true, nil
}
func (s *fakeJobStore) ClaimNextExport(_ context.Context, _, _ time.Time) (ProcessingJob, ExportRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.State != JobQueued {
		return ProcessingJob{}, ExportRequest{}, false, nil
	}
	s.job.State = JobRunning
	return s.job, s.request, true, nil
}
func (s *fakeJobStore) CompleteExport(_ context.Context, _ string, at time.Time) (ProcessingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.State = JobSucceeded
	s.job.UpdatedAt = at
	return s.job, nil
}
func (s *fakeJobStore) FailExport(_ context.Context, _ string, message string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.State = JobFailed
	s.job.LastError = message
	s.job.UpdatedAt = at
	return nil
}
func (s *fakeJobStore) GetExportJob(_ context.Context, id string) (ProcessingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.ID != id {
		return ProcessingJob{}, ErrNotFound
	}
	return s.job, nil
}

type fakeFFmpegRunner struct{}

func (fakeFFmpegRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "ffprobe" {
		return []byte(`{"streams":[{"codec_type":"video","width":1080,"height":1920}]}`), nil
	}
	target := args[len(args)-1]
	return nil, os.WriteFile(target, []byte("valid-mp4-fixture"), 0o600)
}

func TestFFmpegExportProducesVerifiedStoredArtifactAndIsIdempotent(t *testing.T) {
	store := &fakeMediaStore{inputs: map[string][]byte{"s1": []byte("a"), "s2": []byte("bb"), "s3": []byte("ccc")}}
	jobs := &fakeJobStore{}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	queue := FFmpegExportQueue{Jobs: jobs, Objects: store, Reader: store, Runner: fakeFFmpegRunner{}, Now: func() time.Time { return now }, TempRoot: t.TempDir()}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(workerCtx)
	spec := deterministicProductionSpec(Product{Name: "สินค้า", Description: "จริง"}, []Shot{{1, "ก่อน"}, {2, "ใช้"}, {3, "หลัง"}})
	request := ExportRequest{MissionID: "m1", IdempotencyKey: "export:m1:1", OutputKey: "missions/m1/exports/first-post.mp4", Draft: Draft{ProductionSpec: spec}, Assets: []Asset{{Shot: 1, StorageKey: "s1", ContentType: "image/jpeg", Bytes: 1}, {Shot: 2, StorageKey: "s2", ContentType: "video/mp4", Bytes: 2}, {Shot: 3, StorageKey: "s3", ContentType: "image/png", Bytes: 3}}}
	job, err := queue.EnqueueExport(context.Background(), request)
	if err != nil || job.State != JobQueued {
		t.Fatalf("queued job=%#v err=%v", job, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, _ = queue.GetExport(context.Background(), job.ID)
		if job.State == JobSucceeded {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if job.State != JobSucceeded || store.outputLen() == 0 {
		t.Fatalf("job=%#v outputBytes=%d err=%v", job, store.outputLen(), err)
	}
	again, err := queue.EnqueueExport(context.Background(), request)
	if err != nil || again.ID != job.ID || again.State != JobSucceeded {
		t.Fatalf("idempotent job=%#v err=%v", again, err)
	}
}
