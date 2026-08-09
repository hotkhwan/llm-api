package mission

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestCreateMissionLocaleDefaultsValidatesAndPersists(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, MemoryObjectStore{}, FallbackCaptioner{}, &MemoryLedger{}, time.Now, func() string { return "localized" })
	thai, err := service.CreateWithConsentAndLocale(context.Background(), "u", Product{Name: "สินค้า", Description: "ข้อมูลจริง"}, "v1", "")
	if err != nil || thai.Locale != LocaleThai || thai.Shots[0].Instruction != "ถ่ายภาพหรือคลิปก่อนใช้สินค้า" {
		t.Fatalf("default locale mission=%#v err=%v", thai, err)
	}
	service = NewService(NewMemoryRepository(), MemoryObjectStore{}, FallbackCaptioner{}, &MemoryLedger{}, time.Now, func() string { return "english" })
	english, err := service.CreateWithConsentAndLocale(context.Background(), "u", Product{Name: "Box", Description: "Stores small items"}, "v1", LocaleEnglish)
	if err != nil || english.Locale != LocaleEnglish || english.Shots[0].Instruction != "Capture a photo or video before using the product" {
		t.Fatalf("English locale mission=%#v err=%v", english, err)
	}
	if _, err := service.CreateWithConsentAndLocale(context.Background(), "u", Product{Name: "Box", Description: "Stores items"}, "v1", "fr"); err == nil {
		t.Fatal("unsupported locale accepted")
	}
}

