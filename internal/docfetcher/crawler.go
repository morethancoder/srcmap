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
	"sync/atomic"
	"time"

	"github.com/morethancoder/srcmap/internal/httpx"
	"github.com/morethancoder/srcmap/internal/logging"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// DefaultCrawlTimeout caps a full crawl across all pages.
const DefaultCrawlTimeout = 90 * time.Second

// DefaultMaxPages caps the total number of pages fetched in one crawl.
const DefaultMaxPages = 200

// WebCrawler crawls documentation pages starting from a URL.
type WebCrawler struct {
	Client      *http.Client
	MaxDepth    int
	MaxPages    int           // hard cap on pages fetched; 0 → DefaultMaxPages
	Timeout     time.Duration // overall crawl timeout; 0 → DefaultCrawlTimeout
	Concurrency int
	Selector    string // CSS selector (simplified — extracts main content)
}

// NewWebCrawler creates a crawler with sensible defaults.
func NewWebCrawler() *WebCrawler {
	return &WebCrawler{
		Client:      httpx.Default(),
		MaxDepth:    2,
		MaxPages:    DefaultMaxPages,
		Timeout:     DefaultCrawlTimeout,
		Concurrency: 4,
	}
}

// Crawl fetches pages starting from startURL up to the configured depth.
func (c *WebCrawler) Crawl(ctx context.Context, startURL string, depth int) ([]RawPage, error) {
	if depth <= 0 {
		depth = c.MaxDepth
	}
	maxPages := c.MaxPages
	if maxPages <= 0 {
		maxPages = DefaultMaxPages
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultCrawlTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	visited := &sync.Map{}
	var mu sync.Mutex
	pages := make([]RawPage, 0, maxPages)
	var fetched int64

	t := logging.Stage("crawl", "url", startURL, "depth", depth, "max_pages", maxPages, "timeout", timeout)

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
				if atomic.LoadInt64(&fetched) >= int64(maxPages) {
					return nil
				}
				pageStart := time.Now()
				page, links, err := c.fetchPage(gctx, item.url)
				if err != nil {
					log.Warn().Err(err).Str("stage", "crawl.page").Str("url", item.url).Msg("fetch failed")
					return nil
				}
				n := atomic.AddInt64(&fetched, 1)
				if n > int64(maxPages) {
					// Overshoot from parallel gate pass; drop the page.
					return nil
				}
				log.Info().
					Str("stage", "crawl.page").
					Str("url", item.url).
					Int("depth", item.depth).
					Int64("n", n).
					Int("bytes", len(page.Content)).
					Int("links", len(links)).
					Dur("took", time.Since(pageStart)).
					Msg("fetched")

				mu.Lock()
				pages = append(pages, *page)
				mu.Unlock()

				if item.depth < depth && atomic.LoadInt64(&fetched) < int64(maxPages) {
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
		if atomic.LoadInt64(&fetched) >= int64(maxPages) {
			log.Warn().Str("stage", "crawl").Int("max_pages", maxPages).Msg("page cap reached")
			break
		}
		queue = append(queue, discovered...)
	}

	t.Done("url", startURL, "pages", len(pages))

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

// AnchorMarker is a sentinel injected into extracted text to preserve
// the HTML id of a heading across tag-stripping. Chunker splits on this.
const AnchorMarker = "@@SMANCHOR:"

var (
	noiseBlockTags = []string{"script", "style", "noscript", "iframe", "nav", "header", "footer", "aside", "form"}
	mainScopeRe    = regexp.MustCompile(`(?is)<(?:main|article)\b[^>]*>(.*?)</(?:main|article)\s*>`)
	mainRoleRe     = regexp.MustCompile(`(?is)<(\w+)\b[^>]*\brole\s*=\s*"main"[^>]*>(.*?)</\w+\s*>`)
	noiseClassRe   = regexp.MustCompile(`(?is)<(?:div|section|ul|ol)\b[^>]*\b(?:class|id)\s*=\s*"[^"]*\b(?:nav|menu|sidebar|footer|header|breadcrumb|toc|site-nav|global-nav|topbar|cookie)\b[^"]*"[^>]*>.*?</(?:div|section|ul|ol)\s*>`)
	anchorHeadRe   = regexp.MustCompile(`(?is)<h([1-6])\b([^>]*\bid\s*=\s*"([^"]+)"[^>]*)>\s*(.*?)\s*</h[1-6]>`)
	plainHeadRe    = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*>\s*(.*?)\s*</h[1-6]>`)
)

func stripNoiseBlocks(html string) string {
	// Go's regexp has no backrefs and no recursive matching, so nested
	// same-tag noise (e.g. a <nav> inside a sidebar <nav>) needs repeated
	// passes. Iterate until the string stops shrinking, capped at 5.
	for _, tag := range noiseBlockTags {
		re := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>.*?</` + tag + `\s*>`)
		for i := 0; i < 5; i++ {
			next := re.ReplaceAllString(html, "")
			if next == html {
				break
			}
			html = next
		}
	}
	return html
}

// Simple HTML text extraction (strips tags, preserves heading anchors).
func extractText(html string) string {
	// Prefer <main>/<article>/role=main if present.
	if m := mainScopeRe.FindStringSubmatch(html); len(m) > 1 && len(m[1]) > 200 {
		html = m[1]
	} else if m := mainRoleRe.FindStringSubmatch(html); len(m) > 2 && len(m[2]) > 200 {
		html = m[2]
	}

	// Strip navigation/chrome blocks entirely.
	html = stripNoiseBlocks(html)
	// Class-based noise can nest (sidebar > submenu > link-list).
	// Iterate to a fixed point, capped.
	for i := 0; i < 5; i++ {
		next := noiseClassRe.ReplaceAllString(html, "")
		if next == html {
			break
		}
		html = next
	}

	// Preserve anchor IDs on headings so chunker can split on semantic boundaries.
	html = anchorHeadRe.ReplaceAllStringFunc(html, func(s string) string {
		m := anchorHeadRe.FindStringSubmatch(s)
		if len(m) < 5 {
			return s
		}
		level, id, inner := m[1], m[3], stripTagsInline(m[4])
		return fmt.Sprintf("\n\n%s%s@@\nH%s %s\n\n", AnchorMarker, id, level, inner)
	})
	// Non-anchored headings: emit as markdown-style for splitter fallback.
	html = plainHeadRe.ReplaceAllStringFunc(html, func(s string) string {
		m := plainHeadRe.FindStringSubmatch(s)
		if len(m) < 3 {
			return s
		}
		level, inner := m[1], stripTagsInline(m[2])
		return fmt.Sprintf("\n\nH%s %s\n\n", level, inner)
	})

	// Remove remaining HTML tags.
	text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(html, "\n")

	// Clean up whitespace.
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

func stripTagsInline(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, ""))
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
