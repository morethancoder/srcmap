package docfetcher

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/morethancoder/srcmap/internal/httpx"
	"github.com/morethancoder/srcmap/internal/logging"
)

// SingleFileFetcher fetches a single markdown/text file from a URL.
type SingleFileFetcher struct {
	Client *http.Client
}

// Fetch retrieves a single file and returns it as a RawPage.
func (f *SingleFileFetcher) Fetch(ctx context.Context, url string) (*RawPage, error) {
	client := f.Client
	if client == nil {
		client = httpx.Default()
	}

	t := logging.Stage("doc.fetch", "url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	t.Done("url", url, "bytes", len(body))

	content := string(body)
	title := ""
	ctype := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(ctype, "text/html") ||
		(ctype == "" && strings.Contains(content[:min(len(content), 512)], "<html"))
	if isHTML {
		title = extractTitle(content)
		content = extractText(content)
	}

	h := sha256.Sum256([]byte(content))
	return &RawPage{
		URL:         url,
		Title:       title,
		Content:     content,
		Fingerprint: fmt.Sprintf("%x", h),
	}, nil
}
