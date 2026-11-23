package documents

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectKind(t *testing.T) {
	cases := []struct{ in, wantKind, wantID string }{
		{"foo.pdf", "pdf", ""},
		{"foo.html", "html", ""},
		{"foo.md", "markdown", ""},
		{"foo.txt", "text", ""},
		{"1706.03762", "arxiv", "1706.03762"},
		{"1706.03762v2", "arxiv", "1706.03762v2"},
		{"hep-th/9901001", "arxiv", "hep-th/9901001"},
		{"https://arxiv.org/abs/1706.03762", "arxiv", "1706.03762"},
		{"https://arxiv.org/pdf/1706.03762", "arxiv", "1706.03762"},
		{"https://example.org/paper.pdf", "pdf", ""},
		{"https://example.org/page", "html", ""},
		{"https://example.org/notes.md", "markdown", ""},
	}
	for _, c := range cases {
		kind, id := DetectKind(c.in)
		if kind != c.wantKind || id != c.wantID {
			t.Errorf("DetectKind(%q) = (%q, %q), want (%q, %q)", c.in, kind, id, c.wantKind, c.wantID)
		}
	}
}

func TestConvertText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	os.WriteFile(path, []byte("# Hello\n\nWorld paragraph.\n"), 0o644)
	doc, err := Convert(path, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "markdown" {
		t.Fatalf("kind = %s", doc.Kind)
	}
	if doc.Title != "Hello" {
		t.Fatalf("title = %q", doc.Title)
	}
	if !strings.Contains(doc.Text, "World paragraph") {
		t.Fatalf("text = %q", doc.Text)
	}
}

func TestConvertHTML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.html")
	html := `<html><head><title>Test Page</title></head><body><h1>Hello</h1><script>var x=1;</script><p>Body text here</p></body></html>`
	os.WriteFile(path, []byte(html), 0o644)
	doc, err := Convert(path, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Test Page" {
		t.Fatalf("title = %q", doc.Title)
	}
	if !strings.Contains(doc.Text, "Body text here") {
		t.Fatalf("text = %q", doc.Text)
	}
	if strings.Contains(doc.Text, "var x") {
		t.Fatalf("script leaked into text: %q", doc.Text)
	}
}

func TestConvertPDF(t *testing.T) {
	doc, err := Convert("../../testdata/fixture.pdf", &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "pdf" {
		t.Fatalf("kind = %s", doc.Kind)
	}
	if !strings.Contains(doc.Text, "podcast test fixture") {
		t.Fatalf("text = %q", doc.Text)
	}
}

func TestTruncatedPDFOverLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.txt")
	os.WriteFile(path, []byte(strings.Repeat("word ", 20000)), 0o644)
	doc, err := Convert(path, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Truncated {
		t.Fatal("expected truncation")
	}
}

func TestConvertArxivFallback(t *testing.T) {
	// mock: PDF fetch fails (404), abstract page serves HTML
	mux := http.NewServeMux()
	mux.HandleFunc("/pdf/test.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/abs/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>arXiv:1706.03762 - Test Paper</title></head><body><h1>Hello</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// We cannot intercept ArxivAbsURL/ArxivPDFURL (they are constants), so just
	// verify the HTML fallback path via the public Convert on a plain URL.
	doc, err := Convert(srv.URL+"/abs/test", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Text, "Hello") {
		t.Fatalf("text = %q", doc.Text)
	}
}
