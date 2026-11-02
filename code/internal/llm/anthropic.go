package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/anthropics/anthropic-sdk-go/v2/shared/constant"

	"revisiontool/internal/config"
)

type anthropicClient struct {
	client anthropic.Client
	model  string
	effort string
}

func newAnthropic(cfg config.Config) *anthropicClient {
	return &anthropicClient{
		client: anthropic.NewClient(option.WithAPIKey(cfg.LLMAPIKey)),
		model:  cfg.LLMModel,
		effort: cfg.LLMEffort,
	}
}

func (c *anthropicClient) Model() string { return "anthropic/" + c.model }

// supportsDefaultFallback lists models that accept server-side refusal
// fallbacks in the "default" form.
func supportsDefaultFallback(model string) bool {
	switch model {
	case "claude-opus-5-5", "claude-opus-5", "claude-fable-5-1", "claude-sonnet-5-5":
		return true
	}
	return false
}

func (c *anthropicClient) Complete(ctx context.Context, req Request) (string, error) {
	var content []anthropic.BetaContentBlockParamUnion
	if req.Reference != "" {
		// The reference material is identical for every question in a
		// session, so it is cached: system + reference form the prefix.
		content = append(content, anthropic.BetaContentBlockParamUnion{OfText: &anthropic.BetaTextBlockParam{
			Text:         req.Reference,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}})
	}
	content = append(content, anthropic.NewBetaTextBlock(req.Prompt))

	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(req.MaxTokens),
		System:    []anthropic.BetaTextBlockParam{{Text: req.System}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(content...)},
	}
	if req.Schema != nil {
		params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: req.Schema}
	}
	if c.effort != "" {
		params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(c.effort)
	}
	if supportsDefaultFallback(c.model) {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}

	resp, err := c.client.Beta.Messages.New(ctx, params)
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case 401, 403:
				return "", fmt.Errorf("the Anthropic API rejected the API key (HTTP %d)", apiErr.StatusCode)
			case 429:
				return "", fmt.Errorf("the Anthropic API rate limit was reached; wait a moment and try again")
			case 400:
				return "", fmt.Errorf("the Anthropic API rejected the request: %s", apiErr.Error())
			}
		}
		return "", fmt.Errorf("Anthropic API call failed: %w", err)
	}

	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return "", fmt.Errorf("the model declined to answer this request")
	case anthropic.BetaStopReasonMaxTokens:
		return "", fmt.Errorf("the model's reply was cut off (raise LLM_MAX_OUTPUT_TOKENS)")
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String(), nil
}
