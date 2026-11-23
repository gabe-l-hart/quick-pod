package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gabe-l-hart/podcasts/internal/config"
	"github.com/gabe-l-hart/podcasts/internal/decision"
	"github.com/gabe-l-hart/podcasts/internal/documents"
	"github.com/gabe-l-hart/podcasts/internal/llm"
	"github.com/gabe-l-hart/podcasts/internal/prompts"
	"github.com/gabe-l-hart/podcasts/internal/research"
)

// Result is the outcome of one pipeline run.
type Result struct {
	Script     *Script
	Critique   *Critique
	Decision   *decision.Result
	Brief      *research.Brief
	Summary    string
	Iterations []Iteration
}

// Iteration is one research+write+critique round.
type Iteration struct {
	N        int
	Script   *Script
	Critique *Critique
	Decision *decision.Result
}

// Runner drives the loop.
type Runner struct {
	Cfg       *config.Config
	Prompts   *prompts.Registry
	Docs      []documents.Document
	Primary   LLM            // research/summarize/write (primary LLM)
	Critic    LLM            // independent critic LLM
	Decision  decision.Model // nil for the llm decision path
	Research  *research.Agent
	TimeBudget time.Duration
	// Logf receives progress messages (may be nil).
	Logf func(format string, args ...any)
}

func (r *Runner) log(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

// Run executes research → write → critique → decision until the verdict is a pass
// or the iteration cap is reached.
func (r *Runner) Run(ctx context.Context) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.After(r.TimeBudget)
	cfg := r.Cfg

	// 1. Research (once)
	r.log("research: running agent (max %d steps)", cfg.Pipeline.MaxResearchSteps)
	select {
	case <-deadline:
		return nil, fmt.Errorf("time budget exhausted before research finished")
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	brief, err := r.Research.Run(ctx, r.Primary)
	if err != nil {
		return nil, fmt.Errorf("research: %w", err)
	}
	r.log("research: done (%d sources, %d failures)", len(brief.Sources), len(brief.Failures))

	// 2. Summarize the brief for the writer
	summary, err := r.Summarize(ctx, brief.Text)
	if err != nil {
		return nil, fmt.Errorf("summarize: %w", err)
	}

	// 3. Loop
	pc := pipelineConfig{
		TargetMinutes: cfg.Pipeline.TargetMinutes,
		WordsPerMinute: cfg.Pipeline.WordsPerMinute,
		WordBudget:    cfg.WordBudget(),
	}
	var issues []string
	var iterations []Iteration
	var lastScript *Script
	var lastCritique *Critique
	var lastDecision *decision.Result

	for iter := 1; iter <= cfg.Pipeline.MaxIterations; iter++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(r.TimeBudget):
			return nil, fmt.Errorf("time budget exhausted at iteration %d", iter)
		default:
		}

		r.log("iteration %d/%d: writing script", iter, cfg.Pipeline.MaxIterations)
		script, err := WriteScript(ctx, r.Primary, r.Prompts, r.Research.Topic, summary, r.Docs, issues, pc)
		if err != nil {
			return nil, fmt.Errorf("iteration %d: %w", iter, err)
		}
		r.log("iteration %d: script ready (%d words)", iter, script.TotalWords())

		r.log("iteration %d: critiquing", iter)
		critique, err := CritiqueScript(ctx, r.Critic, r.Prompts, r.Research.Topic, summary, script, pc, cfg.Pipeline.PassThreshold)
		if err != nil {
			return nil, fmt.Errorf("iteration %d: %w", iter, err)
		}
		r.log("iteration %d: critique done (score=%.2f pass=%v)", iter, critique.Score, critique.Pass)

		decision, err := r.decide(ctx, script, critique)
		if err != nil {
			return nil, fmt.Errorf("iteration %d: %w", iter, err)
		}

		it := Iteration{N: iter, Script: script, Critique: critique, Decision: decision}
		iterations = append(iterations, it)
		lastScript, lastCritique, lastDecision = script, critique, decision

		if decision.Pass {
			r.log("iteration %d: verdict = PASS", iter)
			return &Result{
				Script:     lastScript,
				Critique:   lastCritique,
				Decision:   lastDecision,
				Brief:      brief,
				Summary:    summary,
				Iterations: iterations,
			}, nil
		}
		r.log("iteration %d: verdict = FAIL (threshold=%.2f) — issues: %v", iter, cfg.Pipeline.PassThreshold, critique.Issues)
		issues = critique.Issues
	}

	r.log("no pass after %d iterations (cap reached)", cfg.Pipeline.MaxIterations)
	return &Result{
		Script:     lastScript,
		Critique:   lastCritique,
		Decision:   lastDecision,
		Brief:      brief,
		Summary:    summary,
		Iterations: iterations,
	}, nil
}

// Summarize condenses the research brief for the writer.
func (r *Runner) Summarize(ctx context.Context, brief string) (string, error) {
	system, err := r.Prompts.Render(prompts.SummarizeSystem, nil)
	if err != nil {
		return "", err
	}
	user, err := r.Prompts.Render(prompts.SummarizeUser, map[string]any{"Brief": brief})
	if err != nil {
		return "", err
	}
	resp, err := r.Primary.Chat(ctx, []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, llm.ChatOptions{})
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	return resp.Content, nil
}

// decide takes the verdict from the configured path.
func (r *Runner) decide(ctx context.Context, script *Script, critique *Critique) (*decision.Result, error) {
	if r.Decision == nil || r.Cfg.Critic.Decision == "llm" {
		return &decision.Result{
			Pass:   critique.Pass,
			Source: "llm",
			Score:  critique.Score,
			Reason: "score >= threshold and model verdict",
		}, nil
	}
	// systemone path
	in := decision.Input{
		Topic:          r.Research.Topic,
		ScriptSummary:  scriptSummary(script),
		CritiqueSummary: critiqueSummary(critique),
	}
	res, err := r.Decision.Decide(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("systemone decision: %w", err)
	}
	return res, nil
}

// scriptSummary renders a compact script summary for the decision model.
func scriptSummary(script *Script) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", script.Title)
	fmt.Fprintf(&b, "Summary: %s\n", script.Summary)
	fmt.Fprintf(&b, "Target: ~%d words\n\n", script.TotalWords())
	for _, seg := range script.Segments {
		t := strings.TrimSpace(seg.Text)
		if len(t) > 1500 {
			t = t[:1500] + "…"
		}
		fmt.Fprintf(&b, "[%s] %s\n%s\n\n", seg.Kind, seg.Title, t)
	}
	return b.String()
}

// critiqueSummary renders the critique as plain text.
func critiqueSummary(critique *Critique) string {
	var b strings.Builder
	fmt.Fprintf(&b, "score=%.2f pass=%v\n", critique.Score, critique.Pass)
	fmt.Fprintf(&b, "topic_fit=%.2f accuracy=%.2f coverage=%.2f length=%.2f engagement=%.2f\n",
		critique.TopicFit, critique.Accuracy, critique.Coverage, critique.Length, critique.Engagement)
	if len(critique.Issues) > 0 {
		fmt.Fprintf(&b, "issues: %s\n", strings.Join(critique.Issues, "; "))
	}
	return b.String()
}
