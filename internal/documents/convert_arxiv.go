package documents

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// convertArxiv fetches the arXiv PDF for the given ID, falling back to the
// abstract page if the PDF cannot be extracted.
func convertArxiv(id string, httpClient *http.Client) (*Document, error) {
	// 1. try the PDF
	doc, err := convertPDF(ArxivPDFURL(id), httpClient)
	if err == nil && doc != nil && strings.TrimSpace(doc.Text) != "" {
		doc.Kind = "arxiv"
		doc.Metadata = map[string]string{"arxiv_id": id, "source": "pdf"}
		doc.Title = arxivTitleFromText(doc.Text, id)
		return doc, nil
	}
	// 2. fallback: the abstract page
	doc, err = convertHTML(ArxivAbsURL(id), httpClient)
	if err != nil {
		return nil, fmt.Errorf("arxiv %s: both PDF and abstract page failed: %v", id, err)
	}
	doc.Kind = "arxiv"
	doc.Metadata = map[string]string{"arxiv_id": id, "source": "abstract"}
	if strings.HasPrefix(doc.Title, "arXiv:") || doc.Title == "" {
		doc.Title = extractArxivTitle(doc.Text, id)
	}
	return doc, nil
}

var arxivTitleRe = regexp.MustCompile(`(?is)arXiv:[0-9.]+(?:v[0-9]+)?\s*[-–]\s*(.+?)\s*\n`)

func extractArxivTitle(text, id string) string {
	// abstract pages render the title inside <h1 class="title mathjax">
	if matches := arxivTitleRe.FindStringSubmatch(text); len(matches) == 2 {
		t := strings.TrimSpace(matches[1])
		t = strings.Join(strings.Fields(t), " ")
		if len(t) > 5 {
			return t
		}
	}
	return "arXiv " + id
}

func arxivTitleFromText(text, id string) string {
	// take the first line of the PDF, usually the title
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if len(line) > 12 {
			return line
		}
	}
	return "arXiv " + id
}

var _ = strings.HasPrefix
