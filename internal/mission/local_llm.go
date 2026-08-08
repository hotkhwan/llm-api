package mission

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// OpenAICompatibleCaptioner calls a local OpenAI-compatible endpoint. The
// caller should always wrap it in FallbackCaptioner so model downtime cannot
// block a beginner's first post.
type OpenAICompatibleCaptioner struct {
	Endpoint string
	Model    string
	APIKey   string
	Client   *http.Client
}

type OpenAICompatiblePlanner struct {
	Endpoint string
	Model    string
	APIKey   string
	Client   *http.Client
}

func (g OpenAICompatiblePlanner) Plan(ctx context.Context, request PlanRequest) (PlanResult, error) {
	endpoint, err := url.Parse(strings.TrimRight(g.Endpoint, "/") + "/chat/completions")
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return PlanResult{}, fmt.Errorf("invalid local LLM endpoint")
	}
	if strings.TrimSpace(g.Model) == "" {
		return PlanResult{}, fmt.Errorf("local LLM model is required")
	}
	facts, err := json.Marshal(request.Product)
	if err != nil {
		return PlanResult{}, err
	}
	skeleton := deterministicProductionSpec(request.Product, request.Shots)
	skeletonJSON, _ := json.Marshal(skeleton)
	prompt := fmt.Sprintf(`Act through these roles in one shared runtime: creativeDirector, storyDirector, brandGuard, productionPlanner, promptCompiler.
Return one JSON object with keys caption, cta, hashtags, productionSpec. productionSpec must preserve schemaVersion %q, exactly 3 shots, 9:16, 6-25 seconds and all continuity bible fields.
Verified product JSON: %s
Safe deterministic skeleton to improve without changing facts: %s
Thai output. Never invent price, promotion, product capability, sales result or income promise.`, ProductionSpecSchemaVersion, facts, skeletonJSON)
	payload := map[string]any{"model": g.Model, "temperature": 0.6, "top_p": 0.95, "response_format": map[string]string{"type": "json_object"}, "messages": []map[string]string{{"role": "system", "content": "You are the KWANNI production brain. Produce strictly typed, truthful affiliate production plans."}, {"role": "user", "content": prompt}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return PlanResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return PlanResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if g.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+g.APIKey)
	}
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return PlanResult{}, fmt.Errorf("local LLM planning request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PlanResult{}, fmt.Errorf("local LLM planning status %d", response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&completion); err != nil {
		return PlanResult{}, fmt.Errorf("decode local LLM planning response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return PlanResult{}, fmt.Errorf("local LLM returned no planning choice")
	}
	var output struct {
		Caption        string         `json:"caption"`
		CTA            string         `json:"cta"`
		Hashtags       []string       `json:"hashtags"`
		ProductionSpec ProductionSpec `json:"productionSpec"`
	}
	if err := json.Unmarshal([]byte(completion.Choices[0].Message.Content), &output); err != nil {
		return PlanResult{}, fmt.Errorf("decode local LLM production JSON: %w", err)
	}
	return PlanResult{Caption: output.Caption, CTA: output.CTA, Hashtags: output.Hashtags, ProductionSpec: output.ProductionSpec, Roles: append([]RoleExecution(nil), defaultRoleExecutions...), Provider: "local-qwen-role-planner", Units: completion.Usage.TotalTokens}, nil
}

func (g OpenAICompatibleCaptioner) Generate(ctx context.Context, request CaptionRequest) (CaptionResult, error) {
	endpoint, err := url.Parse(strings.TrimRight(g.Endpoint, "/") + "/chat/completions")
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return CaptionResult{}, fmt.Errorf("invalid local LLM endpoint")
	}
	model := strings.TrimSpace(g.Model)
	if model == "" {
		return CaptionResult{}, fmt.Errorf("local LLM model is required")
	}
	productFacts, err := json.Marshal(request.Product)
	if err != nil {
		return CaptionResult{}, err
	}
	prompt := fmt.Sprintf("ข้อมูลสินค้าจริง JSON: %s\nเขียน JSON ภาษาไทยเท่านั้น รูปแบบ caption, cta, hashtags ใช้เฉพาะข้อมูลใน JSON ห้ามสร้างราคา โปรโมชั่น คุณสมบัติ ผลลัพธ์ หรือคำสัญญารายได้", productFacts)
	payload := map[string]any{
		"model":           model,
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
		"messages":        []map[string]string{{"role": "system", "content": "คุณคือผู้ช่วย KWANNI ที่เขียนข้อความตรงไปตรงมาและไม่รับประกันรายได้"}, {"role": "user", "content": prompt}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CaptionResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return CaptionResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if g.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+g.APIKey)
	}
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return CaptionResult{}, fmt.Errorf("local LLM request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return CaptionResult{}, fmt.Errorf("local LLM status %d", response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&completion); err != nil {
		return CaptionResult{}, fmt.Errorf("decode local LLM response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return CaptionResult{}, fmt.Errorf("local LLM returned no choice")
	}
	var output struct {
		Caption  string   `json:"caption"`
		CTA      string   `json:"cta"`
		Hashtags []string `json:"hashtags"`
	}
	if err := json.Unmarshal([]byte(completion.Choices[0].Message.Content), &output); err != nil {
		return CaptionResult{}, fmt.Errorf("decode local LLM JSON: %w", err)
	}
	if strings.TrimSpace(output.Caption) == "" {
		return CaptionResult{}, fmt.Errorf("local LLM returned empty caption")
	}
	return CaptionResult{Caption: output.Caption, CTA: output.CTA, Hashtags: output.Hashtags, Provider: "local-openai-compatible", Units: completion.Usage.TotalTokens}, nil
}
