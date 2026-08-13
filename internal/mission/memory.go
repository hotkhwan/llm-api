package mission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu       sync.RWMutex
	missions map[string]Mission
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{missions: make(map[string]Mission)}
}

func (r *MemoryRepository) Create(_ context.Context, mission Mission) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.missions[mission.ID]; exists {
		return ErrVersionConflict
	}
	r.missions[mission.ID] = cloneMission(mission)
	return nil
}

func (r *MemoryRepository) Get(_ context.Context, id string) (Mission, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mission, ok := r.missions[id]
	if !ok {
		return Mission{}, ErrNotFound
	}
	return cloneMission(mission), nil
}

func (r *MemoryRepository) Save(_ context.Context, mission Mission, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.missions[mission.ID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expected {
		return ErrVersionConflict
	}
	mission.Version = expected + 1
	r.missions[mission.ID] = cloneMission(mission)
	return nil
}

func cloneMission(value Mission) Mission {
	value.Product.Facts = append([]string(nil), value.Product.Facts...)
	value.Shots = append([]Shot(nil), value.Shots...)
	value.Assets = append([]Asset(nil), value.Assets...)
	value.ProductReferences = append([]ProductReference(nil), value.ProductReferences...)
	if value.Draft != nil {
		copy := *value.Draft
		copy.Hashtags = append([]string(nil), copy.Hashtags...)
		copy.Timeline = append([]Clip(nil), copy.Timeline...)
		copy.ProductionSpec = cloneProductionSpec(copy.ProductionSpec)
		copy.RoleExecutions = append([]RoleExecution(nil), copy.RoleExecutions...)
		value.Draft = &copy
	}
	if value.Export != nil {
		copy := *value.Export
		value.Export = &copy
	}
	if value.Posted != nil {
		copy := *value.Posted
		value.Posted = &copy
	}
	if value.ExportJob != nil {
		copy := *value.ExportJob
		value.ExportJob = &copy
	}
	if value.Outcome != nil {
		copy := *value.Outcome
		value.Outcome = &copy
	}
	if value.NextAction != nil {
		copy := *value.NextAction
		value.NextAction = &copy
	}
	if value.VisualQC != nil {
		copy := *value.VisualQC
		copy.History = append([]VisualQCReport(nil), copy.History...)
		if copy.Job != nil {
			job := *copy.Job
			copy.Job = &job
		}
		if copy.LatestReport != nil {
			report := cloneVisualQCReport(*copy.LatestReport)
			copy.LatestReport = &report
		}
		for index := range copy.History {
			copy.History[index] = cloneVisualQCReport(copy.History[index])
		}
		if copy.ManualOverride != nil {
			override := *copy.ManualOverride
			copy.ManualOverride = &override
		}
		value.VisualQC = &copy
	}
	if value.VideoGeneration != nil {
		copy := *value.VideoGeneration
		copy.Shots = append([]GeneratedShot(nil), copy.Shots...)
		if copy.Job != nil {
			job := *copy.Job
			copy.Job = &job
		}
		for index := range copy.Shots {
			copy.Shots[index].Defects = append([]VisualQCDefect(nil), copy.Shots[index].Defects...)
			if copy.Shots[index].Fidelity != nil {
				fidelity := *copy.Shots[index].Fidelity
				fidelity.ReferenceText = append([]string(nil), fidelity.ReferenceText...)
				fidelity.ObservedText = append([]string(nil), fidelity.ObservedText...)
				fidelity.Defects = append([]VisualQCDefect(nil), fidelity.Defects...)
				copy.Shots[index].Fidelity = &fidelity
			}
			if copy.Shots[index].Cinematic != nil {
				cinematic := cloneVisualQCReport(*copy.Shots[index].Cinematic)
				copy.Shots[index].Cinematic = &cinematic
			}
		}
		value.VideoGeneration = &copy
	}
	return value
}

func cloneVisualQCReport(value VisualQCReport) VisualQCReport {
	value.EvidenceFrames = append([]EvidenceFrame(nil), value.EvidenceFrames...)
	value.Shots = append([]VisualQCShot(nil), value.Shots...)
	for index := range value.Shots {
		value.Shots[index].Defects = append([]VisualQCDefect(nil), value.Shots[index].Defects...)
		for defect := range value.Shots[index].Defects {
			value.Shots[index].Defects[defect].EvidenceFrameIDs = append([]string(nil), value.Shots[index].Defects[defect].EvidenceFrameIDs...)
		}
	}
	return value
}

func cloneProductionSpec(value ProductionSpec) ProductionSpec {
	value.StoryBeats = append([]string(nil), value.StoryBeats...)
	value.Shots = append([]ProductionShot(nil), value.Shots...)
	value.Continuity.Product.VerifiedFacts = append([]string(nil), value.Continuity.Product.VerifiedFacts...)
	value.Continuity.Product.ReferenceKeys = append([]string(nil), value.Continuity.Product.ReferenceKeys...)
	value.Continuity.Product.RequiredDetails = append([]string(nil), value.Continuity.Product.RequiredDetails...)
	value.Continuity.Product.ForbiddenChanges = append([]string(nil), value.Continuity.Product.ForbiddenChanges...)
	value.Continuity.Character.Constraints = append([]string(nil), value.Continuity.Character.Constraints...)
	value.Continuity.Camera.Constraints = append([]string(nil), value.Continuity.Camera.Constraints...)
	if value.ProviderPrompts != nil {
		copied := make(map[string]string, len(value.ProviderPrompts))
		for key, item := range value.ProviderPrompts {
			copied[key] = item
		}
		value.ProviderPrompts = copied
	}
	return value
}

type MemoryObjectStore struct{}

func (MemoryObjectStore) Put(_ context.Context, key, contentType string, reader io.Reader) (ObjectMetadata, error) {
	h := sha256.New()
	n, err := io.Copy(h, reader)
	if err != nil {
		return ObjectMetadata{}, err
	}
	return ObjectMetadata{Key: key, ContentType: contentType, Bytes: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func (MemoryObjectStore) DownloadURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "", nil
}

type MemoryJobQueue struct {
	mu   sync.Mutex
	jobs map[string]ProcessingJob
	now  func() time.Time
	id   IDGenerator
}

func NewMemoryJobQueue(now func() time.Time, id IDGenerator) *MemoryJobQueue {
	return &MemoryJobQueue{jobs: make(map[string]ProcessingJob), now: now, id: id}
}

func (q *MemoryJobQueue) EnqueueExport(_ context.Context, request ExportRequest) (ProcessingJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, exists := q.jobs[request.IdempotencyKey]; exists {
		return job, nil
	}
	job := ProcessingJob{ID: q.id(), Kind: "ffmpegExport", State: JobSucceeded, IdempotencyKey: request.IdempotencyKey, UpdatedAt: q.now().UTC()}
	q.jobs[request.IdempotencyKey] = job
	return job, nil
}

func (q *MemoryJobQueue) GetExport(_ context.Context, id string) (ProcessingJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, job := range q.jobs {
		if job.ID == id {
			return job, nil
		}
	}
	return ProcessingJob{}, ErrNotFound
}

type MemoryLedger struct {
	mu     sync.Mutex
	Audits []AuditEvent
	Costs  []CostEntry
}

func (l *MemoryLedger) RecordAudit(_ context.Context, event AuditEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Audits = append(l.Audits, event)
	return nil
}
func (l *MemoryLedger) RecordCost(_ context.Context, entry CostEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Costs = append(l.Costs, entry)
	return nil
}
