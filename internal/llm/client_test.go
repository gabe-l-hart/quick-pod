package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatRoundTrip(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "chat/completions") {
			t.Fatalf("path %s", r.URL.Path)
		}
		jsn := json.NewDecoder(r.Body)
		if err := jsn.Decode(&captured); err != nil {
			t.Fatal(err)
		}
		if r.Header.Get("Authorization") != "Bearer testkey" {
			t.Fatalf("bad Authorization: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "testkey", "test-model", 0.5, 128, "llamacpp")
	resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, ChatOptions{MaxTokens: 64})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello" {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	if captured["model"] != "test-model" {
		t.Fatalf("model = %v", captured["model"])
	}
	if captured["temperature"] != 0.5 {
		t.Fatalf("temperature = %v", captured["temperature"])
	}
	if _, ok := captured["grammar"]; ok {
		t.Fatal("grammar should be absent")
	}
}

func TestChatJSONSchemaTopLevel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsn := json.NewDecoder(r.Body)
		var body map[string]any
		jsn.Decode(&body)
		if body["json_schema"] == nil {
			t.Error("expected top-level json_schema")
		}
		if body["response_format"] != nil {
			t.Error("llamacpp mode should not send response_format")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":true}"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m", 0.2, 128, "llamacpp")
	schema := map[string]any{"title": "T", "type": "object"}
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, ChatOptions{ResponseFormat: schema})
	if err != nil {
		t.Fatal(err)
	}
}

func TestChatJSONSchemaOpenAIStyle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsn := json.NewDecoder(r.Body)
		var body map[string]any
		jsn.Decode(&body)
		rf := body["response_format"]
		if rf == nil {
			t.Error("expected response_format")
		}
		if _, ok := body["json_schema"]; ok {
			t.Error("openai mode should not send top-level json_schema")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m", 0.2, 128, "openai")
	schema := map[string]any{"title": "T", "type": "object"}
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, ChatOptions{ResponseFormat: schema})
	if err != nil {
		t.Fatal(err)
	}
}

func TestChatRetryOnServerError(t *testing.T) {
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m", 0.2, 128, "llamacpp")
	c.Retries = 1
	resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q", resp.Content)
	}
	if count != 2 {
		t.Fatalf("attempts = %d", count)
	}
}

func TestToolCallRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer srv.Close()
	
	c := New(srv.URL+"/v1", "", "m", 0.2, 128, "llamacpp")
	resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, ChatOptions{
		Tools: []Tool{{Type: "function", Function: FunctionDef{Name: "web_search"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("tool call = %+v", resp.ToolCalls[0])
	}
}
