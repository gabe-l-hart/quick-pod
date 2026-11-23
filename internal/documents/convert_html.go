package documents

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// convertHTML fetches (or reads) a page and strips tags into plain text.
func convertHTML(input string, httpClient *http.Client) (*Document, error) {
	var raw []byte
	var err error
	if isURL(input) {
		req, err := http.NewRequest(http.MethodGet, input, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "podcasts/1.0")
		req.Header.Set("Accept", "text/html,text/plain,*/*")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch html %s: %w", input, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch html %s: HTTP %d", input, resp.StatusCode)
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, err
		}
	} else {
		raw, err = os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("read html %s: %w", input, err)
		}
	}
	title := extractTitle(string(raw))
	text := stripHTML(string(raw))
	text, truncated := truncate(text, defaultMaxChars)
	if title == "" {
		title = input
	}
	return &Document{Source: input, Kind: "html", Title: title, Text: text, Truncated: truncated}, nil
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func extractTitle(html string) string {
	if matches := titleRe.FindStringSubmatch(html); len(matches) == 2 {
		return strings.TrimSpace(strings.Join(strings.Fields(matches[1]), " "))
	}
	return ""
}

var (
	scriptRe  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	tagRe     = regexp.MustCompile(`(?s)<[^>]+>`)
	blankRe   = regexp.MustCompile(`\s+`)
	htmlEntities = map[string]string{
		"&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": `"`, "&apos;": "'", "&nbsp;": " ",
	}
)

func stripHTML(html string) string {
	html = scriptRe.ReplaceAllString(html, " ")
	html = styleRe.ReplaceAllString(html, " ")
	// strip comments and tags
	html = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(html, " ")
	text := tagRe.ReplaceAllString(html, " ")
	for k, v := range htmlEntities {
		text = strings.ReplaceAll(text, k, v)
	}
	text = blankRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}
