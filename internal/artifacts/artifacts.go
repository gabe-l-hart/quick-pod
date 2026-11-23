// Package artifacts writes the episode's files to the output directory.
package artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gabe-l-hart/podcasts/internal/config"
	"github.com/gabe-l-hart/podcasts/internal/pipeline"
	"github.com/gabe-l-hart/podcasts/internal/research"
)

// Options carries everything Write needs.
type Options struct {
	Topic         string
	Slug          string
	Result        *pipeline.Result
	Config        *config.Config
	DocsCount     int
	AudioPath     string
	AudioFormat   string
	AudioChunks   int
	AudioSeconds  float64
	RenderedAudio bool
	GeneratedAt   time.Time
}

// Write writes the episode artifacts into dir and returns the paths written.
func Write(dir string, opts Options) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string

	// script.md
	scriptPath := filepath.Join(dir, "script.md")
	if err := os.WriteFile(scriptPath, []byte(opts.Result.Script.RenderMarkdown()), 0o644); err != nil {
		return nil, err
	}
	written = append(written, scriptPath)

	// research.md
	if opts.Result.Brief != nil {
		path := filepath.Join(dir, "research.md")
		var b strings.Builder
		fmt.Fprintf(&b, "# Research brief: %s\n\n", opts.Topic)
		b.WriteString(opts.Result.Brief.Text)
		if len(opts.Result.Brief.Sources) > 0 {
			b.WriteString("\n\n---\n\n## Sources\n")
			for _, s := range opts.Result.Brief.Sources {
				fmt.Fprintf(&b, "- [%d] %s — %s\n", s.Index, s.Title, s.URL)
			}
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}

	// critique.json
	if opts.Result.Critique != nil {
		path := filepath.Join(dir, "critique.json")
		if err := writeJSON(path, opts.Result.Critique); err != nil {
			return nil, err
		}
		written = append(written, path)
	}

	// iterations
	if len(opts.Result.Iterations) > 0 {
		iterDir := filepath.Join(dir, "iterations")
		if err := os.MkdirAll(iterDir, 0o755); err != nil {
			return nil, err
		}
		for _, it := range opts.Result.Iterations {
			path := filepath.Join(iterDir, fmt.Sprintf("iteration-%d.md", it.N))
			var b strings.Builder
			fmt.Fprintf(&b, "# Iteration %d\n\n## Script\n\n%s\n\n## Critique\n\n", it.N, it.Script.RenderMarkdown())
			if it.Critique != nil {
				fmt.Fprintf(&b, "- score: %.2f (pass=%v)\n", it.Critique.Score, it.Critique.Pass)
				if len(it.Critique.Issues) > 0 {
					fmt.Fprintf(&b, "- issues: %s\n", strings.Join(it.Critique.Issues, "; "))
				}
			}
			if it.Decision != nil {
				fmt.Fprintf(&b, "- decision: %s pass=%v (%s)\n", it.Decision.Source, it.Decision.Pass, it.Decision.Reason)
			}
			if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
				return nil, err
			}
			written = append(written, path)
			jsonPath := filepath.Join(iterDir, fmt.Sprintf("iteration-%d.json", it.N))
			if err := writeJSON(jsonPath, map[string]any{
				"iteration": it.N,
				"script":    it.Script,
				"critique":  it.Critique,
				"decision":  it.Decision,
			}); err != nil {
				return nil, err
			}
			written = append(written, jsonPath)
		}
	}

	// meta.json
	snap, err := opts.Config.Snapshot()
	if err != nil {
		return nil, err
	}
	var sources []research.Source
	var failures []string
	if opts.Result.Brief != nil {
		sources = append([]research.Source{}, opts.Result.Brief.Sources...)
		sort.Slice(sources, func(i, j int) bool { return sources[i].Index < sources[j].Index })
		failures = opts.Result.Brief.Failures
	}
	genAt := opts.GeneratedAt
	if genAt.IsZero() {
		genAt = time.Now()
	}
	meta := map[string]any{
		"topic":           opts.Topic,
		"slug":            opts.Slug,
		"generated_at":    genAt.UTC().Format(time.RFC3339),
		"iterations":      len(opts.Result.Iterations),
		"final_verdict":   opts.Result.Decision,
		"docs_count":      opts.DocsCount,
		"script_words":    opts.Result.Script.TotalWords(),
		"script_target_minutes": opts.Config.Pipeline.TargetMinutes,
		"script_wpm":      opts.Config.Pipeline.WordsPerMinute,
		"duration_seconds": opts.Result.Script.DurationSec(opts.Config.Pipeline.WordsPerMinute),
		"sources":         sources,
		"failures":        failures,
		"config":          snap,
		"audio":           audioMeta(opts),
	}
	metaPath := filepath.Join(dir, "meta.json")
	if err := writeJSON(metaPath, meta); err != nil {
		return nil, err
	}
	written = append(written, metaPath)

	return written, nil
}

func audioMeta(opts Options) map[string]any {
	if !opts.RenderedAudio {
		return map[string]any{"rendered": false}
	}
	return map[string]any{
		"rendered":  true,
		"path":      opts.AudioPath,
		"format":    opts.AudioFormat,
		"chunks":    opts.AudioChunks,
		"seconds":   opts.AudioSeconds,
	}
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
