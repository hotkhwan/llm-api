package mission

import (
	"context"
	"encoding/json"
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

func TestOpenAICompatiblePlannerUsesOneRuntimeAndBearerSecret(t *testing.T) {
	product := Product{Name: "กล่อง", Description: "กล่องสีขาว"}
	guides := []Shot{{1, "ก่อน"}, {2, "ใช้"}, {3, "หลัง"}}
	spec := deterministicProductionSpec(product, guides)
	content, _ := json.Marshal(map[string]any{"caption": "ลองกล่องจากข้อมูลจริง", "cta": "ดูรายละเอียด", "hashtags": []string{"#ลอง"}, "productionSpec": spec})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Fatalf("authorization header missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}, "usage": map[string]int{"total_tokens": 123}})
	}))
	defer server.Close()
	result, err := (OpenAICompatiblePlanner{Endpoint: server.URL + "/v1", Model: "qwen", APIKey: "private-test-key", Client: server.Client()}).Plan(context.Background(), PlanRequest{Product: product, Shots: guides})
	if err != nil || result.Provider != "local-qwen-role-planner" || result.Units != 123 || len(result.Roles) != 5 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
