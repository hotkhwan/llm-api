package mission

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

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
	if len(ledger.Costs) != 1 || ledger.Costs[0].CostMicros != 0 {
		t.Fatalf("cost ledger = %#v", ledger.Costs)
	}
	if len(ledger.Audits) != 7 {
		t.Fatalf("audit count = %d, want 7", len(ledger.Audits))
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
	if _, err := service.Upload(ctx, m.ID, 1, "image/jpeg", append(photo, 0x01)); err == nil {
		t.Fatal("accepted duplicate shot")
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
