// Package llm is the single OpenAI-compatible chat-completions client used by
// the primary LLM and the critic.
//
// It covers OpenAI, Ollama, llama.cpp's server and similar. Structured output
// supports both the OpenAI "response_format" shape and llama.cpp's top-level
// "json_schema" field (and an optional raw GBNF grammar), selected by config.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Message is one chat message.
type Message struct {
	Role       string       `json:"role"`
	Content    any          `json:"content,omitempty"`
	ToolCalls  []ToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

// ToolCall is an assistant tool invocation echoed back in follow-up messages.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is a single function name + serialized arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes a function the model may call.
type Tool struct {
	Type     string     `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef is the JSON-schema description of one tool.
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Usage reports token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response is the decoded assistant message.
type Response struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

// ChatOptions tunes one call.
type ChatOptions struct {
	Tools          []Tool                  // nil → no tools available
	ResponseFormat any                     // "json" schema via config's schema_style (see Client.schemaStyle)
	Grammar        string                  // optional GBNF text (llama.cpp only, sent as top-level "grammar")
	MaxTokens      int                     // 0 → client default
	Temperature    *float64                // nil → client default
	ToolChoice     string                  // e.g. "auto" | "none" | "required"
}

// Client is the OpenAI-compatible chat client.
type Client struct {
	BaseURL  string
	APIKey   string
	Model    string
	Temp     float64
	MaxTokens int
	HTTP     *http.Client
	// SchemaStyle selects the structured-output request shape:
	//   "llamacpp" (default): top-level "json_schema" request field
	//   "openai": "response_format" field
	//   "": no structured output
	SchemaStyle string
	Retries     int  // default 2 (i.e. up to 3 attempts)
	// DefaultGrammar is injected as the "grammar" request field on every
	// Chat call unless the call already provides one (llama.cpp only).
	DefaultGrammar string
}

// New builds a client from the config-level settings.
func New(baseURL, apiKey, model string, temp float64, maxTokens int, schemaStyle string) *Client {
	c := &Client{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       model,
		Temp:        temp,
		MaxTokens:   maxTokens,
		HTTP:        &http.Client{Timeout: 10 * time.Minute},
		SchemaStyle: schemaStyle,
		Retries:     2,
	}
	if c.SchemaStyle == "" {
		c.SchemaStyle = "llamacpp"
	}
	return c
}

type chatRequest struct {
	Model       string       `json:"model"`
	Messages    []Message    `json:"messages"`
	Tools       []Tool       `json:"tools,omitempty"`
	ToolChoice  string       `json:"tool_choice,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	ResponseFormat any       `json:"response_format,omitempty"`
	Grammar     string       `json:"grammar,omitempty"`
	JSONSchema any           `json:"json_schema,omitempty"`
}

// Chat sends one request (with retries) and returns the assistant message.
func (c *Client) Chat(ctx context.Context, msgs []Message, opts ChatOptions) (*Response, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("llm: base_url is empty")
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.MaxTokens
	}
	reqBody := chatRequest{
		Model:     c.Model,
		Messages:  msgs,
		ToolChoice: opts.ToolChoice,
		MaxTokens: maxTokens,
	}
	if opts.Temperature != nil {
		reqBody.Temperature = opts.Temperature
	} else {
		t := c.Temp
		reqBody.Temperature = &t
	}
	if len(opts.Tools) > 0 {
		reqBody.Tools = opts.Tools
		if reqBody.ToolChoice == "" {
			reqBody.ToolChoice = "auto"
		}
	}
	// Structured output: one path is chosen per SchemaStyle.
	grammar := opts.Grammar
	if grammar == "" {
		grammar = c.DefaultGrammar
	}
	if grammar != "" {
		// GBNF is llama.cpp-only; send it as a top-level field and do NOT send json_schema.
		reqBody.Grammar = grammar
	} else if opts.ResponseFormat != nil {
		if c.SchemaStyle == "openai" {
			// OpenAI: response_format.json_schema = {name, strict, schema}
			reqBody.ResponseFormat = map[string]any{
				"type":       "json_schema",
				"json_schema": map[string]any{
					"name":  "podcast_structured",
					"strict": true,
					"schema": opts.ResponseFormat,
				},
			}
		} else {
			// llama.cpp: top-level json_schema field with the raw schema
			reqBody.JSONSchema = opts.ResponseFormat
		}
	}

	url, err := url.JoinPath(c.BaseURL, "chat/completions")
	if err != nil {
		return nil, fmt.Errorf("llm: bad base_url %q: %w", c.BaseURL, err)
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("llm: encode request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			wait := backoff(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("llm: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
					wait := time.Duration(secs) * time.Second
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(wait):
					}
					continue
				}
			}
			lastErr = fmt.Errorf("llm: HTTP %d from %s: %s", resp.StatusCode, url, truncate(string(respBody), 400))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("llm: HTTP %d from %s: %s", resp.StatusCode, url, truncate(string(respBody), 400))
		}
		parsed, err := parseResponse(respBody)
		if err != nil {
			return nil, fmt.Errorf("llm: parse response: %w", err)
		}
		return parsed, nil
	}
	return nil, fmt.Errorf("llm: failed after %d attempts: %w", c.Retries+1, lastErr)
}

// parseResponse decodes the OpenAI-style chat-completions body.
func parseResponse(body []byte) (*Response, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content   any      `json:"content"`
				ToolCalls []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response (body: %s)", truncate(string(body), 200))
	}
	ch := raw.Choices[0]
	resp := &Response{Content: nilContent(ch.Message.Content)}
	for _, tc := range ch.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	resp.Usage = Usage{
		PromptTokens:     raw.Usage.PromptTokens,
		CompletionTokens: raw.Usage.CompletionTokens,
		TotalTokens:      raw.Usage.TotalTokens,
	}
	return resp, nil
}

// nilContent handles content that is a string or an array of content parts.
func nilContent(c any) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		out := ""
		for _, part := range v {
			switch p := part.(type) {
			case map[string]any:
				if t, ok := p["text"].(string); ok {
					out += t
				}
			}
		}
		return out
	default:
		return fmt.Sprintf("%v", v)
	}
}

func backoff(attempt int) time.Duration {
	// 500ms, 1s, 2s, 4s (capped at 4s)
	d := 500 * time.Millisecond
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > 4*time.Second {
			d = 4 * time.Second
			break
		}
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
