package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"revisiontool/internal/config"
)

var testSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"ok": map[string]any{"type": "boolean"}},
	"required":             []string{"ok"},
	"additionalProperties": false,
}

func TestAnthropicRequestShape(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"text","text":"{\"ok\":true}"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	c := &anthropicClient{
		client: anthropic.NewClient(option.WithAPIKey("k"), option.WithBaseURL(srv.URL)),
		model:  "claude-opus-5-5",
		effort: "medium",
	}
	out, err := c.Complete(context.Background(), Request{System: "sys", Reference: "REF", Prompt: "Q", Schema: testSchema, MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ OK bool }
	if err := DecodeJSON(out, &v); err != nil || !v.OK {
		t.Fatalf("reply %q: %v", out, err)
	}

	raw, _ := json.Marshal(body)
	s := string(raw)
	for _, want := range []string{`"cache_control":{"type":"ephemeral"}`, `"effort":"medium"`, `"json_schema"`, `"fallbacks":"default"`} {
		if !strings.Contains(s, want) {
			t.Errorf("request body missing %s: %s", want, s)
		}
	}
}

func TestOpenAICompatible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad", 400)
			return
		}
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"`+"```json\\n{\\\"ok\\\":true}\\n```"+`"}}]}`)
	}))
	defer srv.Close()
	c := newOpenAI(config.Config{LLMBaseURL: srv.URL + "/v1", LLMAPIKey: "k", LLMModel: "m"})
	out, err := c.Complete(context.Background(), Request{System: "s", Prompt: "p", Schema: testSchema, MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ OK bool }
	if err := DecodeJSON(out, &v); err != nil || !v.OK {
		t.Fatalf("reply %q: %v", out, err)
	}
}
