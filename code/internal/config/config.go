// Package config loads application settings from environment variables
// (optionally seeded from a .env file). All limits and provider settings live
// here so they are defined once rather than scattered through the code.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds every tunable setting of the application.
type Config struct {
	Addr   string // HTTP listen address, e.g. ":8080"
	DBPath string // SQLite database file

	// Upload limits.
	MaxReferenceFileBytes int64
	MaxPaperFileBytes     int64
	MaxReferenceDocs      int

	// LLM settings.
	LLMProvider        string // "anthropic" or "openai"
	LLMModel           string
	LLMAPIKey          string
	LLMBaseURL         string // only used by the openai-compatible provider
	LLMEffort          string // anthropic only: low|medium|high|xhigh|max
	LLMContextWindow   int    // total tokens the model accepts (input + output)
	LLMMaxOutputTokens int    // tokens reserved for the model's answer

	// External search settings.
	SearchProvider   string // "tavily" or "none"
	SearchAPIKey     string
	SearchMaxResults int
}

// Load reads configuration from the environment. If a .env file exists in the
// working directory its values are applied first, without overriding
// variables that are already set in the real environment.
func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}

	c := Config{
		Addr:                  env("ADDR", "127.0.0.1:8080"),
		DBPath:                env("DB_PATH", "revision.db"),
		MaxReferenceFileBytes: int64(envInt("MAX_REFERENCE_FILE_MB", 10)) << 20,
		MaxPaperFileBytes:     int64(envInt("MAX_PAPER_FILE_MB", 10)) << 20,
		MaxReferenceDocs:      envInt("MAX_REFERENCE_DOCS", 5),
		LLMProvider:           strings.ToLower(env("LLM_PROVIDER", "anthropic")),
		LLMEffort:             env("LLM_EFFORT", "medium"),
		SearchProvider:        strings.ToLower(env("SEARCH_PROVIDER", "")),
		SearchMaxResults:      envInt("SEARCH_MAX_RESULTS", 5),
	}

	switch c.LLMProvider {
	case "anthropic":
		c.LLMAPIKey = os.Getenv("ANTHROPIC_API_KEY")
		c.LLMModel = env("LLM_MODEL", "claude-opus-5-5")
		c.LLMContextWindow = envInt("LLM_CONTEXT_WINDOW", 1_000_000)
		c.LLMMaxOutputTokens = envInt("LLM_MAX_OUTPUT_TOKENS", 16_000)
	case "openai":
		c.LLMAPIKey = os.Getenv("OPENAI_API_KEY")
		c.LLMBaseURL = strings.TrimRight(env("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/")
		c.LLMModel = os.Getenv("LLM_MODEL")
		if c.LLMModel == "" {
			return c, fmt.Errorf("LLM_MODEL must be set when LLM_PROVIDER=openai")
		}
		c.LLMContextWindow = envInt("LLM_CONTEXT_WINDOW", 128_000)
		c.LLMMaxOutputTokens = envInt("LLM_MAX_OUTPUT_TOKENS", 8_000)
	default:
		return c, fmt.Errorf("unknown LLM_PROVIDER %q (use \"anthropic\" or \"openai\")", c.LLMProvider)
	}

	if c.SearchProvider == "" {
		if os.Getenv("TAVILY_API_KEY") != "" {
			c.SearchProvider = "tavily"
		} else {
			c.SearchProvider = "none"
		}
	}
	switch c.SearchProvider {
	case "tavily":
		c.SearchAPIKey = os.Getenv("TAVILY_API_KEY")
	case "none":
	default:
		return c, fmt.Errorf("unknown SEARCH_PROVIDER %q (use \"tavily\" or \"none\")", c.SearchProvider)
	}

	if c.MaxReferenceDocs < 1 || c.MaxReferenceFileBytes <= 0 || c.MaxPaperFileBytes <= 0 {
		return c, fmt.Errorf("upload limits must be positive")
	}
	if c.LLMMaxOutputTokens >= c.LLMContextWindow {
		return c, fmt.Errorf("LLM_MAX_OUTPUT_TOKENS must be smaller than LLM_CONTEXT_WINDOW")
	}
	return c, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// loadDotEnv applies KEY=VALUE lines from path. Missing file is not an error.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, set := os.LookupEnv(key); !set {
			os.Setenv(key, val)
		}
	}
	return sc.Err()
}