func TestFirstMissionFlow(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	ledger := &MemoryLedger{}
	service := NewService(NewMemoryRepository(), MemoryObjectStore{}, FallbackCaptioner{}, ledger, func() time.Time { return now }, func() string { return "mission-1" })
	ctx := context.Background()
	m, err := service.Create(ctx, "user-1", Product{Name: "กล่องจัดของเล่น", Description: "กล่องพับได้สำหรับจัดของเล่น"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.State != StateMissionAccepted || len(m.Shots) != 3 {
		t.Fatalf("created mission = %#v", m)
	}
	assetBody := []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00}
	if _, err := service.UploadProductReference(ctx, m.ID, 1, "image/jpeg", assetBody); err != nil {
		t.Fatalf("product reference: %v", err)
	}
	for _, shot := range []int{3, 1, 2} {
		m, err = service.Upload(ctx, m.ID, shot, "image/jpeg", assetBody)
		if err != nil {
			t.Fatalf("upload shot %d: %v", shot, err)
		}
	}
	if m.State != StateAssetsUploaded || len(m.Assets) != 3 {
		t.Fatalf("uploaded mission = %#v", m)
	}
	versionAfterUpload := m.Version
	m, err = service.Upload(ctx, m.ID, 2, "image/jpeg", assetBody)
	if err != nil || m.Version != versionAfterUpload {
		t.Fatalf("idempotent upload: version=%d err=%v", m.Version, err)
	}
	m, err = service.GenerateDraft(ctx, m.ID)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if m.State != StateDraftReady || m.Draft == nil || m.Draft.GeneratedBy != "deterministic-fallback" || len(m.Draft.Timeline) != 3 {
		t.Fatalf("draft mission = %#v", m)
	}
	if m.Draft.ProductionSpec.SchemaVersion != ProductionSpecSchemaVersion || len(m.Draft.ProductionSpec.Shots) != 3 || len(m.Draft.RoleExecutions) != 5 {
		t.Fatalf("canonical production spec = %#v", m.Draft)
	}
	for index, clip := range m.Draft.Timeline {
		if clip.Shot != index+1 {
			t.Fatalf("timeline order = %#v", m.Draft.Timeline)
		}
	}
	versionAfterDraft := m.Version
	m, err = service.GenerateDraft(ctx, m.ID)
	if err != nil || m.Version != versionAfterDraft {
		t.Fatalf("idempotent draft: version=%d err=%v", m.Version, err)
	}
	m, err = service.Export(ctx, m.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	versionAfterExport := m.Version
	m, err = service.Export(ctx, m.ID)
	if err != nil || m.Version != versionAfterExport {
		t.Fatalf("idempotent export: version=%d err=%v", m.Version, err)
	}
	if m.State != StateExported || m.Export == nil || m.Export.Format != "video/mp4" {
		t.Fatalf("exported mission = %#v", m)
	}
	m, err = service.MarkPosted(ctx, m.ID, "TikTok", "https://www.tiktok.com/@example/video/1")
	if err != nil {
		t.Fatalf("mark posted: %v", err)
	}
	versionAfterPost := m.Version
	m, err = service.MarkPosted(ctx, m.ID, "TIKTOK", "https://www.tiktok.com/@example/video/1")
	if err != nil || m.Version != versionAfterPost {
		t.Fatalf("idempotent post: version=%d err=%v", m.Version, err)
	}
	if m.State != StatePosted || m.Posted == nil || m.Posted.Platform != "tiktok" {
		t.Fatalf("posted mission = %#v", m)
	}
	m, err = service.RecordOutcome(ctx, m.ID, 100, 10, 1)
	if err != nil || m.State != StateNextMissionReady || m.NextAction == nil || m.NextAction.Kind != "repeat" {
		t.Fatalf("outcome mission=%#v err=%v", m, err)
	}
	if len(ledger.Costs) != 1 || ledger.Costs[0].CostMicros != 0 {
		t.Fatalf("cost ledger = %#v", ledger.Costs)
	}
	if len(ledger.Audits) != 9 {
		t.Fatalf("audit count = %d, want 9", len(ledger.Audits))
	}
}

func TestDraftRequiresProductReference(t *testing.T) {
	service := NewService(NewMemoryRepository(), MemoryObjectStore{}, FallbackCaptioner{}, &MemoryLedger{}, time.Now, func() string { return "id" })
	m, err := service.Create(context.Background(), "u", Product{Name: "สินค้า", Description: "ข้อมูลจริง"})
	if err != nil {
		t.Fatal(err)
	}
	photo := []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00}
	for shot := 1; shot <= 3; shot++ {
		if _, err := service.Upload(context.Background(), m.ID, shot, "image/jpeg", photo); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.GenerateDraft(context.Background(), m.ID); err == nil {
		t.Fatal("draft accepted without product reference")
	}
}

func TestProductReferencePutIsIdempotentAndReplaceableBeforeDraft(t *testing.T) {
	service := NewService(NewMemoryRepository(), MemoryObjectStore{}, FallbackCaptioner{}, &MemoryLedger{}, time.Now, func() string { return "id" })
	m, err := service.Create(context.Background(), "u", Product{Name: "สินค้า", Description: "ข้อมูลจริง"})
	if err != nil {
		t.Fatal(err)
	}
	first := []byte{0xFF, 0xD8, 0xFF, 0xDB, 0, 1}
	m, err = service.UploadProductReference(context.Background(), m.ID, 1, "image/jpeg", first)
	if err != nil {
		t.Fatal(err)
	}
	version := m.Version
	m, err = service.UploadProductReference(context.Background(), m.ID, 1, "image/jpeg", first)
	if err != nil || m.Version != version {
		t.Fatalf("idempotent reference version=%d err=%v", m.Version, err)
	}
	second := append(append([]byte(nil), first...), 2)
	m, err = service.UploadProductReference(context.Background(), m.ID, 1, "image/jpeg", second)
	if err != nil || m.Version != version+1 || len(m.ProductReferences) != 1 {
		t.Fatalf("replace reference mission=%#v err=%v", m, err)
	}
}

func TestOutcomeValidation(t *testing.T) {
	service := NewService(NewMemoryRepository(), MemoryObjectStore{}, FallbackCaptioner{}, &MemoryLedger{}, time.Now, func() string { return "id" })
	if _, err := service.RecordOutcome(context.Background(), "id", 1, 2, 0); err == nil {
		t.Fatal("accepted clicks greater than views")
	}
}

func TestFirstMissionRejectsOutOfOrderAndDuplicateActions(t *testing.T) {
	service := NewService(NewMemoryRepository(), MemoryObjectStore{}, FallbackCaptioner{}, &MemoryLedger{}, time.Now, func() string { return "mission-1" })
	ctx := context.Background()
	m, err := service.Create(ctx, "user-1", Product{Name: "สินค้า", Description: "ข้อมูลจริง"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GenerateDraft(ctx, m.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("draft before capture error = %v", err)
	}
	if _, err := service.Upload(ctx, m.ID, 1, "text/plain", []byte("bad")); err == nil {
		t.Fatal("accepted non-media upload")
	}
	if _, err := service.Upload(ctx, m.ID, 1, "image/jpeg", []byte("plain text")); err == nil {
		t.Fatal("accepted media type that did not match its bytes")
	}
	photo := []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00}
	if _, err := service.Upload(ctx, m.ID, 1, "image/jpeg", photo); err != nil {
		t.Fatal(err)
	}
	replaced, err := service.Upload(ctx, m.ID, 1, "image/jpeg", append(photo, 0x01))
	if err != nil || len(replaced.Assets) != 1 || replaced.Assets[0].SHA256 == "" {
		t.Fatalf("replace shot: mission=%#v err=%v", replaced, err)
	}
}

type failingCaptioner struct{}

func (failingCaptioner) Generate(context.Context, CaptionRequest) (CaptionResult, error) {
	return CaptionResult{}, errors.New("model unavailable")
}

func TestCaptionFallsBackWhenLocalModelUnavailable(t *testing.T) {
	result, err := (FallbackCaptioner{Primary: failingCaptioner{}}).Generate(context.Background(), CaptionRequest{Product: Product{Name: "สินค้า"}})
	if err != nil || result.Provider != "deterministic-fallback" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

type prohibitedCaptioner struct{}

func (prohibitedCaptioner) Generate(context.Context, CaptionRequest) (CaptionResult, error) {
	return CaptionResult{Caption: "รับประกันรายได้แน่นอน", Provider: "unsafe"}, nil
}

func TestCaptionFallsBackForProhibitedIncomeClaim(t *testing.T) {
	result, err := (FallbackCaptioner{Primary: prohibitedCaptioner{}}).Generate(context.Background(), CaptionRequest{Product: Product{Name: "สินค้า"}})
	if err != nil || result.Provider != "deterministic-fallback" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestMemoryRepositoryRejectsStaleSave(t *testing.T) {
	repo := NewMemoryRepository()
	m := Mission{ID: "m1", Version: 1}
	if err := repo.Create(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	m.State = StateCaptureStarted
	if err := repo.Save(context.Background(), m, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), m, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale save error = %v", err)
	}
}

type inconsistentObjectStore struct{}

func (inconsistentObjectStore) Put(context.Context, string, string, io.Reader) (ObjectMetadata, error) {
	return ObjectMetadata{Key: "wrong/key", ContentType: "image/jpeg", Bytes: 7, SHA256: "wrong"}, nil
}

type statusExportQueue struct{ job ProcessingJob }

func (q *statusExportQueue) EnqueueExport(context.Context, ExportRequest) (ProcessingJob, error) {
	return q.job, nil
}
func (q *statusExportQueue) GetExport(context.Context, string) (ProcessingJob, error) {
	return q.job, nil
}

type signingMemoryStore struct{ MemoryObjectStore }

func (signingMemoryStore) DownloadURL(context.Context, string, time.Duration) (string, error) {
	return "https://s3.example/download", nil
}

type completedVisualQueue struct{ report VisualQCReport }

func (q completedVisualQueue) EnqueueVisualQC(context.Context, VisualQCRequest) (ProcessingJob, error) {
	return ProcessingJob{ID: "vj", Kind: "visualQc", State: JobSucceeded}, nil
}
func (q completedVisualQueue) GetVisualQC(context.Context, string) (ProcessingJob, *VisualQCReport, error) {
	report := q.report
	return ProcessingJob{ID: "vj", Kind: "visualQc", State: JobSucceeded}, &report, nil
}

func TestGetReconcilesCompletedExportAndRenewsDownloadURL(t *testing.T) {
	repo := NewMemoryRepository()
	now := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)
	m := Mission{ID: "m1", UserID: "u", State: StateExportQueued, Version: 1, ExportJob: &ProcessingJob{ID: "j1", State: JobQueued}, Export: &Export{StorageKey: "missions/m1/exports/first-post.mp4"}, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	queue := &statusExportQueue{job: ProcessingJob{ID: "j1", State: JobSucceeded}}
	service := NewServiceWithOptions(repo, signingMemoryStore{}, CaptionBackedPlanner{}, queue, nil, &MemoryLedger{}, func() time.Time { return now }, func() string { return "id" }, 1024)
	got, err := service.Get(context.Background(), m.ID)
	if err != nil || got.State != StateExported || got.Export.DownloadURL == "" {
		t.Fatalf("mission=%#v err=%v", got, err)
	}
	version := got.Version
	got, err = service.Get(context.Background(), m.ID)
	if err != nil || got.Version != version || got.Export.DownloadURL == "" {
		t.Fatalf("renew mission=%#v err=%v", got, err)
	}
}

func TestVisualQCManualOverrideDoesNotBlockPostedExport(t *testing.T) {
	repo := NewMemoryRepository()
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	m := Mission{ID: "m1", UserID: "u", State: StateExported, Version: 1, Export: &Export{StorageKey: "video"}, ExportJob: &ProcessingJob{ID: "e1", State: JobSucceeded}, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithOptions(repo, MemoryObjectStore{}, CaptionBackedPlanner{}, NewMemoryJobQueue(func() time.Time { return now }, func() string { return "id" }), nil, &MemoryLedger{}, func() time.Time { return now }, func() string { return "id" }, 1024)
	got, err := service.OverrideVisualQC(context.Background(), m.ID, "u", "accept", "ตรวจวิดีโอด้วยตนเองแล้ว")
	if err != nil || got.State != StateExported || got.VisualQC == nil || got.VisualQC.ManualOverride == nil || got.VisualQC.ManualOverride.Decision != "accept" {
		t.Fatalf("mission=%#v err=%v", got, err)
	}
	got, err = service.MarkPosted(context.Background(), m.ID, "tiktok", "")
	if err != nil || got.State != StatePosted {
		t.Fatalf("posted mission=%#v err=%v", got, err)
	}
}

func TestPostedMissionReconcilesVisualQCAndRenewsDownloadURL(t *testing.T) {
	repo := NewMemoryRepository()
	now := time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)
	report := VisualQCReport{Revision: 1, ModelRevision: "shotvl-rev", Score: .8, Threshold: .75, Passed: true, CreatedAt: now}
	m := Mission{ID: "m1", UserID: "u", State: StatePosted, Version: 1, Export: &Export{StorageKey: "video"}, ExportJob: &ProcessingJob{ID: "e1", State: JobSucceeded}, VisualQC: &VisualQCState{Job: &ProcessingJob{ID: "vj", Kind: "visualQc", State: JobQueued}, History: []VisualQCReport{}}, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithOptions(repo, signingMemoryStore{}, CaptionBackedPlanner{}, NewMemoryJobQueue(func() time.Time { return now }, func() string { return "id" }), completedVisualQueue{report: report}, &MemoryLedger{}, func() time.Time { return now }, func() string { return "id" }, 1024)
	got, err := service.Get(context.Background(), m.ID)
	if err != nil || got.State != StatePosted || got.Export.DownloadURL == "" || got.VisualQC.LatestReport == nil || len(got.VisualQC.History) != 1 {
		t.Fatalf("mission=%#v err=%v", got, err)
	}
}

func TestUploadRejectsInconsistentObjectStoreMetadata(t *testing.T) {
	service := NewService(NewMemoryRepository(), inconsistentObjectStore{}, FallbackCaptioner{}, &MemoryLedger{}, time.Now, func() string { return "mission-1" })
	m, err := service.Create(context.Background(), "user-1", Product{Name: "สินค้า", Description: "ข้อมูลจริง"})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00}
	if _, err := service.Upload(context.Background(), m.ID, 1, "image/jpeg", body); err == nil {
		t.Fatal("accepted inconsistent object metadata")
	}
	stored, err := service.Get(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Assets) != 0 || stored.State != StateMissionAccepted {
		t.Fatalf("mission changed after rejected store metadata: %#v", stored)
	}
}
