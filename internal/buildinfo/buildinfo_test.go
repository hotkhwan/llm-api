package buildinfo

import "testing"

func TestStaticMetadata(t *testing.T) {
	provider := Static{Version: "1.2.3", Commit: "abc123"}
	got := provider.Metadata()
	if got.Version != "1.2.3" || got.Commit != "abc123" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
}
