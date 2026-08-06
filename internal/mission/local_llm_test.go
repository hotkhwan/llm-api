package mission

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleCaptioner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"caption\":\"ลองใช้จริง\",\"cta\":\"ดูสินค้า\",\"hashtags\":[\"#ลองแล้ว\"]}"}}],"usage":{"total_tokens":42}}`))
	}))
	defer server.Close()
	result, err := (OpenAICompatibleCaptioner{Endpoint: server.URL + "/v1", Model: "test", Client: server.Client()}).Generate(context.Background(), CaptionRequest{Product: Product{Name: "สินค้า", Description: "ข้อมูลจริง"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Caption != "ลองใช้จริง" || result.Provider != "local-openai-compatible" || result.Units != 42 {
		t.Fatalf("result = %#v", result)
	}
}
