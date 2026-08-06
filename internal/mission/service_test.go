package mission

import (
	"context"
	"errors"
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
	for shot := 1; shot <= 3; shot++ {
		m, err = service.Upload(ctx, m.ID, shot, "video/mp4", []byte("clip"))
		if err != nil {
			t.Fatalf("upload shot %d: %v", shot, err)
		}
	}
	if m.State != StateAssetsUploaded || len(m.Assets) != 3 {
		t.Fatalf("uploaded mission = %#v", m)
	}
	m, err = service.GenerateDraft(ctx, m.ID)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if m.State != StateDraftReady || m.Draft == nil || m.Draft.GeneratedBy != "deterministic-fallback" || len(m.Draft.Timeline) != 3 {
		t.Fatalf("draft mission = %#v", m)
	}
	m, err = service.Export(ctx, m.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if m.State != StateExported || m.Export == nil || m.Export.Format != "video/mp4" {
		t.Fatalf("exported mission = %#v", m)
	}
	m, err = service.MarkPosted(ctx, m.ID, "TikTok", "https://www.tiktok.com/@example/video/1")
	if err != nil {
		t.Fatalf("mark posted: %v", err)
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
	if _, err := service.Upload(ctx, m.ID, 1, "image/jpeg", []byte("photo")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Upload(ctx, m.ID, 1, "image/jpeg", []byte("duplicate")); err == nil {
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
