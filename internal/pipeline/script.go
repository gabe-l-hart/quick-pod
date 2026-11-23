// Package pipeline implements the writer, critic and iteration loop.
package pipeline

import (
	"fmt"
	"strings"
)

// Script is one episode script.
type Script struct {
	Title    string         `json:"title"`
	Summary  string         `json:"summary"`
	Segments []Segment      `json:"segments"`
}

// Segment is one spoken section.
type Segment struct {
	Kind      string  `json:"kind"` // intro | body | synthesis | outro
	Title     string  `json:"title"`
	Text      string  `json:"text"`
	ApproxSec float64 `json:"approx_sec"`
}

// TotalWords is the script's total word count.
func (s *Script) TotalWords() int {
	n := 0
	for _, seg := range s.Segments {
		n += len(strings.Fields(seg.Text))
	}
	return n
}

// DurationSec estimates the spoken duration.
func (s *Script) DurationSec(wpm int) float64 {
	if wpm <= 0 {
		wpm = 150
	}
	return float64(s.TotalWords()) / float64(wpm) * 60
}

// RenderMarkdown renders the script for the artifacts/output.
func (s *Script) RenderMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", s.Title)
	if s.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", s.Summary)
	}
	fmt.Fprintf(&b, "_~%d words, ~%.1f minutes at 150 wpm_\n\n", s.TotalWords(), s.DurationSec(150)/60)
	for _, seg := range s.Segments {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", seg.Title, seg.Text)
	}
	return b.String()
}

// Critique is the critic's evaluation of the script.
type Critique struct {
	Score      float64  `json:"score"`
	TopicFit   float64  `json:"topic_fit"`
	Accuracy   float64  `json:"accuracy"`
	Coverage   float64  `json:"coverage"`
	Length     float64  `json:"length"`
	Engagement float64  `json:"engagement"`
	Issues     []string `json:"issues"`
	Pass       bool     `json:"pass"`
}

// ScriptText renders the script as plain text for the critic prompt and TTS.
func ScriptText(s *Script) string {
	var b strings.Builder
	for _, seg := range s.Segments {
		fmt.Fprintf(&b, "[%s] %s\n%s\n\n", seg.Kind, strings.TrimSpace(seg.Title), strings.TrimSpace(seg.Text))
	}
	return b.String()
}
