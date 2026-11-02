package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"revisiontool/internal/config"
)

// openAIClient talks to any OpenAI-compatible chat-completions endpoint
// (OpenAI, Google Gemini's OpenAI-compatible endpoint, Groq, OpenRouter, a
// local Ollama server, ...) using only net/http.
type openAIClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func newOpenAI(cfg config.Config) *openAIClient {
	return &openAIClient{
		baseURL: cfg.LLMBaseURL,
		apiKey:  cfg.LLMAPIKey,
		model:   cfg.LLMModel,
		http:    &http.Client{Timeout: 10 * time.Minute},
	}
}

func (c *openAIClient) Model() string { return "openai-compatible/" + c.model }

func (c *openAIClient) Complete(ctx context.Context, req Request) (string, error) {
	user := req.Prompt
	if req.Reference != "" {
		user = req.Reference + "\n\n" + req.Prompt
	}
	system := req.System
	if req.Schema != nil {
		schema, _ := json.Marshal(req.Schema)
		system += "\n\nRespond with a single JSON object that conforms to this JSON schema and nothing else:\n" + string(schema)
	}
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens":      req.MaxTokens,
		"response_format": map[string]string{"type": "json_object"},
	}
	payload, _ := json.Marshal(body)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(hreq)
	if err != nil {
		return "", fmt.Errorf("LLM API call failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return "", fmt.Errorf("the LLM API rejected the API key (HTTP %d)", resp.StatusCode)
	case resp.StatusCode == 429:
		return "", fmt.Errorf("the LLM API rate limit was reached; wait a moment and try again")
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("LLM API returned HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("unexpected LLM API response")
	}
	ch := out.Choices[0]
	if ch.Message.Refusal != "" {
		return "", fmt.Errorf("the model declined to answer this request")
	}
	if ch.FinishReason == "length" {
		return "", fmt.Errorf("the model's reply was cut off (raise LLM_MAX_OUTPUT_TOKENS)")
	}
	return ch.Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
