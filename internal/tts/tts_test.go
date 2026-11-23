package tts

import (
	"strings"
	"testing"
)

func TestChunksBasic(t *testing.T) {
	text := strings.Repeat("This is a sentence. ", 100)
	chunks := Chunks(text, 100)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 100 {
			t.Fatalf("chunk over limit: %d chars", len(c))
		}
	}
}

func TestChunksShortText(t *testing.T) {
	chunks := Chunks("one sentence", 4096)
	if len(chunks) != 1 || chunks[0] != "one sentence" {
		t.Fatalf("chunks = %v", chunks)
	}
}

func TestChunksNeverDropsSentences(t *testing.T) {
	text := "First sentence. Second sentence. Third sentence."
	chunks := Chunks(text, 100)
	joined := strings.Join(chunks, " ")
	if !strings.Contains(joined, "First") || !strings.Contains(joined, "Third") {
		t.Fatalf("chunks = %v", chunks)
	}
}
