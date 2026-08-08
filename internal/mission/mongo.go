package mission

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoStore is the durable mission repository, audit/cost ledger and job
// queue. State transitions use version compare-and-swap and job idempotency is
// enforced by a unique index.
type MongoStore struct {
	missions    *mongo.Collection
	audits      *mongo.Collection
	costs       *mongo.Collection
	jobs        *mongo.Collection
	visualLocks *mongo.Collection
	now         func() time.Time
	id          IDGenerator
}

func NewMongoStore(database *mongo.Database, now func() time.Time, id IDGenerator) *MongoStore {
	return &MongoStore{missions: database.Collection("missions"), audits: database.Collection("mission_audit"), costs: database.Collection("ai_cost_ledger"), jobs: database.Collection("processing_jobs"), visualLocks: database.Collection("visual_qc_locks"), now: now, id: id}
}

func (s *MongoStore) EnsureIndexes(ctx context.Context) error {
	_, err := s.missions.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("mission_id_unique")})
	if err != nil {
		return fmt.Errorf("mission index: %w", err)
	}
	_, err = s.jobs.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "idempotencykey", Value: 1}}, Options: options.Index().SetUnique(true).SetName("job_idempotency_unique")})
	if err != nil {
		return fmt.Errorf("job index: %w", err)
	}
	_, err = s.jobs.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "kind", Value: 1}, {Key: "state", Value: 1}, {Key: "updatedat", Value: 1}}, Options: options.Index().SetName("job_worker_queue")})
	if err != nil {
		return fmt.Errorf("job worker index: %w", err)
	}
	_, err = s.audits.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "missionid", Value: 1}, {Key: "action", Value: 1}, {Key: "at", Value: 1}}, Options: options.Index().SetName("activation_funnel")})
	if err != nil {
		return fmt.Errorf("activation event index: %w", err)
	}
	return nil
}

func (s *MongoStore) Create(ctx context.Context, value Mission) error {
	_, err := s.missions.InsertOne(ctx, value)
	if mongo.IsDuplicateKeyError(err) {
		return ErrVersionConflict
	}
	return err
}

func (s *MongoStore) Get(ctx context.Context, id string) (Mission, error) {
	var value Mission
	err := s.missions.FindOne(ctx, bson.M{"id": id}).Decode(&value)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Mission{}, ErrNotFound
	}
	return value, err
}

func (s *MongoStore) Save(ctx context.Context, value Mission, expected int64) error {
	value.Version = expected + 1
	result, err := s.missions.ReplaceOne(ctx, bson.M{"id": value.ID, "version": expected}, value)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		if _, getErr := s.Get(ctx, value.ID); errors.Is(getErr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrVersionConflict
	}
	return nil
}

func (s *MongoStore) RecordAudit(ctx context.Context, value AuditEvent) error {
	_, err := s.audits.InsertOne(ctx, value)
	return err
}

func (s *MongoStore) RecordCost(ctx context.Context, value CostEntry) error {
	_, err := s.costs.InsertOne(ctx, value)
	return err
}

func (s *MongoStore) EnqueueExport(ctx context.Context, request ExportRequest) (ProcessingJob, error) {
	job, _, err := s.ClaimExport(ctx, request)
	return job, err
}

type storedProcessingJob struct {
	ProcessingJob `bson:",inline"`
	Request       ExportRequest    `bson:"request"`
	LeaseUntil    time.Time        `bson:"leaseuntil,omitempty"`
	VisualRequest *VisualQCRequest `bson:"visualrequest,omitempty"`
	VisualResult  *VisualQCReport  `bson:"visualresult,omitempty"`
}

func (s *MongoStore) ClaimNextExport(ctx context.Context, now, leaseUntil time.Time) (ProcessingJob, ExportRequest, bool, error) {
	if _, err := s.jobs.UpdateMany(ctx, bson.M{"kind": "ffmpegExport", "state": JobRunning, "leaseuntil": bson.M{"$lte": now}, "attempt": bson.M{"$gte": 3}}, bson.M{"$set": bson.M{"state": JobFailed, "updatedat": now, "lasterror": "maximum export attempts exceeded"}}); err != nil {
		return ProcessingJob{}, ExportRequest{}, false, err
	}
	filter := bson.M{"kind": "ffmpegExport", "attempt": bson.M{"$lt": 3}, "$or": bson.A{bson.M{"state": JobQueued}, bson.M{"state": JobRunning, "leaseuntil": bson.M{"$lte": now}}}}
	update := bson.M{"$set": bson.M{"state": JobRunning, "leaseuntil": leaseUntil, "updatedat": now}, "$inc": bson.M{"attempt": 1}}
	var stored storedProcessingJob
	err := s.jobs.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetSort(bson.D{{Key: "updatedat", Value: 1}}).SetReturnDocument(options.After)).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ProcessingJob{}, ExportRequest{}, false, nil
	}
	if err != nil {
		return ProcessingJob{}, ExportRequest{}, false, err
	}
	return stored.ProcessingJob, stored.Request, true, nil
}

