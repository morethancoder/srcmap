package docfetcher

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// WebCrawler crawls documentation pages starting from a URL.
type WebCrawler struct {
	Client      *http.Client
	MaxDepth    int
	Concurrency int
	Selector    string // CSS selector (simplified — extracts main content)
}

// NewWebCrawler creates a crawler with sensible defaults.
func NewWebCrawler() *WebCrawler {
	return &WebCrawler{
		Client:      http.DefaultClient,
		MaxDepth:    2,
		Concurrency: 4,
	}
}

// Crawl fetches pages starting from startURL up to the configured depth.
func (c *WebCrawler) Crawl(ctx context.Context, startURL string, depth int) ([]RawPage, error) {
	if depth <= 0 {
		depth = c.MaxDepth
	}

	visited := &sync.Map{}
	var mu sync.Mutex
	var pages []RawPage

	baseURL, err := url.Parse(startURL)
	if err != nil {
		return nil, fmt.Errorf("parsing start URL: %w", err)
	}

	type crawlItem struct {
		url   string
		depth int
	}

	queue := []crawlItem{{url: startURL, depth: 0}}

	for len(queue) > 0 {
		// Collect items at the same depth for parallel fetching
		currentDepth := queue[0].depth
		var batch []crawlItem
		var remaining []crawlItem
		for _, item := range queue {
			if item.depth == currentDepth {
				batch = append(batch, item)
			} else {
				remaining = append(remaining, item)
			}
		}
		queue = remaining

		// Collect discovered URLs from goroutines into a separate slice
		var discovered []crawlItem

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(c.Concurrency)

		for _, item := range batch {
			if _, loaded := visited.LoadOrStore(item.url, true); loaded {
				continue
			}

			g.Go(func() error {
				page, links, err := c.fetchPage(gctx, item.url)
				if err != nil {
					return nil
				}

				mu.Lock()
				pages = append(pages, *page)
				mu.Unlock()

				if item.depth < depth {
					for _, link := range links {
						linkURL, err := url.Parse(link)
						if err != nil {
							continue
						}
						resolved := baseURL.ResolveReference(linkURL)
						if resolved.Host == baseURL.Host {
							mu.Lock()
							discovered = append(discovered, crawlItem{url: resolved.String(), depth: item.depth + 1})
							mu.Unlock()
						}
					}
				}
				return nil
			})
		}

		g.Wait()
		queue = append(queue, discovered...)
	}

	return pages, nil
}

func (c *WebCrawler) fetchPage(ctx context.Context, pageURL string) (*RawPage, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	text := extractText(string(body))
	links := extractLinks(string(body))
	title := extractTitle(string(body))

	h := sha256.Sum256([]byte(text))
	fingerprint := fmt.Sprintf("%x", h)

	return &RawPage{
		URL:         pageURL,
		Title:       title,
		Content:     text,
		Fingerprint: fingerprint,
	}, links, nil
}

// Simple HTML text extraction (strips tags).
func extractText(html string) string {
	// Remove script and style blocks
	for _, tag := range []string{"script", "style"} {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		html = re.ReplaceAllString(html, "")
	}

	// Remove HTML tags
	tagRe := regexp.MustCompile(`<[^>]+>`)
	text := tagRe.ReplaceAllString(html, "\n")

	// Clean up whitespace
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	return strings.Join(cleaned, "\n")
}

func extractLinks(html string) []string {
	linkRe := regexp.MustCompile(`<a[^>]+href="([^"]*)"`)
	matches := linkRe.FindAllStringSubmatch(html, -1)
	var links []string
	for _, m := range matches {
		if len(m) > 1 {
			links = append(links, m[1])
		}
	}
	return links
}

func extractTitle(html string) string {
	titleRe := regexp.MustCompile(`<title>([^<]*)</title>`)
	if m := titleRe.FindStringSubmatch(html); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
