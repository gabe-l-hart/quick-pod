package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "podcasts.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsValidate(t *testing.T) {
	cfg := Default()
	cfg.LLM.BaseURL = "http://127.0.0.1:8080/v1"
	cfg.LLM.Model = "m"
	cfg.Critic.LLM.BaseURL = "http://127.0.0.1:8080/v1"
	cfg.Critic.LLM.Model = "mc"
	cfg.TTS.BaseURL = "http://127.0.0.1:8083/v1"
	cfg.TTS.Model = "tts"
	cfg.Sources.Arxiv = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid default: %v", err)
	}
}

func TestMissingLLMRequired(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing llm.base_url")
	}
}

func TestCriticSystemoneRequiresPath(t *testing.T) {
	cfg := Default()
	cfg.LLM.BaseURL = "http://x/v1"
	cfg.LLM.Model = "m"
	cfg.Critic.LLM.BaseURL = "http://x/v1"
	cfg.Critic.LLM.Model = "mc"
	cfg.Critic.Decision = "systemone"
	cfg.TTS.BaseURL = "http://x/v1"
	cfg.TTS.Model = "t"
	cfg.Sources.Arxiv = true
	// systemone base_url + path empty → error
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing systemone path")
	}
	cfg.Critic.Systemone.BaseURL = "http://x/v1"
	cfg.Critic.Systemone.Path = "/v1/systemone"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvExpansion(t *testing.T) {
	t.Setenv("PODCAST_TEST_KEY", "expanded-key")
	path := writeTemp(t, `
llm:
  base_url: "http://127.0.0.1:8080/v1"
  api_key: "${PODCAST_TEST_KEY}"
  model: "m"
critic:
  llm:
    base_url: "http://127.0.0.1:8080/v1"
    model: "mc"
  decision: "llm"
tts:
  base_url: "http://127.0.0.1:8083/v1"
  model: "t"
sources:
  arxiv: true
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKey != "expanded-key" {
		t.Fatalf("api_key = %q", cfg.LLM.APIKey)
	}
}

func TestValidationRejectsBadTarget(t *testing.T) {
	cfg := Default()
	cfg.LLM.BaseURL = "http://x/v1"
	cfg.LLM.Model = "m"
	cfg.Critic.LLM.BaseURL = "http://x/v1"
	cfg.Critic.LLM.Model = "mc"
	cfg.TTS.BaseURL = "http://x/v1"
	cfg.TTS.Model = "t"
	cfg.Sources.Arxiv = true
	cfg.Pipeline.TargetMinutes = 30
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for target_minutes out of range")
	}
}

func TestSlug(t *testing.T) {
	slug := Slug("Retrieval-augmented generation: how it changed the field!")
	if slug == "" || strings.Contains(slug, " ") {
		t.Fatalf("slug = %q", slug)
	}
	if slug != "retrieval-augmented-generation-how-it-changed-the-field" {
		t.Fatalf("slug = %q", slug)
	}
}
