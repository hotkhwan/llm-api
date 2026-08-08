package mission

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSeaweedFSPresignUsesBrowserPublicEndpoint(t *testing.T) {
	store, err := NewSeaweedFSStore(context.Background(), "http://s3.store.svc.cluster.local:9000", "https://dgx-s3.k-lynx.com", "us-east-1", "llm-api", "mission-zero", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.DownloadURL(context.Background(), "missions/m1/exports/first-post.mp4", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "dgx-s3.k-lynx.com" {
		t.Fatalf("presigned host=%q url=%q", parsed.Host, value)
	}
}

func TestSeaweedFSStoreSpoolsSeekableChecksumUpload(t *testing.T) {
	payload := []byte("product-reference")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/llm-api/mission-zero/missions/m1/product.jpg" {
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		if !bytes.Equal(body, payload) {
			t.Errorf("payload = %q", body)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := NewSeaweedFSStore(context.Background(), server.URL, server.URL, "us-east-1", "llm-api", "mission-zero", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Put(context.Background(), "missions/m1/product.jpg", "image/jpeg", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Bytes != int64(len(payload)) || metadata.ContentType != "image/jpeg" || metadata.SHA256 == "" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestSeaweedFSStoreRejectsOversizedSpool(t *testing.T) {
	store, err := NewSeaweedFSStore(context.Background(), "http://127.0.0.1:1", "http://127.0.0.1:1", "us-east-1", "llm-api", "mission-zero", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	store.maxObjectBytes = 3
	_, err = store.Put(context.Background(), "too-large", "application/octet-stream", strings.NewReader("four"))
	if err == nil || !strings.Contains(err.Error(), "between 1 and 3 bytes") {
		t.Fatalf("expected bounded spool error, got %v", err)
	}
}
