package docfetcher

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/morethancoder/srcmap/internal/httpx"
	"github.com/morethancoder/srcmap/internal/logging"
)

// DiscoveryService handles LLM-powered doc source discovery.
// In MCP mode, the structured search prompt is issued to the agent via MCP tool.
// This struct handles the validation and classification after the agent responds.
type DiscoveryService struct {
	Client *http.Client
}

// NewDiscoveryService creates a new discovery service.
func NewDiscoveryService() *DiscoveryService {
	return &DiscoveryService{
		Client: httpx.NewClient(10 * time.Second),
	}
}

// SearchPrompt returns the structured prompt that should be sent to the agent LLM.
func SearchPrompt(name string) string {
	return fmt.Sprintf(
		`Search for the most LLM-friendly documentation source for %s. `+
			`Look for: a single markdown file (docs.md, llms.txt, llms-full.txt), `+
			`an OpenAPI/Swagger spec, or a GitHub /docs folder. `+
			`Return the single best URL and a one-sentence reason, or "none" if `+
			`nothing suitable exists. Prefer official sources.`, name)
}

// ValidateAndClassify takes a URL returned by the agent and validates it.
func (d *DiscoveryService) ValidateAndClassify(ctx context.Context, url, fallbackURL string) (*DiscoveryResult, error) {
	if url == "" || strings.EqualFold(url, "none") {
		return &DiscoveryResult{
			Found:       false,
			ContentType: ContentScrape,
			FallbackURL: fallbackURL,
		}, nil
	}

	t := logging.Stage("doc.discover", "url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating HEAD request: %w", err)
	}

	resp, err := d.Client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		status := 0
		if resp != nil {
			status = resp.StatusCode
			resp.Body.Close()
		}
		t.Warn(err, "unvalidated", "url", url, "status", status)
		return &DiscoveryResult{
			Found:       false,
			URL:         url,
			ContentType: ContentScrape,
			Validated:   false,
			FallbackURL: fallbackURL,
		}, nil
	}
	defer resp.Body.Close()

	contentType := classifyURL(url, resp.Header.Get("Content-Type"))
	t.Done("url", url, "content_type", string(contentType))

	return &DiscoveryResult{
		Found:       true,
		URL:         url,
		ContentType: contentType,
		Validated:   true,
	}, nil
}

// classifyURL determines the content type from URL extension and Content-Type header.
func classifyURL(url, contentTypeHeader string) ContentType {
	lower := strings.ToLower(url)

	// Check for OpenAPI markers
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") {
		if strings.Contains(lower, "openapi") || strings.Contains(lower, "swagger") {
			return ContentOpenAPI
		}
	}

	// Check for single markdown
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") {
		// Check if it's an llms-index (contains links to other pages)
		// This will be refined after fetching the content
		if strings.Contains(lower, "llms.txt") && !strings.Contains(lower, "llms-full") {
			return ContentLLMSIndex
		}
		return ContentSingleMarkdown
	}

	// Check Content-Type header
	if strings.Contains(contentTypeHeader, "text/markdown") || strings.Contains(contentTypeHeader, "text/plain") {
		return ContentSingleMarkdown
	}

	return ContentScrape
}