func (s *MongoStore) ClaimExport(ctx context.Context, request ExportRequest) (ProcessingJob, bool, error) {
	job := ProcessingJob{ID: s.id(), Kind: "ffmpegExport", State: JobQueued, IdempotencyKey: request.IdempotencyKey, UpdatedAt: s.now().UTC()}
	document := storedProcessingJob{ProcessingJob: job, Request: request}
	_, err := s.jobs.InsertOne(ctx, document)
	if mongo.IsDuplicateKeyError(err) {
		var existing storedProcessingJob
		if findErr := s.jobs.FindOne(ctx, bson.M{"idempotencykey": request.IdempotencyKey}).Decode(&existing); findErr != nil {
			return ProcessingJob{}, false, findErr
		}
		if existing.State == JobFailed {
			if existing.Attempt >= 3 {
				return existing.ProcessingJob, false, nil
			}
			var retried storedProcessingJob
			updateErr := s.jobs.FindOneAndUpdate(ctx, bson.M{"id": existing.ID, "state": JobFailed}, bson.M{"$set": bson.M{"state": JobQueued, "updatedat": s.now().UTC(), "lasterror": ""}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&retried)
			if updateErr == nil {
				return retried.ProcessingJob, true, nil
			}
			if !errors.Is(updateErr, mongo.ErrNoDocuments) {
				return ProcessingJob{}, false, updateErr
			}
		}
		return existing.ProcessingJob, false, nil
	}
	return job, err == nil, err
}

func (s *MongoStore) CompleteExport(ctx context.Context, id string, at time.Time) (ProcessingJob, error) {
	var stored storedProcessingJob
	err := s.jobs.FindOneAndUpdate(ctx, bson.M{"id": id, "kind": "ffmpegExport", "state": bson.M{"$in": bson.A{JobQueued, JobRunning}}}, bson.M{"$set": bson.M{"state": JobSucceeded, "updatedat": at, "lasterror": ""}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ProcessingJob{}, ErrVersionConflict
	}
	return stored.ProcessingJob, err
}

func (s *MongoStore) FailExport(ctx context.Context, id, message string, at time.Time) error {
	_, err := s.jobs.UpdateOne(ctx, bson.M{"id": id, "kind": "ffmpegExport"}, bson.M{"$set": bson.M{"state": JobFailed, "updatedat": at, "lasterror": message}})
	return err
}

func (s *MongoStore) GetExportJob(ctx context.Context, id string) (ProcessingJob, error) {
	var stored storedProcessingJob
	err := s.jobs.FindOne(ctx, bson.M{"id": id, "kind": "ffmpegExport"}).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ProcessingJob{}, ErrNotFound
	}
	return stored.ProcessingJob, err
}

func (s *MongoStore) EnqueueVisualQCJob(ctx context.Context, request VisualQCRequest) (ProcessingJob, error) {
	job := ProcessingJob{ID: s.id(), Kind: "visualQc", State: JobQueued, IdempotencyKey: request.IdempotencyKey, UpdatedAt: s.now().UTC()}
	_, err := s.jobs.InsertOne(ctx, storedProcessingJob{ProcessingJob: job, VisualRequest: &request})
	if mongo.IsDuplicateKeyError(err) {
		var existing storedProcessingJob
		if findErr := s.jobs.FindOne(ctx, bson.M{"idempotencykey": request.IdempotencyKey}).Decode(&existing); findErr != nil {
			return ProcessingJob{}, findErr
		}
		if existing.State == JobFailed && existing.Attempt < 3 {
			var retried storedProcessingJob
			updateErr := s.jobs.FindOneAndUpdate(ctx, bson.M{"id": existing.ID, "state": JobFailed}, bson.M{"$set": bson.M{"state": JobQueued, "updatedat": s.now().UTC(), "lasterror": ""}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&retried)
			if updateErr == nil {
				return retried.ProcessingJob, nil
			}
		}
		return existing.ProcessingJob, nil
	}
	return job, err
}

func (s *MongoStore) ClaimNextVisualQC(ctx context.Context, workerID string, now, leaseUntil time.Time) (ProcessingJob, VisualQCRequest, bool, error) {
	// One global renewable lease keeps ShotVL concurrency at one even when the
	// API has multiple replicas.
	lockFilter := bson.M{"_id": "shotvl-global", "$or": bson.A{bson.M{"leaseuntil": bson.M{"$lte": now}}, bson.M{"owner": workerID}}}
	lockUpdate := bson.M{"$set": bson.M{"owner": workerID, "leaseuntil": leaseUntil}}
	var lock bson.M
	err := s.visualLocks.FindOneAndUpdate(ctx, lockFilter, lockUpdate, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&lock)
	if mongo.IsDuplicateKeyError(err) || errors.Is(err, mongo.ErrNoDocuments) {
		return ProcessingJob{}, VisualQCRequest{}, false, nil
	}
	if err != nil {
		return ProcessingJob{}, VisualQCRequest{}, false, err
	}
	if _, err := s.jobs.UpdateMany(ctx, bson.M{"kind": "visualQc", "state": JobRunning, "leaseuntil": bson.M{"$lte": now}, "attempt": bson.M{"$gte": 3}}, bson.M{"$set": bson.M{"state": JobFailed, "updatedat": now, "lasterror": "maximum visual QC attempts exceeded"}}); err != nil {
		return ProcessingJob{}, VisualQCRequest{}, false, err
	}
	filter := bson.M{"kind": "visualQc", "attempt": bson.M{"$lt": 3}, "$or": bson.A{bson.M{"state": JobQueued}, bson.M{"state": JobRunning, "leaseuntil": bson.M{"$lte": now}}}}
	update := bson.M{"$set": bson.M{"state": JobRunning, "leaseuntil": leaseUntil, "updatedat": now}, "$inc": bson.M{"attempt": 1}}
	var stored storedProcessingJob
	err = s.jobs.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetSort(bson.D{{Key: "updatedat", Value: 1}}).SetReturnDocument(options.After)).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		_, _ = s.visualLocks.DeleteOne(ctx, bson.M{"_id": "shotvl-global", "owner": workerID})
		return ProcessingJob{}, VisualQCRequest{}, false, nil
	}
	if err != nil {
		return ProcessingJob{}, VisualQCRequest{}, false, err
	}
	if stored.VisualRequest == nil {
		return ProcessingJob{}, VisualQCRequest{}, false, fmt.Errorf("visual QC job has no request")
	}
	return stored.ProcessingJob, *stored.VisualRequest, true, nil
}

