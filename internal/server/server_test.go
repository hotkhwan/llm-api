package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/hotkhwan/affiliate-api/internal/buildinfo"
)

func TestLifecycleAndVersionEndpoints(t *testing.T) {
	readiness := NewReadiness()
	app := New(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), buildinfo.Static{
		Version: "1.2.3",
		Commit:  "abc123",
	}, readiness)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{path: "/healthz", wantStatus: 200},
		{path: "/readyz", wantStatus: 503},
		{path: "/version", wantStatus: 200},
	}
	for _, test := range tests {
		response, err := app.Test(httptest.NewRequest("GET", test.path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", test.path, err)
		}
		if response.StatusCode != test.wantStatus {
			t.Fatalf("GET %s status = %d, want %d", test.path, response.StatusCode, test.wantStatus)
		}
	}

	readiness.Set(true)
	response, err := app.Test(httptest.NewRequest("GET", "/readyz", nil))
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("GET /readyz status = %d, want 200", response.StatusCode)
	}

	response, err = app.Test(httptest.NewRequest("GET", "/version", nil))
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	var got buildinfo.Metadata
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	if got.Version != "1.2.3" || got.Commit != "abc123" {
		t.Fatalf("unexpected version payload: %#v", got)
	}
}

func TestRequestID(t *testing.T) {
	app := New(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), buildinfo.Static{}, NewReadiness())

	request := httptest.NewRequest("GET", "/healthz", nil)
	request.Header.Set(requestIDHeader, "caller-123")
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if got := response.Header.Get(requestIDHeader); got != "caller-123" {
		t.Fatalf("request ID = %q, want caller-123", got)
	}

	request = httptest.NewRequest("GET", "/healthz", nil)
	request.Header.Set(requestIDHeader, "not valid")
	response, err = app.Test(request)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if got := response.Header.Get(requestIDHeader); got == "" || got == "not valid" {
		t.Fatalf("invalid request ID was not replaced: %q", got)
	}
}
