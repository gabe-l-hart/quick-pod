// Package search implements the research tools' backing sources: arXiv (keyless)
// and the general search providers (Brave, Tavily, Serper).
package search

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result is one search hit.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Arxiv queries export.arxiv.org/api/query.
type Arxiv struct {
	HTTP    *http.Client
	Endpoint string // default https://export.arxiv.org/api/query
}

// NewArxiv constructs an Arxiv client with the real endpoint (overridable in tests).
func NewArxiv(httpClient *http.Client) *Arxiv {
	return &Arxiv{HTTP: httpClient, Endpoint: "https://export.arxiv.org/api/query"}
}

type arxivEntry struct {
	ID         string   `xml:"id"`
	Title      string   `xml:"title"`
	Summary    string   `xml:"summary"`
	Published  string   `xml:"published"`
	Authors    []struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

type arxivFeed struct {
	Entries []arxivEntry `xml:"entry"`
}

// Search runs one arXiv query (max 20 results).
func (a *Arxiv) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	if maxResults <= 0 {
		maxResults = 5
	}
	// Sanitize: arXiv query language uses + for spaces, AND/OR/NOT; do a conservative cleanup.
	q := strings.Map(func(r rune) rune {
		if r == '"' || r == '&' || r == '<' || r == '>' {
			return -1
		}
		return r
	}, query)
	q = strings.Join(strings.Fields(q), "+")
	if q == "" {
		return nil, fmt.Errorf("empty arxiv query")
	}
	params := url.Values{}
	params.Set("search_query", "all:"+q)
	params.Set("start", "0")
	params.Set("max_results", fmt.Sprintf("%d", maxResults))
	endpoint := a.Endpoint
	if endpoint == "" {
		endpoint = "https://export.arxiv.org/api/query"
	}
	u := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "podcasts/1.0")
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arxiv search: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse arxiv feed: %w", err)
	}
	results := make([]Result, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		title := strings.Join(strings.Fields(e.Title), " ")
		snippet := strings.Join(strings.Fields(e.Summary), " ")
		if len(snippet) > 600 {
			snippet = snippet[:600] + "…"
		}
		id := e.ID
		if !strings.HasSuffix(id, ".pdf") {
			// keep the abs page URL
		}
		results = append(results, Result{Title: title, URL: id, Snippet: snippet})
	}
	return results, nil
}

func (a *Arxiv) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Provider is the general (non-arxiv) search backing.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string, maxResults int) ([]Result, error)
}

// NewProvider constructs the configured general search provider.
func NewProvider(kind, apiKey string, httpClient *http.Client) (Provider, error) {
	switch strings.ToLower(kind) {
	case "", "none":
		return nil, nil
	case "brave":
		return NewBrave(apiKey, httpClient), nil
	case "tavily":
		return NewTavily(apiKey, httpClient), nil
	case "serper":
		return NewSerper(apiKey, httpClient), nil
	}
	return nil, fmt.Errorf("unsupported search provider %q", kind)
}

// Brave is a Brave Search API client.
type Brave struct {
	APIKey   string
	HTTP     *http.Client
	Endpoint string // default https://api.search.brave.com/res/v1/web/search
}

// NewBrave constructs a Brave client with the real endpoint.
func NewBrave(apiKey string, httpClient *http.Client) *Brave {
	return &Brave{APIKey: apiKey, HTTP: httpClient, Endpoint: "https://api.search.brave.com/res/v1/web/search"}
}

func (b *Brave) Name() string { return "brave" }

func (b *Brave) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	endpoint := b.Endpoint
	if endpoint == "" {
		endpoint = "https://api.search.brave.com/res/v1/web/search"
	}
	u := endpoint + "?q=" + url.QueryEscape(query) + "&count=" + fmt.Sprintf("%d", maxResults)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.APIKey)
	req.Header.Set("User-Agent", "podcasts/1.0")
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Web struct {
			Results []struct {
				Title   string `json:"title"`
				URL     string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(body.Web.Results))
	for _, r := range body.Web.Results {
		title := r.Title
		url := r.URL
		snippet := r.Description
		if len(snippet) > 600 {
			snippet = snippet[:600] + "…"
		}
		out = append(out, Result{Title: title, URL: url, Snippet: snippet})
	}
	return out, nil
}

func (b *Brave) httpClient() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Tavily is a Tavily Search API client.
type Tavily struct {
	APIKey   string
	HTTP     *http.Client
	Endpoint string // default https://api.tavily.com/search
}

// NewTavily constructs a Tavily client with the real endpoint.
func NewTavily(apiKey string, httpClient *http.Client) *Tavily {
	return &Tavily{APIKey: apiKey, HTTP: httpClient, Endpoint: "https://api.tavily.com/search"}
}

func (t *Tavily) Name() string { return "tavily" }

func (t *Tavily) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	payload := map[string]any{"query": query, "max_results": maxResults}
	endpoint := t.Endpoint
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}
	raw, err := t.post(ctx, endpoint, payload)
	if err != nil {
		return nil, err
	}
	results, _ := raw["results"].([]any)
	out := make([]Result, 0, len(results))
	for _, item := range results {
		m, _ := item.(map[string]any)
		title, _ := m["title"].(string)
		url, _ := m["url"].(string)
		content, _ := m["content"].(string)
		if len(content) > 600 {
			content = content[:600] + "…"
		}
		out = append(out, Result{Title: title, URL: url, Snippet: content})
	}
	return out, nil
}

func (t *Tavily) post(ctx context.Context, url string, payload map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)
	req.Header.Set("User-Agent", "podcasts/1.0")
	resp, err := t.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tavily search: HTTP %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (t *Tavily) httpClient() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Serper is a Serper (Google Search) client.
type Serper struct {
	APIKey   string
	HTTP     *http.Client
	Endpoint string // default https://google.serper.dev/search
}

// NewSerper constructs a Serper client with the real endpoint.
func NewSerper(apiKey string, httpClient *http.Client) *Serper {
	return &Serper{APIKey: apiKey, HTTP: httpClient, Endpoint: "https://google.serper.dev/search"}
}

func (s *Serper) Name() string { return "serper" }

func (s *Serper) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	payload := map[string]any{"q": query, "num": maxResults}
	body, _ := json.Marshal(payload)
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = "https://google.serper.dev/search"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", s.APIKey)
	req.Header.Set("User-Agent", "podcasts/1.0")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("serper search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serper search: HTTP %d", resp.StatusCode)
	}
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	raw2, _ := raw["organic"].([]any)
	out := make([]Result, 0, len(raw2))
	for _, item := range raw2 {
		m, _ := item.(map[string]any)
		title, _ := m["title"].(string)
		url, _ := m["link"].(string)
		snippet, _ := m["snippet"].(string)
		if len(snippet) > 600 {
			snippet = snippet[:600] + "…"
		}
		out = append(out, Result{Title: title, URL: url, Snippet: snippet})
	}
	return out, nil
}

func (s *Serper) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}