func (s *MongoStore) CompleteVisualQC(ctx context.Context, id, workerID string, report VisualQCReport, at time.Time) (ProcessingJob, error) {
	defer func() {
		_, _ = s.visualLocks.DeleteOne(context.WithoutCancel(ctx), bson.M{"_id": "shotvl-global", "owner": workerID})
	}()
	var stored storedProcessingJob
	err := s.jobs.FindOneAndUpdate(ctx, bson.M{"id": id, "kind": "visualQc", "state": JobRunning}, bson.M{"$set": bson.M{"state": JobSucceeded, "updatedat": at, "visualresult": report, "lasterror": ""}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ProcessingJob{}, ErrVersionConflict
	}
	return stored.ProcessingJob, err
}

func (s *MongoStore) FailVisualQC(ctx context.Context, id, workerID, message string, at time.Time) error {
	defer func() {
		_, _ = s.visualLocks.DeleteOne(context.WithoutCancel(ctx), bson.M{"_id": "shotvl-global", "owner": workerID})
	}()
	_, err := s.jobs.UpdateOne(ctx, bson.M{"id": id, "kind": "visualQc"}, bson.M{"$set": bson.M{"state": JobFailed, "updatedat": at, "lasterror": message}})
	return err
}

func (s *MongoStore) GetVisualQCJob(ctx context.Context, id string) (ProcessingJob, *VisualQCReport, error) {
	var stored storedProcessingJob
	err := s.jobs.FindOne(ctx, bson.M{"id": id, "kind": "visualQc"}).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ProcessingJob{}, nil, ErrNotFound
	}
	return stored.ProcessingJob, stored.VisualResult, err
}
