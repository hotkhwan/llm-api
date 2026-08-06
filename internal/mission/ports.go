package mission

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound        = errors.New("mission not found")
	ErrVersionConflict = errors.New("mission version conflict")
	ErrInvalidState    = errors.New("invalid mission state transition")
)

// Repository is deliberately compatible with a MongoDB implementation: Save
// is a compare-and-swap on Version, so concurrent workers cannot advance the
// same mission twice.
type Repository interface {
	Create(context.Context, Mission) error
	Get(context.Context, string) (Mission, error)
	Save(context.Context, Mission, int64) error
}

// ObjectStore represents the S3 API exposed by SeaweedFS. Domain code never
// depends on a vendor-specific MinIO SDK or name.
type ObjectStore interface {
	Put(context.Context, string, string, io.Reader) (ObjectMetadata, error)
}

type ObjectMetadata struct {
	Key         string
	ContentType string
	Bytes       int64
	SHA256      string
}

type CaptionRequest struct {
	Product Product
	Shots   []Shot
}

type CaptionResult struct {
	Caption    string
	CTA        string
	Hashtags   []string
	Provider   string
	Units      int64
	CostMicros int64
}

type CaptionGenerator interface {
	Generate(context.Context, CaptionRequest) (CaptionResult, error)
}

type AuditSink interface {
	RecordAudit(context.Context, AuditEvent) error
	RecordCost(context.Context, CostEntry) error
}
