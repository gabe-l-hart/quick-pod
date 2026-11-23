// Package mock provides a deterministic LLM for offline development and tests.
// It implements the pipeline/research LLM interfaces — no HTTP calls are made.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gabe-l-hart/podcasts/internal/llm"
)

// Client is a deterministic mock of the primary LLM. Behavior depends on the
// system prompt: the research agent gets a tool call then a brief, the
// writer gets a valid JSON script, the critic gets a JSON critique.
type Client struct {
	// CriticFailures is how many critic calls should fail first.
	CriticFailures int
	criticCalls    int
	// ResearchStep tracks the research agent's tool-call sequence.
	ResearchStep *int
}

// New constructs a mock that fails the first CriticFailures critic calls (pass=false).
func New(criticFailures int) *Client {
	return &Client{CriticFailures: criticFailures, ResearchStep: new(int)}
}

// Chat implements llm.Client (struct) — this satisfies the interface via value.
func (c *Client) Chat(ctx context.Context, msgs []llm.Message, opts llm.ChatOptions) (*llm.Response, error) {
	system := ""
	for _, m := range msgs {
		if m.Role == "system" {
			system = m.Content.(string)
			break
		}
	}

	// Research agent: return the final brief directly (no tool calls)
	if strings.Contains(system, "research assistant") {
		return &llm.Response{Content: "## Key findings\n- Mock finding one [1]\n- Mock finding two [2]\n\n## Worthy of a podcast moment\n- The surprise at the end [1]\n\n## Sources\n- [1] https://arxiv.org/abs/1706.03762 — Mock paper one\n- [2] https://example.org/mock — Mock paper two"}, nil
	}

	// Summarizer: pass through the brief
	if strings.Contains(system, "summarizer for a podcast producer") {
		for _, m := range msgs {
			if m.Role == "user" {
				if s, ok := m.Content.(string); ok && strings.Contains(s, "Research brief") {
					// echo back a short summary
					idx := strings.Index(s, "Research brief:\n")
					summary := strings.TrimSpace(s[idx+len("Research brief:\n"):])
					if len(summary) > 800 {
						summary = summary[:800] + "…"
					}
					return &llm.Response{Content: summary}, nil
				}
			}
		}
		return &llm.Response{Content: "Mock summary"}, nil
	}

	// Writer: return a valid JSON script
	if strings.Contains(system, "podcast writer") {
		script := map[string]any{
			"title":   "Mock Episode",
			"summary": "A mock episode used to exercise the pipeline.",
			"segments": []map[string]any{
				{"kind": "intro", "title": "Intro", "text": "Welcome to this mock episode, where today we explore the topic.", "approx_sec": 12},
				{"kind": "body", "title": "Body", "text": "The first finding shows that the subject is rich and worth a long listen. We should dig in, look at the second finding, and make sure the listener leaves with something concrete to think about.", "approx_sec": 90},
				{"kind": "synthesis", "title": "Synthesis", "text": "Pulling it together, the topic rewards curiosity and careful listening.", "approx_sec": 30},
				{"kind": "outro", "title": "Outro", "text": "Thanks for listening. See you next time.", "approx_sec": 10},
			},
		}
		b, _ := json.Marshal(script)
		return &llm.Response{Content: string(b)}, nil
	}

	// Critic: return a JSON critique; first CriticFailures calls fail
	if strings.Contains(system, "critical editor") {
		c.criticCalls++
		pass := c.criticCalls > c.CriticFailures
		critique := map[string]any{
			"score":      pick(0.42, 0.86, pass),
			"topic_fit":  pick(0.4, 0.9, pass),
			"accuracy":   pick(0.4, 0.85, pass),
			"coverage":   pick(0.4, 0.88, pass),
			"length":     pick(0.4, 0.8, pass),
			"engagement": pick(0.4, 0.87, pass),
			"issues":     []string{"Make the body segment longer and cite the evidence more concretely."},
			"pass":       pass,
		}
		if pass {
			critique["issues"] = []string{}
		}
		b, _ := json.Marshal(critique)
		return &llm.Response{Content: string(b)}, nil
	}

	// Fallback
	return &llm.Response{Content: "Mock: unsupported prompt"}, nil
}

func pick(fail, success float64, pass bool) float64 {
	if pass {
		return success
	}
	return fail
}

var _ = fmt.Sprintf
