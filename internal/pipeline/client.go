package pipeline

import (
	"context"

	"github.com/gabe-l-hart/podcasts/internal/llm"
)

// LLM is the minimal chat interface the pipeline needs.
// Either the real llm.Client or a mock implements it.
type LLM interface {
	Chat(ctx context.Context, msgs []llm.Message, opts llm.ChatOptions) (*llm.Response, error)
}

