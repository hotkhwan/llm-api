package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hotkhwan/llm-api/internal/buildinfo"
)

func testOptions() Options {
	return Options{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		BodyLimit:    1 << 20,
		Concurrency:  1024,
		RequestID: func() (string, error) {
			return "generated-request-id", nil
		},
	}
}

func TestLifecycleAndVersionEndpoints(t *testing.T) {
	readiness := NewReadiness()
	app := New(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), buildinfo.Static{
		Version: "1.2.3",
		Commit:  "abc123",
	}, readiness, testOptions())

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

func TestServerResourcePolicy(t *testing.T) {
	options := testOptions()
	app := New(slog.Default(), buildinfo.Static{}, NewReadiness(), options)
	got := app.Config()
	if got.ReadTimeout != options.ReadTimeout || got.WriteTimeout != options.WriteTimeout ||
		got.IdleTimeout != options.IdleTimeout || got.BodyLimit != options.BodyLimit || got.Concurrency != options.Concurrency {
		t.Fatalf("Fiber config does not match resource policy: %#v", got)
	}
}

func TestRequestLogUsesFinalErrorHandlerStatus(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		register   func(*fiber.App)
		wantStatus int
	}{
		{name: "not found", method: "GET", path: "/missing", wantStatus: 404},
		{name: "method not allowed", method: "POST", path: "/healthz", wantStatus: 405},
		{
			name:   "handler error",
			method: "GET",
			path:   "/handler-error",
			register: func(app *fiber.App) {
				app.Get("/handler-error", func(*fiber.Ctx) error {
					return fiber.NewError(fiber.StatusTeapot, "teapot")
				})
			},
			wantStatus: fiber.StatusTeapot,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			app := New(slog.New(slog.NewJSONHandler(&logs, nil)), buildinfo.Static{}, NewReadiness(), testOptions())
			if test.register != nil {
				test.register(app)
			}
			response, err := app.Test(httptest.NewRequest(test.method, test.path, nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if response.StatusCode != test.wantStatus {
				t.Fatalf("response status = %d, want %d", response.StatusCode, test.wantStatus)
			}
			entry := decodeLog(t, &logs)
			if got := int(entry["status"].(float64)); got != test.wantStatus {
				t.Fatalf("logged status = %d, want %d; log=%#v", got, test.wantStatus, entry)
			}
			if entry["method"] != test.method || entry["path"] != test.path || entry["msg"] != "request completed" {
				t.Fatalf("unexpected structured log: %#v", entry)
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	app := New(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), buildinfo.Static{}, NewReadiness(), testOptions())

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
	if got := response.Header.Get(requestIDHeader); got != "generated-request-id" {
		t.Fatalf("request ID = %q, want deterministic generated ID", got)
	}
}

func TestRequestIDGenerationFailsClosed(t *testing.T) {
	options := testOptions()
	options.RequestID = func() (string, error) { return "", errors.New("entropy unavailable") }
	var logs bytes.Buffer
	app := New(slog.New(slog.NewJSONHandler(&logs, nil)), buildinfo.Static{}, NewReadiness(), options)

	response, err := app.Test(httptest.NewRequest("GET", "/healthz", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusServiceUnavailable)
	}
	entry := decodeLog(t, &logs)
	if got := int(entry["status"].(float64)); got != fiber.StatusServiceUnavailable {
		t.Fatalf("logged status = %d, want %d", got, fiber.StatusServiceUnavailable)
	}
}

func decodeLog(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.NewDecoder(logs).Decode(&entry); err != nil {
		t.Fatalf("decode structured log: %v; raw=%q", err, logs.String())
	}
	return entry
}
