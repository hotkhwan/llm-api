package mission

import (
	"context"
	"errors"
	"io"
	"time"
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

type ObjectURLSigner interface {
	DownloadURL(context.Context, string, time.Duration) (string, error)
}

type ObjectReader interface {
	Get(context.Context, string) (io.ReadCloser, error)
}

type ExportJobStore interface {
	ClaimExport(context.Context, ExportRequest) (ProcessingJob, bool, error)
	ClaimNextExport(context.Context, time.Time, time.Time) (ProcessingJob, ExportRequest, bool, error)
	CompleteExport(context.Context, string, time.Time) (ProcessingJob, error)
	FailExport(context.Context, string, string, time.Time) error
	GetExportJob(context.Context, string) (ProcessingJob, error)
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

type PlanRequest struct {
	Product Product
	Shots   []Shot
}

type PlanResult struct {
	Caption        string
	CTA            string
	Hashtags       []string
	ProductionSpec ProductionSpec
	Roles          []RoleExecution
	Provider       string
	Units          int64
	CostMicros     int64
}

// ProductionPlanner uses one shared language-model runtime with role-specific
// prompts and schemas. Implementations must not start one model per role.
type ProductionPlanner interface {
	Plan(context.Context, PlanRequest) (PlanResult, error)
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

type ExportRequest struct {
	MissionID      string
	IdempotencyKey string
	Assets         []Asset
	Draft          Draft
	OutputKey      string
}

// ExportQueue is implemented by an idempotent FFmpeg worker queue. Enqueue
// returns the same job for the same idempotency key across retries.
type ExportQueue interface {
	EnqueueExport(context.Context, ExportRequest) (ProcessingJob, error)
	GetExport(context.Context, string) (ProcessingJob, error)
}

type VisualQCRequest struct {
	MissionID      string
	IdempotencyKey string
	VideoKey       string
	Spec           ProductionSpec
}

// VisualQCQueue is deliberately optional and load-on-demand. A ShotVL worker
// may implement it, but the interactive Qwen runtime and First Post flow never
// depend on ShotVL being resident or available.
type VisualQCQueue interface {
	EnqueueVisualQC(context.Context, VisualQCRequest) (ProcessingJob, error)
}

type AuditSink interface {
	RecordAudit(context.Context, AuditEvent) error
	RecordCost(context.Context, CostEntry) error
}
