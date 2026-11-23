package documents

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// convertText reads a plain-text / markdown document (local file or URL).
func convertText(input string, httpClient *http.Client) (*Document, error) {
	var raw []byte
	var err error
	if isURL(input) {
		req, err := http.NewRequest(http.MethodGet, input, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "podcasts/1.0")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch text %s: %w", input, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch text %s: HTTP %d", input, resp.StatusCode)
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, err
		}
	} else {
		raw, err = os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("read text %s: %w", input, err)
		}
	}
	kind := "text"
	if strings.HasSuffix(strings.ToLower(input), ".md") {
		kind = "markdown"
	}
	text := string(raw)
	// title: first heading or first line
	title := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimPrefix(line, "# ")
			break
		}
		if title != "" {
			break
		}
		if line != "" {
			title = line
			break
		}
	}
	if title == "" {
		if !isURL(input) {
			title = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		} else {
			title = input
		}
	}
	text, truncated := truncate(text, defaultMaxChars)
	return &Document{Source: input, Kind: kind, Title: title, Text: text, Truncated: truncated}, nil
}
