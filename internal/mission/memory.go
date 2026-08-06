package mission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
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
	if value.Draft != nil {
		copy := *value.Draft
		copy.Hashtags = append([]string(nil), copy.Hashtags...)
		copy.Timeline = append([]Clip(nil), copy.Timeline...)
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
