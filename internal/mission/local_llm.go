package mission

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	Client   *http.Client
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
	prompt := fmt.Sprintf("สินค้า: %s\nข้อมูลจริง: %s\nเขียน JSON ภาษาไทยเท่านั้น รูปแบบ caption, cta, hashtags ห้ามสร้างราคา โปรโมชั่น หรือคุณสมบัติที่ไม่มีในข้อมูล", request.Product.Name, request.Product.Description)
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
	if err := json.NewDecoder(response.Body).Decode(&completion); err != nil {
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
