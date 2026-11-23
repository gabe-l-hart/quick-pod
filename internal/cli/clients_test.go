package cli

import (
	"os"
	"testing"

	"github.com/gabe-l-hart/podcasts/internal/config"
)

func TestClientsSystemoneMode(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.BaseURL = "http://x/v1"
	cfg.LLM.Model = "m"
	cfg.Critic.LLM.BaseURL = "http://y/v1"
	cfg.Critic.LLM.Model = "mc"
	cfg.Critic.Decision = "systemone"
	cfg.Critic.Systemone.BaseURL = "http://y/v1"
	cfg.Critic.Systemone.Path = "/v1/systemone"

	primary, critic, decisionModel, err := clients(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if primary == nil || critic == nil {
		t.Fatal("primary/critic clients required")
	}
	if decisionModel == nil {
		t.Fatal("systemone decision model required")
	}
}

func TestClientsLLMMode(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.BaseURL = "http://x/v1"
	cfg.LLM.Model = "m"
	cfg.Critic.LLM.BaseURL = "http://x/v1"
	cfg.Critic.LLM.Model = "mc"
	cfg.Critic.Decision = "llm"
	_, _, decisionModel, err := clients(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if decisionModel != nil {
		t.Fatal("llm mode should not build a decision model")
	}
}

func TestClientsGrammar(t *testing.T) {
	dir := t.TempDir()
	grammar := dir + "/grammar.gbnf"
	if err := os.WriteFile(grammar, []byte("root ::= \"x\""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.LLM.BaseURL = "http://x/v1"
	cfg.LLM.Model = "m"
	cfg.Critic.LLM.BaseURL = "http://x/v1"
	cfg.Critic.LLM.Model = "mc"
	cfg.Critic.Grammar = grammar
	_, critic, _, err := clients(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if critic.DefaultGrammar == "" {
		t.Fatal("expected DefaultGrammar to be set")
	}
}
