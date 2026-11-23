// Package tts is the text-to-speech client. It talks to any OpenAI-compatible
// /v1/audio/speech endpoint (cloud OpenAI or local llama.cpp llama-server).
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Client is one TTS endpoint.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	Voice   string
	Format  string
	Speed   float64
	HTTP    *http.Client
}

// New constructs the client.
func New(baseURL, apiKey, model, voice, format string, speed float64) *Client {
	if format == "" {
		format = "wav"
	}
	if speed == 0 {
		speed = 1.0
	}
	return &Client{BaseURL: baseURL, APIKey: apiKey, Model: model, Voice: voice, Format: format, Speed: speed, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

// Synthesize renders one chunk of text and returns the audio bytes.
func (c *Client) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("tts: base_url is empty")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("tts: empty text")
	}
	reqBody := map[string]any{
		"model":          c.Model,
		"input":          text,
		"voice":          c.Voice,
		"response_format": c.Format,
		"speed":          c.Speed,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	// base_url like "http://host/v1" → "/audio/speech"
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/audio/speech"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("tts: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("tts: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tts: read response: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tts: empty audio response")
	}
	return out, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// Chunks splits text into chunks that fit the TTS provider's input limit
// (default 4096 chars), preferring sentence boundaries.
func Chunks(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = 4096
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= maxChars {
		return []string{text}
	}
	// split into sentences (keep the terminator)
	re := regexp.MustCompile(`[^.!?]+[.!?]+(?:\s|$)`)
	sentences := re.FindAllString(text, -1)
	if len(sentences) == 0 {
		sentences = []string{text}
	}
	var chunks []string
	var cur strings.Builder
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		// single sentence over the limit: hard-split
		for len(sentence) > maxChars {
			if cur.Len() > 0 {
				chunks = append(chunks, cur.String())
				cur.Reset()
			}
			chunks = append(chunks, sentence[:maxChars])
			sentence = strings.TrimSpace(sentence[maxChars:])
		}
		if cur.Len() > 0 && cur.Len()+len(sentence) > maxChars {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
		cur.WriteString(sentence)
		if cur.Len() > maxChars {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
