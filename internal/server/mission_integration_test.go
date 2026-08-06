package server

import (
	"bytes"
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
	response := missionRequest(t, app, "POST", "/v1/missions", "application/json", []byte(`{"userId":"u1","product":{"name":"กล่อง","description":"ใช้จัดของ"}}`))
	if response.StatusCode != 201 {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	for shot := 1; shot <= 3; shot++ {
		response = missionRequest(t, app, "PUT", fmt.Sprintf("/v1/missions/mission-http-1/assets/%d", shot), "video/mp4", []byte("clip"))
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
}

func missionRequest(t *testing.T, app interface {
	Test(*http.Request, ...int) (*http.Response, error)
}, method, path, contentType string, body []byte) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
