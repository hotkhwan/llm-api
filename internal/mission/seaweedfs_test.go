package mission

import (
	"context"
	"net/url"
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
