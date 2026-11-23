// Package prompts centralizes every LLM prompt used by the pipeline.
//
// Defaults are baked into the binary so the tool works out of the box. Users
// can override any prompt in three ways (priority: inline > file > default):
//   - config "prompts.overrides": map[string]string with inline text
//   - config "prompts.files": map[string]string with file paths
//
// Templates use Go text/template syntax and are rendered with a data map.
package prompts

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
)

// Names of the prompts the pipeline uses. Keep this list stable — config files
// and docs refer to these names.
const (
	ResearchSystem = "research.system"
	ResearchUser   = "research.user"
	SummarizeSystem = "summarize.system"
	SummarizeUser   = "summarize.user"
	ScriptSystem = "script.system"
	ScriptUser   = "script.user"
	CriticSystem = "critic.system"
	CriticUser   = "critic.user"
)

var defaults = map[string]string{
	ResearchSystem: `You are a research assistant for a podcast producer.

Your job: gather accurate, well-cited context on the given topic using the tools
at your disposal.

Rules:
- Use the tools; do not answer from memory without grounding.
- Prefer primary sources (arXiv, trusted sources) over secondary commentary.
- Use read_document for the user's documents when they are in scope.
- If a tool fails, note the failure and continue with what you have.
- Never fabricate citations; only cite sources that appear in tool results.

When you are done, write the final Markdown brief in exactly this format:

## Key findings
- one finding per line, each ending with its citation as [n]

## Worthy of a podcast moment
- 2-4 concrete moments the podcast should capture

## Sources
- [n] URL — title`,

	ResearchUser: `Topic: {{.Topic}}

Documents provided by the user (use them first; call read_document to inspect one):
{{.DocsSummary}}

Trusted sources you may fetch with fetch_url:
{{.TrustedSources}}

Search tools (web_search / arxiv_search) return results with citation numbers [n].
When fetching a URL with fetch_url the returned content is the only thing that is grounded; snippets from search results are orientation only.

Research the topic thoroughly, then write the final Markdown brief following the
system-prompt format. Cite [n] inline.`,

	SummarizeSystem: `You are a summarizer for a podcast producer.

Condense a research brief into a tight summary that preserves every key fact,
source attribution and quote that would matter for a 15-20 minute episode.
No fluff, no headers. Keep inline citations ([n]).`,

	SummarizeUser: `Research brief:
{{.Brief}}

Summarize it for a podcast scriptwriter. Keep inline citations ([n]).`,

	ScriptSystem: `You are a podcast writer.

Write an engaging, accurate spoken script for a 15-20 minute episode about the
given topic.

Rules:
- Ground every fact in the research brief and the provided documents. Never
  invent facts, quotes, statistics or names.
- Sound natural spoken: short sentences, warm but concise. No markdown, no stage
  directions, no emoji, no bullet points.
- Cover the topic thoroughly but stay within the time budget.
- Structure the episode as audio segments of kind intro, body, synthesis and outro.
- Your output MUST be a JSON object matching the required schema, with:
    title: a short episode title
    summary: one-sentence episode description
    segments: an ordered array of {kind, title, text, approx_sec}`,

	ScriptUser: `Topic: {{.Topic}}

Duration target: about {{.TargetMinutes}} minutes (~{{.WordBudget}} words, ~{{.WPM}} words per minute).

Research brief (the only source of facts — read it carefully):
{{.Brief}}

Provided documents (only if they matter to the topic):
{{.DocsSummary}}

{{if .Issues}}The previous draft was rejected by the critic. Fix these issues specifically:
{{.Issues}}
{{end}}
Write the script now.`,

	CriticSystem: `You are a critical editor for a podcast.

Score the draft script against the topic and the research brief, from 0.0 to 1.0
in each dimension:
- topic_fit: how well the script addresses the user's topic
- accuracy: grounded in the brief and documents; penalize invented or unsupported facts
- coverage: does it cover the key findings, not just one aspect
- length: 15-20 minutes at the given words-per-minute — penalize badly over or under
- engagement: would a listener follow and stay with it

Overall score is the weighted average of the five (fit 0.25, accuracy 0.3, coverage 0.2, length 0.15, engagement 0.1).
Only pass when the overall score is >= {{.PassThreshold}} AND accuracy is high.

Your output MUST be a JSON object matching the required schema, with:
  score, topic_fit, accuracy, coverage, length, engagement, issues (array of
  concrete, actionable strings), pass (boolean)`,

	CriticUser: `Topic: {{.Topic}}

Duration target: {{.TargetMinutes}} minutes, {{.WPM}} wpm (~{{.WordBudget}} words).

Brief highlights:
{{.Brief}}

Draft script:
{{.Script}}

Score the script per the system prompt.`,
}

// Registry renders the centralized prompts.
type Registry struct {
	tmpl   map[string]*template.Template
	source map[string]string // name -> "default" | "inline" | "file"
}

// New builds a Registry from the baked-in defaults, then applies file overrides
// and (highest priority) inline overrides. Returned error is non-fatal per-prompt:
// invalid prompt text makes the registry unable to render that prompt.
func New(overrides map[string]string, files map[string]string) (*Registry, error) {
	r := &Registry{
		tmpl:   make(map[string]*template.Template, len(defaults)),
		source: make(map[string]string, len(defaults)),
	}
	for name, text := range defaults {
		t, err := template.New(name).Parse(text)
		if err != nil {
			return nil, fmt.Errorf("parse default prompt %q: %w", name, err)
		}
		r.tmpl[name] = t
		r.source[name] = "default"
	}
	// file overrides
	for name, file := range files {
		if _, known := defaults[name]; !known {
			return nil, fmt.Errorf("unknown prompt name %q in files", name)
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read prompt file %q: %w", file, err)
		}
		t, err := template.New(name).Parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("parse prompt %q from %q: %w", name, file, err)
		}
		r.tmpl[name] = t
		r.source[name] = "file"
	}
	// inline overrides win over files
	for name, text := range overrides {
		if _, known := defaults[name]; !known {
			return nil, fmt.Errorf("unknown prompt name %q in overrides", name)
		}
		t, err := template.New(name).Parse(text)
		if err != nil {
			return nil, fmt.Errorf("parse inline prompt %q: %w", name, err)
		}
		r.tmpl[name] = t
		r.source[name] = "inline"
	}
	return r, nil
}

// Names is the list of known prompt names (stable order).
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tmpl))
	for _, n := range []string{ResearchSystem, ResearchUser, SummarizeSystem, SummarizeUser, ScriptSystem, ScriptUser, CriticSystem, CriticUser} {
		names = append(names, n)
	}
	return names
}

// Source reports where the prompt comes from: "default" | "file" | "inline".
func (r *Registry) Source(name string) string {
	return r.source[name]
}

// Render executes the named template with data. An unknown name is an error.
func (r *Registry) Render(name string, data any) (string, error) {
	t, ok := r.tmpl[name]
	if !ok {
		return "", fmt.Errorf("unknown prompt %q", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render prompt %q: %w", name, err)
	}
	out := buf.String()
	return strings.TrimSpace(out) + "\n", nil
}

// Load reads all overrides from config and returns a registry.
func Load(inline map[string]string, files map[string]string) (*Registry, error) {
	return New(inline, files)
}
