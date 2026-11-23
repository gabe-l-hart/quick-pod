package documents

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// convertPDF extracts text from a PDF, from a local path or a URL.
func convertPDF(input string, httpClient *http.Client) (*Document, error) {
	tmpSource := input
	tmpPath := ""

	if isURL(input) {
		req, err := http.NewRequest(http.MethodGet, input, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "podcasts/1.0")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch pdf %s: %w", input, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch pdf %s: HTTP %d", input, resp.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return nil, fmt.Errorf("read pdf body: %w", err)
		}
		tmpFile, err := os.CreateTemp("", "podcasts-*.pdf")
		if err != nil {
			return nil, fmt.Errorf("create temp pdf: %w", err)
		}
		tmpPath = tmpFile.Name()
		defer func() {
			tmpFile.Close()
			os.Remove(tmpPath)
		}()
		if _, err := tmpFile.Write(raw); err != nil {
			return nil, fmt.Errorf("write temp pdf: %w", err)
		}
		tmpFile.Close()
		tmpSource = tmpPath
	}

	f, reader, err := pdf.Open(tmpSource)
	if err != nil {
		return nil, fmt.Errorf("open pdf %s: %w", input, err)
	}
	defer f.Close()
	readerText, err := reader.GetPlainText()
	if err != nil {
		return nil, fmt.Errorf("extract text %s: %w", input, err)
	}
	raw, err := io.ReadAll(readerText)
	if err != nil {
		return nil, fmt.Errorf("read pdf text %s: %w", input, err)
	}
	text := string(raw)
	text, truncated := truncate(text, defaultMaxChars)
	title := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	if isURL(input) {
		// e.g. "1706.03762.pdf" -> "1706.03762"
		title = strings.TrimSuffix(title, ".pdf")
	}
	return &Document{Source: input, Kind: "pdf", Title: title, Text: text, Truncated: truncated}, nil
}
