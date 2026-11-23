package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fakeAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/1706.03762</id>
    <title>Attention Is All You Need</title>
    <summary>In this paper we propose the Transformer architecture.</summary>
    <published>2017-06-12T00:00:00Z</published>
    <author><name>A. Vaswani</name></author>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/1409.4847</id>
    <title>The Illustrated Transformer</title>
    <summary>A figure-by-figure breakdown.</summary>
    <published>2017-05-20T00:00:00Z</published>
    <author><name>A. The Illustrated Transformer</name></author>
  </entry>
</feed>`

func TestArxivSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("search_query"), "all:") {
			t.Errorf("search_query = %s", r.URL.Query().Get("search_query"))
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, fakeAtom)
	}))
	defer srv.Close()

	a := &Arxiv{Endpoint: srv.URL + "/api/query"}
	results, err := a.Search(context.Background(), "attention mechanism", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Title != "Attention Is All You Need" {
		t.Fatalf("title = %q", results[0].Title)
	}
	if results[0].URL != "http://arxiv.org/abs/1706.03762" {
		t.Fatalf("URL = %q", results[0].URL)
	}
}

func TestBraveSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "testkey" {
			t.Errorf("bad X-Subscription-Token")
		}
		fmt.Fprint(w, `{"web":{"results":[{"title":"T","url":"https://example.com","description":"d"}]}}`)
	}))
	defer srv.Close()

	b := NewBrave("testkey", nil)
	b.Endpoint = srv.URL
	results, err := b.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com" {
		t.Fatalf("results = %+v", results)
	}
}

func TestTavilySearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"title":"T","url":"https://example.com","content":"body"}]}`)
	}))
	defer srv.Close()

	tv := NewTavily("testkey", nil)
	tv.Endpoint = srv.URL
	results, err := tv.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "T" {
		t.Fatalf("results = %+v", results)
	}
}

func TestSerperSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "testkey" {
			t.Errorf("bad X-API-KEY")
		}
		fmt.Fprint(w, `{"organic":[{"title":"T","link":"https://example.com","snippet":"body"}]}`)
	}))
	defer srv.Close()

	s := NewSerper("testkey", nil)
	s.Endpoint = srv.URL
	results, err := s.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com" {
		t.Fatalf("results = %+v", results)
	}
}
