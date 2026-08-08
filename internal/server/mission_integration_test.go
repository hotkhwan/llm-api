package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hotkhwan/llm-api/internal/buildinfo"
	"github.com/hotkhwan/llm-api/internal/mission"
)

func TestMissionHTTPFlow(t *testing.T) {
	service := mission.NewService(mission.NewMemoryRepository(), mission.MemoryObjectStore{}, mission.FallbackCaptioner{}, &mission.MemoryLedger{}, time.Now, func() string { return "mission-http-1" })
	options := testOptions()
	options.Mission = service
	app := New(slog.Default(), buildinfo.Static{}, NewReadiness(), options)
	response := missionRequest(t, app, "POST", "/v1/missions", "application/json", []byte(`{"consentAccepted":true,"privacyNoticeVersion":"2026-08-08","product":{"name":"กล่อง","description":"ใช้จัดของ"}}`))
	if response.StatusCode != 201 {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	response = missionRequest(t, app, "PUT", "/v1/missions/mission-http-1/product-references/1", "image/jpeg", []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00})
	if response.StatusCode != 200 {
		t.Fatalf("product reference status = %d", response.StatusCode)
	}
	for shot := 1; shot <= 3; shot++ {
		response = missionRequest(t, app, "PUT", fmt.Sprintf("/v1/missions/mission-http-1/assets/%d", shot), "image/jpeg", []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00})
		if response.StatusCode != 200 {
			t.Fatalf("upload %d status = %d", shot, response.StatusCode)
		}
	}
	for _, action := range []string{"draft", "export"} {
		response = missionRequest(t, app, "POST", "/v1/missions/mission-http-1/"+action, "application/json", nil)
		if response.StatusCode != 200 {
			t.Fatalf("%s status = %d", action, response.StatusCode)
		}
	}
	response = missionRequest(t, app, "POST", "/v1/missions/mission-http-1/posted", "application/json", []byte(`{"platform":"tiktok"}`))
	if response.StatusCode != 200 {
		t.Fatalf("posted status = %d", response.StatusCode)
	}
	var got mission.Mission
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != mission.StatePosted {
		t.Fatalf("state = %q", got.State)
	}
	response = missionRequest(t, app, "PUT", "/v1/missions/mission-http-1/outcome", "application/json", []byte(`{"views":20,"clicks":2,"sales":0}`))
	if response.StatusCode != 200 {
		t.Fatalf("outcome status = %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != mission.StateNextMissionReady || got.NextAction == nil {
		t.Fatalf("outcome mission=%#v", got)
	}
}

func TestMissionRequiresAuthenticatedIdentity(t *testing.T) {
	service := mission.NewService(mission.NewMemoryRepository(), mission.MemoryObjectStore{}, mission.FallbackCaptioner{}, &mission.MemoryLedger{}, time.Now, func() string { return "m1" })
	options := testOptions()
	options.Mission = service
	app := New(slog.Default(), buildinfo.Static{}, NewReadiness(), options)
	req := httptest.NewRequest("POST", "/v1/missions", bytes.NewBufferString(`{"consentAccepted":true,"privacyNoticeVersion":"v1","product":{"name":"x","description":"y"}}`))
	req.Header.Set("Content-Type", "application/json")
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestMissionDoesNotRevealAnotherUsersMission(t *testing.T) {
	repo := mission.NewMemoryRepository()
	service := mission.NewService(repo, mission.MemoryObjectStore{}, mission.FallbackCaptioner{}, &mission.MemoryLedger{}, time.Now, func() string { return "private-mission" })
	if _, err := service.CreateWithConsent(context.Background(), "owner", mission.Product{Name: "x", Description: "y"}, "v1"); err != nil {
		t.Fatal(err)
	}
	options := testOptions()
	options.Mission = service
	app := New(slog.Default(), buildinfo.Static{}, NewReadiness(), options)
	req := httptest.NewRequest("GET", "/v1/missions/private-mission", nil)
	req.Header.Set("X-Authenticated-User-ID", "other-user")
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func missionRequest(t *testing.T, app interface {
	Test(*http.Request, ...int) (*http.Response, error)
}, method, path, contentType string, body []byte) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Authenticated-User-ID", "u1")
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
