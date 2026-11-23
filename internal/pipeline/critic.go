package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gabe-l-hart/podcasts/internal/llm"
	"github.com/gabe-l-hart/podcasts/internal/prompts"
)

var critiqueSchema = map[string]any{
	"title": "CriticEvaluation",
	"type":  "object",
	"properties": map[string]any{
		"score":      map[string]any{"type": "number"},
		"topic_fit":  map[string]any{"type": "number"},
		"accuracy":   map[string]any{"type": "number"},
		"coverage":   map[string]any{"type": "number"},
		"length":     map[string]any{"type": "number"},
		"engagement": map[string]any{"type": "number"},
		"issues":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"pass":       map[string]any{"type": "boolean"},
	},
	"required": []string{"score", "issues", "pass"},
}

// CritiqueScript scores a script against the brief.
func CritiqueScript(ctx context.Context, llmClient LLM, registry *prompts.Registry, topic, summary string, script *Script, cfg pipelineConfig, passThreshold float64) (*Critique, error) {
	system, err := registry.Render(prompts.CriticSystem, map[string]any{
		"PassThreshold": passThreshold,
	})
	if err != nil {
		return nil, err
	}
	user, err := registry.Render(prompts.CriticUser, map[string]any{
		"Topic":         topic,
		"TargetMinutes": cfg.TargetMinutes,
		"WPM":           cfg.WordsPerMinute,
		"WordBudget":    cfg.WordBudget,
		"Brief":         summary,
		"Script":        ScriptText(script),
	})
	if err != nil {
		return nil, err
	}
	resp, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, llm.ChatOptions{
		ResponseFormat: critiqueSchema,
		Grammar:        "",  // caller may fill in via ChatOptions at a higher layer
	})
	if err != nil {
		return nil, fmt.Errorf("critic: %w", err)
	}
	critique, err := parseCritique(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("critic: %w (content: %.300q)", err, resp.Content)
	}
	return critique, nil
}

var _ = regexp.MustCompile

func parseCritique(content string) (*Critique, error) {
	content = strings.TrimSpace(content)
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
	var c Critique
	if err := json.Unmarshal([]byte(content), &c); err != nil {
		return nil, fmt.Errorf("invalid critique JSON: %w", err)
	}
	return &c, nil
}
