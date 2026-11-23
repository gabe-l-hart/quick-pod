package research

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gabe-l-hart/podcasts/internal/documents"
	"github.com/gabe-l-hart/podcasts/internal/llm"
	"github.com/gabe-l-hart/podcasts/internal/prompts"
	"github.com/gabe-l-hart/podcasts/internal/search"
)

// scriptLLM returns a scripted sequence of responses keyed on the step.
type scriptLLM struct {
	step int
	stepResponses []func(call int) *llm.Response
}

func (s *scriptLLM) Chat(ctx context.Context, msgs []llm.Message, opts llm.ChatOptions) (*llm.Response, error) {
	if s.step >= len(s.stepResponses) {
		return &llm.Response{Content: "final brief"}, nil
	}
	fn := s.stepResponses[s.step]
	s.step++
	return fn(s.step), nil
}

func TestAgentToolLoop(t *testing.T) {
	// mock arxiv endpoint
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><id>http://arxiv.org/abs/1706.03762</id><title>Attention Is All You Need</title><summary>summary</summary><published>2017-06-12</published><author><name>Vaswani</name></author></entry></feed>`)
	}))
	defer srv.Close()

	arxiv := &search.Arxiv{Endpoint: srv.URL}

	promptReg, err := prompts.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := NewAgent("test topic", nil, []string{"https://example.org"}, arxiv, nil, promptReg, 3, nil)

	// mock LLM: first call returns one tool call, then final brief
	llm := &scriptLLM{step: 0, stepResponses: []func(call int) *llm.Response{
		func(call int) *llm.Response {
			return &llm.Response{ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "arxiv_search",
					Arguments: `{"query":"attention","n":2}`,
				},
			}}}
		},
		func(call int) *llm.Response {
			return &llm.Response{Content: "## Key findings\n- one [1]\n\n## Worthy of a podcast moment\n- x\n\n## Sources\n- [1] http://arxiv.org/abs/1706.03762"}
		},
	}}

	brief, err := agent.Run(context.Background(), llm)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief.Text, "Key findings") {
		t.Fatalf("brief = %q", brief.Text)
	}
	if len(brief.Sources) == 0 {
		t.Fatalf("no sources registered: %+v", brief)
	}
	if brief.Sources[0].Kind != "arxiv" {
		t.Fatalf("source = %+v", brief.Sources[0])
	}
}

func TestAgentReportsToolError(t *testing.T) {
	arxiv := &search.Arxiv{Endpoint: "http://127.0.0.1:1/nope"} // unreachable
	promptReg, err := prompts.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := NewAgent("test topic", nil, []string{"https://example.org"}, arxiv, nil, promptReg, 2, nil)

	llm := &scriptLLM{step: 0, stepResponses: []func(call int) *llm.Response{
		func(call int) *llm.Response {
			return &llm.Response{ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "arxiv_search",
					Arguments: `{"query":"x","n":1}`,
				},
			}}}
		},
		func(call int) *llm.Response {
			return &llm.Response{Content: "final brief"}
		},
	}}

	brief, err := agent.Run(context.Background(), llm)
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.Failures) == 0 {
		t.Fatal("expected recorded tool failure")
	}
}

func TestReadDocumentTool(t *testing.T) {
	promptReg, err := prompts.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := NewAgent("test topic",
		[]documents.Document{{Source: "local.md", Kind: "markdown", Title: "Local Notes", Text: "body text"}},
		[]string{"https://example.org"},
		&search.Arxiv{}, nil, promptReg, 2, nil)

	_ = &scriptLLM{step: 0, stepResponses: []func(call int) *llm.Response{
		func(call int) *llm.Response {
			return &llm.Response{ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read_document",
					Arguments: `{"id":"doc-1"}`,
				},
			}}}
		},
		func(call int) *llm.Response {
			return &llm.Response{Content: "final brief"}
		},
	}}

	// call the tool directly
	result, err := agent.execTool(context.Background(), nil, llm.ToolCall{
		ID:   "call_1",
		Type: "function",
		Function: llm.FunctionCall{
			Name:      "read_document",
			Arguments: `{"id":"doc-1"}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "body text") {
		t.Fatalf("result = %q", result)
	}
}
