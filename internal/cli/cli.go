// Package cli wires the pipeline together: config → documents → research →
// write → critique → decision → render → artifacts.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gabe-l-hart/podcasts/internal/artifacts"
	"github.com/gabe-l-hart/podcasts/internal/audio"
	"github.com/gabe-l-hart/podcasts/internal/config"
	"github.com/gabe-l-hart/podcasts/internal/decision"
	"github.com/gabe-l-hart/podcasts/internal/documents"
	"github.com/gabe-l-hart/podcasts/internal/llm"
	"github.com/gabe-l-hart/podcasts/internal/mock"
	"github.com/gabe-l-hart/podcasts/internal/pipeline"
	"github.com/gabe-l-hart/podcasts/internal/prompts"
	"github.com/gabe-l-hart/podcasts/internal/research"
	"github.com/gabe-l-hart/podcasts/internal/search"
	"github.com/gabe-l-hart/podcasts/internal/tts"
)

// Options carries the CLI's flags.
type Options struct {
	ConfigPath  string
	Topic       string
	Docs        []string
	OutDir      string
	DryRun      bool
	Mock        bool
	ForceRender bool
	Verbose     bool
	Version     bool
	Stdout      io.Writer
}

// Run executes the full flow.
func Run(ctx context.Context, opts Options, stdout io.Writer) error {
	logf := func(format string, args ...any) { fmt.Fprintf(stdout, format+"\n", args...) }

	if opts.Version {
		fmt.Fprintln(stdout, "podcast dev (no version baked in)")
		return nil
	}
	if opts.Topic == "" {
		return fmt.Errorf("missing --topic (required)")
	}

	// ---- config ----
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}

	// ---- prompts ----
	promptsReg, err := prompts.New(cfg.Prompts.Overrides, cfg.Prompts.Files)
	if err != nil {
		return fmt.Errorf("prompts: %w", err)
	}

	// ---- clients ----
	var primary pipeline.LLM
	var critic pipeline.LLM
	var decisionModel decision.Model
	if opts.Mock {
		mockPrimary := mock.New(1)
		mockCritic := mock.New(1)
		primary, critic = mockPrimary, mockCritic
	} else {
		p, c, dm, err := clients(cfg, opts)
		if err != nil {
			return err
		}
		primary, critic, decisionModel = p, c, dm
	}

	// ---- documents ----
	var docs []documents.Document
	var docFailures []string
	for _, d := range opts.Docs {
		doc, err := documents.Convert(d, &http.Client{Timeout: 60 * time.Second})
		if err != nil {
			docFailures = append(docFailures, fmt.Sprintf("%s: %v", d, err))
			logf("docs: failed to convert %s: %v", d, err)
			continue
		}
		logf("docs: converted %s (%d chars, truncated=%v)", doc.Source, len(doc.Text), doc.Truncated)
		docs = append(docs, *doc)
	}
	if len(docFailures) > 0 {
		logf("docs: %d of %d inputs failed (continuing)", len(docFailures), len(opts.Docs))
	}

	logf("primary: %s (model %s)", shortURL(cfg.LLM.BaseURL), cfg.LLM.Model)
	logf("critic:  %s (model %s)", shortURL(cfg.Critic.LLM.BaseURL), cfg.Critic.LLM.Model)
	logf("decision: %s", decisionDesc(cfg, opts))

	// ---- research ----
	arxiv := search.NewArxiv(&http.Client{Timeout: 60 * time.Second})
	if !cfg.Sources.Arxiv || opts.Mock {
		arxiv = nil // mock / no arxiv: the agent registers no arxiv tool
	}
	generalProvider, err := search.NewProvider(cfg.Search.Provider, cfg.Search.APIKey, &http.Client{Timeout: 60 * time.Second})
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if opts.Mock {
		generalProvider = nil
	}
	agent := research.NewAgent(opts.Topic, docs, cfg.Sources.TrustedURLs, arxiv, generalProvider, promptsReg, cfg.Pipeline.MaxResearchSteps, &http.Client{Timeout: 60 * time.Second})

	// ---- runner ----
	timeBudget, err := cfg.TimeBudgetDur()
	if err != nil {
		return fmt.Errorf("pipeline.time_budget: %w", err)
	}
	runner := &pipeline.Runner{
		Cfg:        cfg,
		Prompts:    promptsReg,
		Docs:       docs,
		Primary:    primary,
		Critic:     critic,
		Decision:   decisionModel,
		Research:   agent,
		TimeBudget: timeBudget,
		Logf:       func(format string, args ...any) { logf(format, args...) },
	}

	result, err := runner.Run(ctx)
	if err != nil {
		return err
	}

	// ---- render audio ----
	outDir := opts.OutDir
	if outDir == "" {
		outDir = cfg.Paths.OutputDir
	}
	slug := config.Slug(opts.Topic)
	episodeDir := filepath.Join(outDir, slug)
	verdict := result.Decision != nil && result.Decision.Pass

	rendered := false
	var audioPath, audioFormat string
	var audioChunks int
	var audioSeconds float64
	var audioErr error

	if !opts.DryRun && !opts.Mock && (verdict || opts.ForceRender) {
		logf("rendering audio...")
		audioPath, audioFormat, audioChunks, audioSeconds, audioErr = renderAudio(ctx, cfg, result.Script, episodeDir, logf)
		if audioErr != nil {
			logf("audio: FAILED: %v", audioErr)
		} else {
			rendered = true
			logf("audio: wrote %s (%s)", audioPath, audioFormat)
		}
	} else {
		switch {
		case opts.DryRun:
			logf("dry-run: skipping audio render")
		case opts.Mock:
			logf("mock mode: skipping audio render")
		case !verdict:
			logf("verdict is FAIL after %d iterations — not rendering audio (use --force-render to override)", len(result.Iterations))
		}
	}

	// ---- artifacts ----
	written, err := artifacts.Write(episodeDir, artifacts.Options{
		Topic:         opts.Topic,
		Slug:          slug,
		Result:        result,
		Config:        cfg,
		DocsCount:     len(docs),
		AudioPath:     audioPath,
		AudioFormat:   audioFormat,
		AudioChunks:   audioChunks,
		AudioSeconds:  audioSeconds,
		RenderedAudio: rendered,
	})
	if err != nil {
		return fmt.Errorf("artifacts: %w", err)
	}
	logf("artifacts: wrote %d files to %s", len(written), episodeDir)

	// ---- summary ----
	logf("")
	logf("Episode: %s", slug)
	logf("  verdict:   %s", verdictString(result))
	logf("  iterations: %d", len(result.Iterations))
	logf("  duration:   ~%.1f min (%d words)", result.Script.DurationSec(cfg.Pipeline.WordsPerMinute)/60, result.Script.TotalWords())
	logf("  output:     %s", episodeDir)
	if rendered {
		logf("  audio:      %s", audioPath)
	}
	if opts.Verbose && len(result.Iterations) > 0 {
		for _, it := range result.Iterations {
			if it.Critique != nil {
				logf("  iter %d: score=%.2f pass=%v", it.N, it.Critique.Score, it.Critique.Pass)
			}
		}
	}

	if audioErr != nil {
		return fmt.Errorf("audio render failed: %w", audioErr)
	}
	if !verdict && !opts.ForceRender {
		return fmt.Errorf("episode did not reach PASS after %d iterations (see %s)", len(result.Iterations), episodeDir)
	}
	return nil
}

