// Package decision implements the critic's pass/fail verdict through either
// the prompting+constrained-decoding path (built in the pipeline) or a
// TypeSafe Jev / llama.cpp /v1/systemone decision model endpoint.
package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Input is the payload the decision model evaluates.
type Input struct {
	Topic          string
	ScriptSummary  string
	CritiqueSummary string // compact JSON/text summary of the full critique
}

// Result is the decision verdict.
type Result struct {
	Pass   bool      `json:"pass"`
	Source string    `json:"source"` // "llm" | "systemone"
	Score  float64   `json:"score"`
	Reason string    `json:"reason"`
}

// Model is the decision interface (systemone + future providers).
type Model interface {
	Decide(ctx context.Context, in Input) (*Result, error)
}

// Systemone is a TypeSafe Jev / llama.cpp /v1/systemone client.
type Systemone struct {
	BaseURL string
	Path    string // default "/v1/systemone"
	APIKey  string
	Threshold float64 // noul probability needed to pass (default 0.5)
	HTTP    *http.Client
}

type systemoneRequest struct {
	State    string `json:"state"`
	Questions map[string]questionSpec `json:"questions"`
}

type questionSpec struct {
	Type        string            `json:"type"`
	Instructions string           `json:"instructions"`
	Criteria    map[string]any    `json:"criteria,omitempty"`
}

type systemoneResponse struct {
	Answers map[string]struct {
		Type      string  `json:"type"`
		Noul      float64 `json:"noul"`
		Choice   string   `json:"choice"`
		Score    float64 `json:"score"`
		Confidence float64 `json:"confidence"`
	} `json:"answers"`
}

// NewSystemone builds the adapter with sane defaults.
func NewSystemone(baseURL, path, apiKey string, threshold float64, httpClient *http.Client) *Systemone {
	if path == "" {
		path = "/v1/systemone"
	}
	if threshold == 0 {
		threshold = 0.5
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Systemone{BaseURL: baseURL, Path: path, APIKey: apiKey, Threshold: threshold, HTTP: httpClient}
}

// Decide posts the state to the decision endpoint.
func (s *Systemone) Decide(ctx context.Context, in Input) (*Result, error) {
	if s.BaseURL == "" {
		return nil, fmt.Errorf("systemone: base_url is empty")
	}
	state := in.Topic
	if in.CritiqueSummary != "" {
		state += "\n\nCritic summary: " + in.CritiqueSummary
	}
	if in.ScriptSummary != "" {
		state += "\n\nScript:\n" + in.ScriptSummary
	}
	if len(state) > 4000 {
		state = state[:4000] + " ... [truncated]"
	}

	reqBody := systemoneRequest{
		State: state,
		Questions: map[string]questionSpec{
			"pass": {
				Type:        "noul",
				Instructions: "Is this draft podcast script a good fit for the user's topic and ready to be recorded as-is?",
				Criteria: map[string]any{
					"false": "the script does not fit the topic, is weak or not ready to record",
					"true":  "the script is a good fit for the topic and ready to record",
				},
			},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("systemone: encode request: %w", err)
	}

	url := strings.TrimRight(s.BaseURL, "/") + s.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("systemone: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("systemone: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("systemone: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("systemone: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 400))
	}
	var parsed systemoneResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("systemone: parse response: %w (body: %s)", err, truncate(string(raw), 200))
	}
	q, ok := parsed.Answers["pass"]
	if !ok {
		return nil, fmt.Errorf("systemone: no answer for question \"pass\" (body: %s)", truncate(string(raw), 200))
	}
	pass := q.Noul >= s.Threshold
	return &Result{
		Pass:   pass,
		Source: "systemone",
		Score:  q.Noul,
		Reason: fmt.Sprintf("noul=%.3f (threshold=%.2f)", q.Noul, s.Threshold),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
