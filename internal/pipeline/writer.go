package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gabe-l-hart/podcasts/internal/documents"
	"github.com/gabe-l-hart/podcasts/internal/llm"
	"github.com/gabe-l-hart/podcasts/internal/prompts"
)

var scriptSchema = map[string]any{
	"title": "Script",
	"type":  "object",
	"properties": map[string]any{
		"title":   map[string]any{"type": "string"},
		"summary": map[string]any{"type": "string"},
		"segments": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":      map[string]any{"type": "string", "enum": []string{"intro", "body", "synthesis", "outro"}},
					"title":     map[string]any{"type": "string"},
					"text":      map[string]any{"type": "string"},
					"approx_sec": map[string]any{"type": "number"},
				},
				"required": []string{"kind", "text"},
			},
		},
	},
	"required": []string{"title", "segments"},
}

// WriteScript produces one script from the brief.
func WriteScript(ctx context.Context, llmClient LLM, registry *prompts.Registry, topic, summary string, docs []documents.Document, issues []string, cfg pipelineConfig) (*Script, error) {
	system, err := registry.Render(prompts.ScriptSystem, nil)
	if err != nil {
		return nil, err
	}
	issuesText := ""
	if len(issues) > 0 {
		issuesText = "\n" + strings.Join(issues, "\n") + "\n"
	}
	user, err := registry.Render(prompts.ScriptUser, map[string]any{
		"Topic":         topic,
		"TargetMinutes": cfg.TargetMinutes,
		"WordBudget":    cfg.WordBudget,
		"WPM":           cfg.WordsPerMinute,
		"Brief":         summary,
		"DocsSummary":   docsSummary(docs),
		"Issues":        strings.TrimSpace(issuesText),
	})
	if err != nil {
		return nil, err
	}
	resp, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, llm.ChatOptions{ResponseFormat: scriptSchema})
	if err != nil {
		return nil, fmt.Errorf("script writer: %w", err)
	}
	script, err := parseScript(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("script writer: %w (content: %.300q)", err, resp.Content)
	}
	// re-derive approx_sec from word count if missing
	wpm := cfg.WordsPerMinute
	for i := range script.Segments {
		wordCount := len(strings.Fields(script.Segments[i].Text))
		sec := float64(wordCount) / float64(wpm) * 60
		if script.Segments[i].ApproxSec <= 0 {
			script.Segments[i].ApproxSec = sec
		}
	}
	return script, nil
}

type pipelineConfig struct {
	TargetMinutes int
	WordsPerMinute int
	WordBudget int
}

// docsSummary renders a compact summary of the user's documents.
func docsSummary(docs []documents.Document) string {
	if len(docs) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, d := range docs {
		snippet := d.Text
		if len(snippet) > 700 {
			snippet = snippet[:700] + "…"
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", d.Title, d.Kind, snippet)
	}
	return strings.TrimRight(b.String(), "\n")
}

var firstBraceRe = regexp.MustCompile(`\{[\s\S]*`)
var lastBraceRe = regexp.MustCompile(`[\s\S]*\}`)

func parseScript(content string) (*Script, error) {
	content = strings.TrimSpace(content)
	// tolerate code fences and surrounding prose
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	content = content[start : end+1]
	var script Script
	if err := json.Unmarshal([]byte(content), &script); err != nil {
		return nil, fmt.Errorf("invalid script JSON: %w", err)
	}
	if script.Title == "" {
		return nil, fmt.Errorf("script missing title")
	}
	if len(script.Segments) == 0 {
		return nil, fmt.Errorf("script has no segments")
	}
	// fill title from segment kinds if missing
	for i := range script.Segments {
		if script.Segments[i].Title == "" {
			script.Segments[i].Title = strings.Title(script.Segments[i].Kind)
		}
		if script.Segments[i].Kind == "" {
			script.Segments[i].Kind = "body"
		}
	}
	return &script, nil
}
