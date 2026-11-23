// Package documents converts user-supplied inputs (local files, URLs, arXiv
// IDs) into normalized Documents.
package documents

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Document is one normalized source.
type Document struct {
	Source    string           `json:"source"`
	Kind      string           `json:"kind"` // pdf | html | arxiv | text | markdown
	Title     string           `json:"title"`
	Text      string           `json:"text"`
	Truncated bool             `json:"truncated"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// DetectKind classifies a raw input string as a local path, URL or arXiv ID.
func DetectKind(input string) (kind, arxivID string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	if isURL(input) {
		if id := arxivIDFromURL(input); id != "" {
			return "arxiv", id
		}
		lower := strings.ToLower(input)
		switch {
		case strings.HasSuffix(lower, ".pdf"):
			return "pdf", ""
		case strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm"):
			return "html", ""
		case strings.HasSuffix(lower, ".md"):
			return "markdown", ""
		case strings.HasSuffix(lower, ".txt"):
			return "text", ""
		default:
			return "html", ""
		}
	}
	if id := bareArxivID(input); id != "" {
		return "arxiv", id
	}
	lower := strings.ToLower(input)
	switch {
	case strings.HasSuffix(lower, ".pdf"):
		return "pdf", ""
	case strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm"):
		return "html", ""
	case strings.HasSuffix(lower, ".md"):
		return "markdown", ""
	case strings.HasSuffix(lower, ".txt"):
		return "text", ""
	}
	return "text", ""
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

var arxivURLRe = regexp.MustCompile(`arxiv\.org/(?:abs|pdf)/([0-9]{4}\.[0-9]{4,5}(?:v[0-9]+)?|[a-z-]+(?:\.[a-z-]+)?/[0-9]{7}(?:v[0-9]+)?)`)

// arxivIDFromURL extracts an arXiv ID from an arXiv URL.
func arxivIDFromURL(input string) string {
	if !isURL(input) {
		return ""
	}
	if matches := arxivURLRe.FindStringSubmatch(input); len(matches) == 2 {
		return matches[1]
	}
	return ""
}

var bareArxivIDRe = regexp.MustCompile(`^([0-9]{4}\.[0-9]{4,5}(?:v[0-9]+)?|[a-z-]+(?:\.[a-z-]+)?/[0-9]{7}(?:v[0-9]+)?)$`)

func bareArxivID(s string) string {
	s = strings.TrimSpace(s)
	if bareArxivIDRe.MatchString(s) {
		return s
	}
	return ""
}

// ArxivAbsURL returns the canonical abstract URL for an arXiv ID.
func ArxivAbsURL(id string) string {
	return "https://arxiv.org/abs/" + id
}

// ArxivPDFURL returns the PDF URL for an arXiv ID.
func ArxivPDFURL(id string) string {
	return "https://arxiv.org/pdf/" + id
}

// Convert downloads or reads the input and returns a normalized Document.
func Convert(input string, httpClient *http.Client) (*Document, error) {
	kind, arxivID := DetectKind(input)
	if kind == "" {
		return nil, fmt.Errorf("empty document input")
	}
	switch kind {
	case "pdf":
		return convertPDF(input, httpClient)
	case "html":
		return convertHTML(input, httpClient)
	case "text", "markdown":
		return convertText(input, httpClient)
	case "arxiv":
		return convertArxiv(arxivID, httpClient)
	}
	return nil, fmt.Errorf("unsupported document kind %q (input %s)", kind, input)
}

const defaultMaxChars = 40000 // per-doc context budget for prompting (truncation happens in the prompt builder)

// truncate normalizes and caps the text.
func truncate(text string, maxChars int) (string, bool) {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxChars {
		return text, false
	}
	return text[:maxChars] + " ... [truncated]", true
}
