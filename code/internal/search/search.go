// Package search performs external web searches. It is only used as a
// fallback when the uploaded documents do not contain enough information, and
// the provider can be replaced without touching the answering workflow.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"revisiontool/internal/config"
)

// Result is one real search hit. Only URLs returned by the provider are ever
// shown to the student as external sources.
type Result struct {
	Title   string
	URL     string
	Content string
}

// Provider is implemented by every search backend.
type Provider interface {
	Search(ctx context.Context, query string, maxResults int) ([]Result, error)
	// Available reports whether searching is configured at all.
	Available() bool
	Name() string
}

// ErrUnavailable means no search provider is configured.
var ErrUnavailable = errors.New("external search is not configured")

// New builds the provider selected by configuration.
func New(cfg config.Config) Provider {
	if cfg.SearchProvider == "tavily" && cfg.SearchAPIKey != "" {
		return &Tavily{APIKey: cfg.SearchAPIKey, HTTP: &http.Client{Timeout: 30 * time.Second}}
	}
	return None{}
}

// None is used when no search provider is configured. It never pretends a
// search happened.
type None struct{}

func (None) Search(context.Context, string, int) ([]Result, error) { return nil, ErrUnavailable }
func (None) Available() bool                                        { return false }
func (None) Name() string                                           { return "none" }

// Tavily uses the Tavily search API (https://docs.tavily.com).
type Tavily struct {
	APIKey   string
	HTTP     *http.Client
	Endpoint string // overridable for tests; defaults to the public API
}

func (t *Tavily) Available() bool { return true }
func (t *Tavily) Name() string    { return "tavily" }

func (t *Tavily) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	endpoint := t.Endpoint
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}
	payload, _ := json.Marshal(map[string]any{
		"query":        query,
		"max_results":  maxResults,
		"search_depth": "basic",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search provider returned HTTP %d", resp.StatusCode)
	}
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unexpected search response: %w", err)
	}
	var results []Result
	for _, r := range out.Results {
		if !strings.HasPrefix(r.URL, "http://") && !strings.HasPrefix(r.URL, "https://") {
			continue
		}
		results = append(results, Result{Title: strings.TrimSpace(r.Title), URL: r.URL, Content: strings.TrimSpace(r.Content)})
	}
	return results, nil
}
