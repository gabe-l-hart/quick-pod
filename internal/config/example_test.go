package config

import "testing"

func TestExampleConfigParses(t *testing.T) {
	path := "../../examples/podcasts.yaml"
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("example config should parse: %v", err)
	}
	if cfg.LLM.Model == "" {
		t.Fatal("llm.model missing")
	}
	if cfg.Critic.LLM.Model == "" {
		t.Fatal("critic model missing")
	}
	if cfg.Critic.Decision != "llm" {
		t.Fatalf("decision = %q", cfg.Critic.Decision)
	}
}
