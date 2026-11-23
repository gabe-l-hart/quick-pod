// Package research drives the research agent: a bounded tool-calling loop that
// gathers context from configured sources and produces a cited Markdown brief.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gabe-l-hart/podcasts/internal/documents"
	"github.com/gabe-l-hart/podcasts/internal/llm"
	"github.com/gabe-l-hart/podcasts/internal/prompts"
	"github.com/gabe-l-hart/podcasts/internal/search"
)

// Source is one registered source (citation registry entry).
type Source struct {
	Index   int    `json:"index"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Kind    string `json:"kind"` // search | arxiv | doc | trusted
}

// Brief is the research output.
type Brief struct {
	Topic     string       `json:"topic"`
	Text      string       `json:"text"` // final markdown brief
	Sources   []Source    `json:"sources"`
	Failures  []string    `json:"failures,omitempty"`
	DocsCount int          `json:"docs_count"`
}

// LLM is the minimal chat interface the agent needs (the same shape llm.Client has).
type LLM interface {
	Chat(ctx context.Context, msgs []llm.Message, opts llm.ChatOptions) (*llm.Response, error)
}

// Agent is one research run.
type Agent struct {
	Topic         string
	Docs          []documents.Document
	TrustedURLs   []string
	Arxiv         *search.Arxiv
	Provider      search.Provider
	Prompts  *prompts.Registry
	MaxSteps  int
	// HTTPClient is used for trusted-URL fetches.
	HTTPClient *http.Client
	// MaxSteps bounds the tool-calling loop; MaxDocChars bounds per-read doc output.
	MaxDocChars int
	// Recorder, if non-nil, is called with tool results for logging.
	Recorder func(stage string)

	// internal state
	Sources  []Source
	Failures []string
	http     *http.Client
}

var arxivURLRe = regexp.MustCompile(`^https?://arxiv\.org/`)

// NewAgent constructs the agent.
func NewAgent(topic string, docs []documents.Document, trustedURLs []string, arxiv *search.Arxiv, provider search.Provider, prompts *prompts.Registry, maxSteps int, httpClient *http.Client) *Agent {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	// auto-add arXiv as a trusted prefix (it is fetch-safe and keyless)
	trusted := append([]string{}, trustedURLs...)
	if len(trusted) == 0 || !hasPrefix(trusted, "https://arxiv.org") {
		trusted = append(trusted, "https://arxiv.org")
	}
	return &Agent{
		Topic:         topic,
		Docs:          docs,
		TrustedURLs:   trusted,
		Arxiv:         arxiv,
		Provider:      provider,
		Prompts:       prompts,
		MaxSteps:      maxSteps,
		HTTPClient:    httpClient,
		MaxDocChars:   12000,
		http:          httpClient,
	}
}

func hasPrefix(prefixes []string, p string) bool {
	for _, x := range prefixes {
		if strings.HasPrefix(x, p) || strings.HasPrefix(p, x) {
			return true
		}
	}
	return false
}

// KnownSources renders the docs in the user prompt.
func (a *Agent) knownSourcesText() string {
	if len(a.Docs) == 0 {
		return "(none — call read_document only if you need it)"
	}
	var b strings.Builder
	for i, d := range a.Docs {
		snippet := d.Text
		if len(snippet) > 500 {
			snippet = snippet[:500] + "…"
		}
		fmt.Fprintf(&b, "[doc-%d] %s (%s)\n  %s\n", i+1, d.Title, d.Source, snippet)
	}
	return strings.TrimRight(b.String(), "\n")
}

// TrustedSourcesText renders the trusted prefix list.
func (a *Agent) trustedSourcesText() string {
	if len(a.TrustedURLs) == 0 {
		return "(none)"
	}
	return strings.Join(a.TrustedURLs, "\n")
}

// Run executes the research loop.
func (a *Agent) Run(ctx context.Context, llmClient LLM) (*Brief, error) {
	a.Sources = nil
	a.Failures = nil
	if err := a.warmDocs(); err != nil {
		a.Failures = append(a.Failures, "docs: "+err.Error())
	}

	system, err := a.Prompts.Render(prompts.ResearchSystem, nil)
	if err != nil {
		return nil, err
	}
	user, err := a.Prompts.Render(prompts.ResearchUser, map[string]any{
		"Topic":          a.Topic,
		"DocsSummary":    a.knownSourcesText(),
		"TrustedSources": a.trustedSourcesText(),
	})
	if err != nil {
		return nil, err
	}
	msgs := []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}

	tools := a.tools()
	for step := 0; step < a.MaxSteps; step++ {
		resp, err := llmClient.Chat(ctx, msgs, llm.ChatOptions{Tools: tools})
		if err != nil {
			return nil, fmt.Errorf("research: LLM call (step %d): %w", step+1, err)
		}
		if len(resp.ToolCalls) == 0 {
			return &Brief{Topic: a.Topic, Text: resp.Content, Sources: a.Sources, Failures: a.Failures, DocsCount: len(a.Docs)}, nil
		}
		msgs = append(msgs, llm.Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			result, err := a.execTool(ctx, llmClient, tc)
			if err != nil {
				// surface the failure to the model so it can react
				a.Failures = append(a.Failures, fmt.Sprintf("%s: %v", tc.Function.Name, err))
				msgs = append(msgs, llm.Message{
					Role:      "tool",
					ToolCallID: tc.ID,
					Content:   "tool error: " + err.Error(),
				})
				continue
			}
			msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
	}
	// hit the step cap: ask the model for the final brief without tools
	finalMsg, err := a.Prompts.Render(prompts.ResearchUser, map[string]any{
		"Topic":          a.Topic,
		"DocsSummary":    a.knownSourcesText(),
		"TrustedSources": a.trustedSourcesText(),
	})
	_ = finalMsg
	msgs = append(msgs, llm.Message{Role: "user", Content: "You have reached the step limit. Write the final Markdown brief now from what you have gathered."})
	resp, err := llmClient.Chat(ctx, msgs, llm.ChatOptions{})
	if err != nil {
		return nil, fmt.Errorf("research: final LLM call: %w", err)
	}
	return &Brief{Topic: a.Topic, Text: resp.Content, Sources: a.Sources, Failures: a.Failures, DocsCount: len(a.Docs)}, nil
}

func (a *Agent) warmDocs() error {
	// no-op for now: docs are already normalized by the caller.
	// This hook exists so the agent can auto-register docs into the sources registry.
	for i := range a.Docs {
		d := &a.Docs[i]
		a.addSource("doc", d.Source, d.Title, "")
	}
	return nil
}

func (a *Agent) addSource(kind, url, title, snippet string) int {
	// dedupe by URL
	for _, s := range a.Sources {
		if s.URL == url && s.Title == title {
			return s.Index
		}
	}
	a.Sources = append(a.Sources, Source{Index: len(a.Sources) + 1, Title: title, URL: url, Snippet: snippet, Kind: kind})
	return len(a.Sources)
}

func (a *Agent) tools() []llm.Tool {
	var out []llm.Tool
	if a.Provider != nil {
		out = append(out, a.webSearchTool())
	}
	if a.Arxiv != nil {
		out = append(out, a.arxivSearchTool())
	}
	if len(a.TrustedURLs) > 0 || a.Arxiv != nil {
		out = append(out, a.fetchURLTool())
	}
	if len(a.Docs) > 0 {
		out = append(out, a.readDocumentTool())
	}
	return out
}

func (a *Agent) webSearchTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "web_search",
			Description: "Search the web for sources about the topic.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"n":     map[string]any{"type": "integer", "description": "how many results (1-10)"},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (a *Agent) arxivSearchTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "arxiv_search",
			Description: "Search arXiv for papers about the topic.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"n":     map[string]any{"type": "integer", "description": "how many results (1-10)"},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (a *Agent) fetchURLTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "fetch_url",
			Description: "Fetch a trusted source URL and return its text. Only trusted URLs (user-configured + arxiv.org) are allowed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{"type": "string"},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (a *Agent) readDocumentTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "read_document",
			Description: "Read one of the user-supplied documents by id (e.g. \"doc-1\"). Returns its full text.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "e.g. doc-1"},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (a *Agent) execTool(ctx context.Context, client LLM, tc llm.ToolCall) (string, error) {
	args := map[string]any{}
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
	}
	switch tc.Function.Name {
	case "web_search":
		if a.Provider == nil {
			return "web_search is not configured (search provider is \"none\"). Use arxiv_search and the provided documents, or set search.provider in the config.", nil
		}
		q, _ := args["query"].(string)
		n := intArg(args["n"], 5)
		results, err := a.Provider.Search(ctx, q, n)
		if err != nil {
			return "", err
		}
		for _, r := range results {
			a.addSource(a.Provider.Name(), r.URL, r.Title, r.Snippet)
		}
		return formatResults(results, "web_search"), nil
	case "arxiv_search":
		q, _ := args["query"].(string)
		n := intArg(args["n"], 5)
		if q == "" {
			return "", fmt.Errorf("arxiv_search requires a query")
		}
		results, err := a.Arxiv.Search(ctx, q, n)
		if err != nil {
			return "", err
		}
		for _, r := range results {
			a.addSource("arxiv", r.URL, r.Title, r.Snippet)
		}
		return formatResults(results, "arxiv_search"), nil
	case "fetch_url":
		u, _ := args["url"].(string)
		if u == "" {
			return "", fmt.Errorf("fetch_url requires a url")
		}
		if !a.allowedURL(u) {
			return "", fmt.Errorf("url %s is not in the allowed trusted sources (%s)", u, strings.Join(a.TrustedURLs, ", "))
		}
		doc, err := documents.Convert(u, a.http)
		if err != nil {
			return "", err
		}
		a.addSource("trusted", u, doc.Title, "")
		budget := a.MaxDocChars
		text := doc.Text
		if len(text) > budget {
			text = text[:budget] + " ... [truncated]"
		}
		return fmt.Sprintf("Fetched %s (%s):\n\n%s", doc.Title, u, text), nil
	case "read_document":
		id, _ := args["id"].(string)
		id = strings.ToLower(strings.TrimSpace(id))
		if !strings.HasPrefix(id, "doc-") {
			return "", fmt.Errorf("id %q must look like doc-N (e.g. doc-1)", id)
		}
		var idx int
		if _, err := fmt.Sscan(strings.TrimPrefix(id, "doc-"), &idx); err != nil || idx < 1 || idx > len(a.Docs) {
			return "", fmt.Errorf("unknown document id %q (have doc-1 .. doc-%d)", id, len(a.Docs))
		}
		d := a.Docs[idx-1]
		budget := a.MaxDocChars
		text := d.Text
		if len(text) > budget {
			text = text[:budget] + " ... [truncated]"
		}
		return fmt.Sprintf("Document %s: %s\nSource: %s (%s)\n\n%s", id, d.Title, d.Source, d.Kind, text), nil
	}
	return "", fmt.Errorf("unknown tool %q", tc.Function.Name)
}

func (a *Agent) allowedURL(u string) bool {
	for _, prefix := range a.TrustedURLs {
		if strings.HasPrefix(u, prefix) {
			return true
		}
	}
	return false
}

func intArg(v any, def int) int {
	switch n := v.(type) {
	case int:
		if n > 0 && n <= 20 {
			return n
		}
	case float64:
		if n > 0 && n <= 20 {
			return int(n)
		}
	}
	return def
}

func formatResults(results []search.Result, tool string) string {
	if len(results) == 0 {
		return fmt.Sprintf("%s: no results", tool)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s found %d result(s):\n", tool, len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "[%d] %s\n    %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			sn := r.Snippet
			if len(sn) > 400 {
				sn = sn[:400] + "…"
			}
			fmt.Fprintf(&b, "    %s\n", sn)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// SortedSources returns the registry in order.
func (a *Agent) SortedSources() []Source {
	sort.Slice(a.Sources, func(i, j int) bool { return a.Sources[i].Index < a.Sources[j].Index })
	return a.Sources
}
