// Package llm is a thin, provider-neutral interface to a language model. The
// rest of the application only depends on Client; switching providers means
// changing configuration, not core logic.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"revisiontool/internal/config"
)

// Request is one model call. Reference holds the large, unchanging reference
// material; providers that support prompt caching cache it so repeated
// questions over the same documents are cheaper and faster.
type Request struct {
	System    string
	Reference string
	Prompt    string
	// Schema is the JSON schema the reply must follow. The reply is always a
	// single JSON object.
	Schema    map[string]any
	MaxTokens int
}

// Client is implemented by every LLM provider.
type Client interface {
	// Complete returns the model's JSON reply as raw text.
	Complete(ctx context.Context, req Request) (string, error)
	// Model returns a human-readable "provider/model" identifier.
	Model() string
}

// New builds the client selected by configuration.
func New(cfg config.Config) (Client, error) {
	if cfg.LLMAPIKey == "" {
		key := "ANTHROPIC_API_KEY"
		if cfg.LLMProvider == "openai" {
			key = "OPENAI_API_KEY"
		}
		return nil, fmt.Errorf("no LLM API key configured: set %s (see README)", key)
	}
	switch cfg.LLMProvider {
	case "anthropic":
		return newAnthropic(cfg), nil
	case "openai":
		return newOpenAI(cfg), nil
	}
	return nil, fmt.Errorf("unknown LLM provider %q", cfg.LLMProvider)
}

// DecodeJSON parses a model reply into v, tolerating code fences or text
// around the JSON object.
func DecodeJSON(reply string, v any) error {
	s := strings.TrimSpace(reply)
	if err := json.Unmarshal([]byte(s), v); err == nil {
		return nil
	}
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("model reply was not JSON")
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), v); err != nil {
		return fmt.Errorf("model reply was not valid JSON: %w", err)
	}
	return nil
}

// EstimateTokens is a deliberately conservative token estimate (about three
// characters per token; real English text averages closer to four). Over-
// estimating means we warn the user early instead of failing mid-request.
func EstimateTokens(s string) int {
	return (len(s) + 2) / 3
}
