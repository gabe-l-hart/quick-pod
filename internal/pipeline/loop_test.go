package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/gabe-l-hart/podcasts/internal/config"
	"github.com/gabe-l-hart/podcasts/internal/mock"
	"github.com/gabe-l-hart/podcasts/internal/prompts"
	"github.com/gabe-l-hart/podcasts/internal/research"
)

func setupRunner(t *testing.T, mockCriticFailures int, maxIterations int) (*Runner, *mock.Client) {
	t.Helper()
	promptReg, err := prompts.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := mock.New(mockCriticFailures)
	cfg := config.Default()
	cfg.Pipeline.MaxIterations = maxIterations
	cfg.Pipeline.MaxResearchSteps = 4
	agent := research.NewAgent("test topic", nil, []string{"https://example.org"}, nil, nil, promptReg, 4, nil)
	return &Runner{
		Cfg:        cfg,
		Prompts:    promptReg,
		Docs:       nil,
		Primary:    m,
		Critic:     m,
		Decision:   nil, // llm decision path
		Research:   agent,
		TimeBudget: 5 * time.Second,
	}, m
}

func TestLoopPassesOnSecondIteration(t *testing.T) {
	r, m := setupRunner(t, 1, 4)
	_ = m
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Iterations) != 2 {
		t.Fatalf("iterations = %d", len(res.Iterations))
	}
	if !res.Decision.Pass {
		t.Fatalf("expected pass, got %+v", res.Decision)
	}
	if res.Script == nil || res.Script.Title == "" {
		t.Fatalf("script = %+v", res.Script)
	}
	if res.Critique == nil {
		t.Fatalf("critique = %+v", res.Critique)
	}
	if res.Summary == "" {
		t.Fatal("summary empty")
	}
}

func TestLoopHitsCap(t *testing.T) {
	r, _ := setupRunner(t, 10, 2) // never passes
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Iterations) != 2 {
		t.Fatalf("iterations = %d", len(res.Iterations))
	}
	if res.Decision.Pass {
		t.Fatalf("expected no pass, got %+v", res.Decision)
	}
}

func TestWriterAndParse(t *testing.T) {
	promptReg, err := prompts.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := mock.New(0)
	cfg := pipelineConfig{TargetMinutes: 17, WordsPerMinute: 150, WordBudget: 2550}
	script, err := WriteScript(context.Background(), m, promptReg, "test topic", "brief", nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if script.Title != "Mock Episode" {
		t.Fatalf("title = %q", script.Title)
	}
	if len(script.Segments) == 0 {
		t.Fatal("no segments")
	}
	if script.TotalWords() < 20 {
		t.Fatalf("words = %d", script.TotalWords())
	}
}

func TestCriticAndParse(t *testing.T) {
	promptReg, err := prompts.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := mock.New(0)
	script := &Script{Title: "t", Segments: []Segment{{Kind: "body", Text: "hello world", Title: "b"}}}
	cfg := pipelineConfig{TargetMinutes: 17, WordsPerMinute: 150, WordBudget: 2550}
	c, err := CritiqueScript(context.Background(), m, promptReg, "topic", "brief", script, cfg, 0.8)
	if err != nil {
		t.Fatal(err)
	}
	if c.Pass == false {
		t.Fatalf("expected pass (mock fail=0), got %+v", c)
	}
	if c.Score < 0.8 {
		t.Fatalf("score = %v", c.Score)
	}
}
