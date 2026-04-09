package docfetcher

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
)

// SingleFileFetcher fetches a single markdown/text file from a URL.
type SingleFileFetcher struct {
	Client *http.Client
}

// Fetch retrieves a single file and returns it as a RawPage.
func (f *SingleFileFetcher) Fetch(ctx context.Context, url string) (*RawPage, error) {
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}

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

	h := sha256.Sum256(body)
	return &RawPage{
		URL:         url,
		Title:       "", // will be extracted during chunking
		Content:     string(body),
		Fingerprint: fmt.Sprintf("%x", h),
	}, nil
}