func loadConfig(opts Options) (*config.Config, error) {
	if opts.Mock {
		cfg := config.Default()
		cfg.LLM.BaseURL = "http://mock.local/v1"
		cfg.LLM.Model = "mock-model"
		cfg.LLM.APIKey = ""
		cfg.Critic.LLM.BaseURL = "http://mock.local/v1"
		cfg.Critic.LLM.Model = "mock-model"
		cfg.Critic.LLM.APIKey = ""
		cfg.TTS.BaseURL = "http://mock.local/v1"
		cfg.TTS.Model = "mock-tts"
		cfg.TTS.APIKey = ""
		cfg.Sources.Arxiv = false
		cfg.Search.Provider = "none"
		cfg.Pipeline.MaxIterations = 3
		return cfg, nil
	}
	path := opts.ConfigPath
	if path == "" {
		path = defaultConfigPath()
	}
	if !fileExists(path) {
		return nil, fmt.Errorf("config file not found: %s (pass --config, or create one — see examples/podcasts.yaml)", path)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func defaultConfigPath() string {
	for _, p := range []string{"podcasts.yaml", filepath.Join(os.Getenv("HOME"), ".config", "podcasts", "podcasts.yaml")} {
		if p != "" && fileExists(p) {
			return p
		}
	}
	return "podcasts.yaml"
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// clients builds the primary/critic LLM clients and the decision model.
func clients(cfg *config.Config, opts Options) (*llm.Client, *llm.Client, decision.Model, error) {
	primary := llm.New(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model, cfg.LLM.Temperature, cfg.LLM.MaxTokens, cfg.Critic.SchemaStyle)
	critic := llm.New(cfg.Critic.LLM.BaseURL, cfg.Critic.LLM.APIKey, cfg.Critic.LLM.Model, cfg.Critic.LLM.Temperature, cfg.Critic.LLM.MaxTokens, cfg.Critic.SchemaStyle)

	// optional strict GBNF for the critic (llama.cpp only)
	if cfg.Critic.Grammar != "" {
		grammarBytes, err := os.ReadFile(cfg.Critic.Grammar)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read critic grammar: %w", err)
		}
		critic.DefaultGrammar = string(grammarBytes)
	}

	var decisionModel decision.Model
	if cfg.Critic.Decision == "systemone" {
		decisionModel = decision.NewSystemone(cfg.Critic.Systemone.BaseURL, cfg.Critic.Systemone.Path, cfg.Critic.Systemone.APIKey, cfg.Critic.Systemone.Threshold, nil)
	}
	return primary, critic, decisionModel, nil
}

// ---- rendering ----

func renderAudio(ctx context.Context, cfg *config.Config, script *pipeline.Script, episodeDir string, logf func(string, ...any)) (string, string, int, float64, error) {
	if script == nil {
		return "", "", 0, 0, fmt.Errorf("no script")
	}
	if len(script.Segments) == 0 {
		return "", "", 0, 0, fmt.Errorf("script has no segments")
	}

	client := tts.New(cfg.TTS.BaseURL, cfg.TTS.APIKey, cfg.TTS.Model, cfg.TTS.Voice, cfg.TTS.Format, cfg.TTS.Speed)

	if err := os.MkdirAll(episodeDir, 0o755); err != nil {
		return "", "", 0, 0, err
	}

	format := cfg.TTS.Format
	if format == "" {
		format = "wav"
	}

	var queue []string
	for _, seg := range script.Segments {
		for _, chunk := range tts.Chunks(seg.Text, 4096) {
			queue = append(queue, chunk)
		}
	}

	parts := make([][]byte, 0, len(queue))
	totalChars := 0
	for _, chunk := range queue {
		if ctx.Err() != nil {
			return "", "", 0, 0, ctx.Err()
		}
		totalChars += len(chunk)
		logf("tts: chunk %d/%d (%d chars)", len(parts)+1, len(queue), len(chunk))
		audioBytes, err := synthWithRetry(ctx, client, chunk)
		if err != nil {
			return "", "", 0, 0, fmt.Errorf("chunk %d: %w", len(parts)+1, err)
		}
		parts = append(parts, audioBytes)
	}

	outPath := filepath.Join(episodeDir, "audio."+format)
	if format == "wav" {
		if err := audio.MergeToFile(outPath, parts); err != nil {
			return "", "", 0, 0, fmt.Errorf("merge wav: %w", err)
		}
	} else {
		return "", "", 0, 0, fmt.Errorf("format %q requires ffmpeg-based assembly (not yet implemented; use wav)", format)
	}

	seconds := float64(totalChars) / 62.5 // ~150 wpm ≈ 75 words/min... rough
	return outPath, format, len(parts), seconds, nil
}

func synthWithRetry(ctx context.Context, client *tts.Client, text string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		audioBytes, err := client.Synthesize(ctx, text)
		if err != nil {
			lastErr = err
			continue
		}
		return audioBytes, nil
	}
	return nil, lastErr
}

func shortURL(u string) string {
	if u == "" {
		return "(empty)"
	}
	if len(u) > 60 {
		return u[:60] + "…"
	}
	return u
}

func decisionDesc(cfg *config.Config, opts Options) string {
	if opts.Mock {
		return "mock (llm decision)"
	}
	if cfg.Critic.Decision == "systemone" {
		return "systemone (" + cfg.Critic.Systemone.Path + ")"
	}
	return "llm (constrained decoding)"
}

func verdictString(result *pipeline.Result) string {
	if result == nil || result.Decision == nil {
		return "UNKNOWN"
	}
	pass := "FAIL"
	if result.Decision.Pass {
		pass = "PASS"
	}
	return fmt.Sprintf("%s (%s, %s)", pass, result.Decision.Source, result.Decision.Reason)
}
