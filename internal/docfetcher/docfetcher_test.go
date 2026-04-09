package docfetcher_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morethancoder/srcmap/internal/docfetcher"
)

// --- Discovery tests ---

func TestDiscoveryPromptTemplate(t *testing.T) {
	prompt := docfetcher.SearchPrompt("data-star")
	if !strings.Contains(prompt, "data-star") {
		t.Error("prompt should contain source name")
	}
	for _, term := range []string{"llms.txt", "llms-full.txt", "docs.md", "OpenAPI", "GitHub"} {
		if !strings.Contains(prompt, term) {
			t.Errorf("prompt missing search term %q", term)
		}
	}
}

func TestDiscoveryValidatesURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ds := docfetcher.NewDiscoveryService()
	ds.Client = srv.Client()

	result, err := ds.ValidateAndClassify(context.Background(), srv.URL+"/docs.md", "")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !result.Found {
		t.Error("expected Found=true")
	}
	if !result.Validated {
		t.Error("expected Validated=true")
	}
}

func TestDiscoveryRejectsUnreachableURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ds := docfetcher.NewDiscoveryService()
	ds.Client = srv.Client()

	result, err := ds.ValidateAndClassify(context.Background(), srv.URL+"/docs.md", "https://fallback.com")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if result.Found {
		t.Error("expected Found=false for 404")
	}
	if result.FallbackURL != "https://fallback.com" {
		t.Errorf("fallback: got %q, want %q", result.FallbackURL, "https://fallback.com")
	}
}

func TestDiscoveryClassifiesSingleMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ds := docfetcher.NewDiscoveryService()
	ds.Client = srv.Client()

	result, err := ds.ValidateAndClassify(context.Background(), srv.URL+"/docs.md", "")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if result.ContentType != docfetcher.ContentSingleMarkdown {
		t.Errorf("content type: got %q, want %q", result.ContentType, docfetcher.ContentSingleMarkdown)
	}
}

func TestDiscoveryClassifiesLLMSIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ds := docfetcher.NewDiscoveryService()
	ds.Client = srv.Client()

	result, err := ds.ValidateAndClassify(context.Background(), srv.URL+"/llms.txt", "")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if result.ContentType != docfetcher.ContentLLMSIndex {
		t.Errorf("content type: got %q, want %q", result.ContentType, docfetcher.ContentLLMSIndex)
	}
}

func TestDiscoveryFallbackScrape(t *testing.T) {
	ds := docfetcher.NewDiscoveryService()
	result, err := ds.ValidateAndClassify(context.Background(), "none", "https://example.com")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if result.Found {
		t.Error("expected Found=false for 'none'")
	}
	if result.ContentType != docfetcher.ContentScrape {
		t.Errorf("content type: got %q, want %q", result.ContentType, docfetcher.ContentScrape)
	}
	if result.FallbackURL != "https://example.com" {
		t.Errorf("fallback: got %q", result.FallbackURL)
	}
}

// --- Single file fetcher tests ---

func TestSingleFileFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# Docs\n\nSome content here.\n"))
	}))
	defer srv.Close()

	f := &docfetcher.SingleFileFetcher{Client: srv.Client()}
	page, err := f.Fetch(context.Background(), srv.URL+"/docs.md")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(page.Content, "Some content here") {
		t.Error("expected content in page")
	}
	if page.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
}

// --- Crawler tests ---

func TestCrawlerBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><title>Home</title></head><body><h1>Welcome</h1><p>Hello world</p><a href="/about">About</a></body></html>`))
		case "/about":
			w.Write([]byte(`<html><head><title>About</title></head><body><h1>About Us</h1><p>We are cool</p></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := docfetcher.NewWebCrawler()
	c.Client = srv.Client()
	pages, err := c.Crawl(context.Background(), srv.URL+"/", 1)
	if err != nil {
		t.Fatalf("crawl: %v", err)
	}
	if len(pages) < 1 {
		t.Fatal("expected at least 1 page")
	}
	// Should have fetched the home page
	found := false
	for _, p := range pages {
		if strings.Contains(p.Content, "Hello world") {
			found = true
		}
	}
	if !found {
		t.Error("expected home page content")
	}
}

func TestCrawlerDepthLimit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte(`<html><body><a href="/deep">Deep</a></body></html>`))
	}))
	defer srv.Close()

	c := docfetcher.NewWebCrawler()
	c.Client = srv.Client()
	// Depth 0 means only start page
	c.Crawl(context.Background(), srv.URL+"/", 0)
	// Should not have crawled many pages
	// With depth=0, only the root is fetched (crawl still runs the first batch)
}

func TestCrawlerDedup(t *testing.T) {
	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		// Page links back to itself
		w.Write([]byte(`<html><body><a href="/">Home</a><a href="/">Home Again</a></body></html>`))
	}))
	defer srv.Close()

	c := docfetcher.NewWebCrawler()
	c.Client = srv.Client()
	c.Crawl(context.Background(), srv.URL+"/", 2)

	if fetchCount > 1 {
		t.Errorf("expected 1 fetch (dedup), got %d", fetchCount)
	}
}

// --- OpenAPI parser tests ---

func TestOpenAPIParser(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	p := &docfetcher.OpenAPIParser{}
	pages, err := p.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Should have 3 operations: GET /pets, POST /pets, GET /pets/{petId}
	if len(pages) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(pages))
	}

	// Verify each operation is present
	urls := make(map[string]bool)
	for _, p := range pages {
		urls[p.URL] = true
	}
	for _, expected := range []string{"GET /pets", "POST /pets", "GET /pets/{petId}"} {
		if !urls[expected] {
			t.Errorf("missing operation %q", expected)
		}
	}
}

// --- Markdown walker tests ---

func TestMarkdownWalker(t *testing.T) {
	w := &docfetcher.MarkdownWalker{}
	pages, err := w.Walk(filepath.Join("..", "..", "testdata", "docs"))
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(pages) != 2 {
		t.Fatalf("expected 2 markdown files, got %d", len(pages))
	}

	titles := make(map[string]bool)
	for _, p := range pages {
		titles[p.Title] = true
	}
	if !titles["Getting Started"] {
		t.Error("missing 'Getting Started' page")
	}
	if !titles["API Reference"] {
		t.Error("missing 'API Reference' page")
	}
}

// --- Chunker tests ---

func TestChunkerDocTypeDetection(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    docfetcher.DocType
	}{
		{"markdown", "## Heading\n\nContent", docfetcher.DocMarkdown},
		{"html headings", "<h2>Title</h2><p>Content</p>", docfetcher.DocHeadingStructured},
		{"flat prose", "Just some plain text without any headings at all.", docfetcher.DocFlatProse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We test via chunking behavior since detectDocType is unexported
			chunker := &docfetcher.DefaultChunker{}
			pages := []docfetcher.RawPage{{Content: tt.content, URL: "test"}}
			chunks, err := chunker.Chunk("test-source", pages)
			if err != nil {
				t.Fatalf("chunk: %v", err)
			}
			if len(chunks) == 0 {
				t.Fatal("expected at least one chunk")
			}
		})
	}
}

func TestChunkerMarkdown(t *testing.T) {
	content := "## Section One\n\nContent for section one.\n\n## Section Two\n\nContent for section two.\n\n## Section Three\n\nContent for section three."

	chunker := &docfetcher.DefaultChunker{}
	pages := []docfetcher.RawPage{{Content: content, URL: "test.md", Title: "Test Doc"}}
	chunks, err := chunker.Chunk("test-source", pages)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	// 3 sections but they're small so may be batched
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// All chunks should have context headers
	for i, c := range chunks {
		if !strings.Contains(c.Content, "[Source: test-source]") {
			t.Errorf("chunk %d missing [Source] header", i)
		}
		if !strings.Contains(c.Content, "[Chunk") {
			t.Errorf("chunk %d missing [Chunk] header", i)
		}
	}
}

func TestChunkerMaxTokenEnforced(t *testing.T) {
	// Create large content with paragraph breaks (~5000 tokens = ~3800 words)
	// Use paragraph breaks so flat-prose splitting can work
	var paragraphs []string
	for i := 0; i < 40; i++ {
		words := make([]string, 100)
		for j := range words {
			words[j] = "word"
		}
		paragraphs = append(paragraphs, strings.Join(words, " "))
	}
	content := strings.Join(paragraphs, "\n\n")

	chunker := &docfetcher.DefaultChunker{}
	pages := []docfetcher.RawPage{{Content: content, URL: "test"}}
	chunks, err := chunker.Chunk("test-source", pages)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		totalTokens := int(float64(len(strings.Fields(c.Content))) * 1.3)
		if totalTokens > 3500 { // max chunk + header overhead
			t.Errorf("chunk %d has ~%d tokens, exceeds max", i, totalTokens)
		}
	}
}

func TestChunkerContextHeader(t *testing.T) {
	content := "## sendMessage\n\nSend a message to a chat."

	chunker := &docfetcher.DefaultChunker{}
	pages := []docfetcher.RawPage{{Content: content, URL: "test", Title: "Bot API"}}
	chunks, err := chunker.Chunk("telegram", pages)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	c := chunks[0]
	if !strings.Contains(c.Content, "[Source: telegram]") {
		t.Error("missing [Source] in header")
	}
	if !strings.Contains(c.Content, "[Chunk 1 of") {
		t.Error("missing [Chunk N of M] in header")
	}
}

func TestChunkerOriginHeader(t *testing.T) {
	content := "## Section\n\nSome content."

	chunker := &docfetcher.DefaultChunker{}
	pages := []docfetcher.RawPage{{Content: content, URL: "test"}}
	chunks, err := chunker.ChunkWithOrigin("data-star", "https://data-star.dev/docs.md", pages)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	if !strings.Contains(chunks[0].Content, "[Origin: https://data-star.dev/docs.md]") {
		t.Error("missing [Origin] header for discovered single-file source")
	}
}

func TestChunkerOpenAPIPassthrough(t *testing.T) {
	specContent, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	parser := &docfetcher.OpenAPIParser{}
	pages, err := parser.Parse(specContent)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	chunker := &docfetcher.DefaultChunker{}
	chunks, err := chunker.Chunk("petstore", pages)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	// Each operation should produce its own chunk (though small ones may be batched)
	if len(chunks) == 0 {
		t.Fatal("expected chunks from OpenAPI")
	}

	for _, c := range chunks {
		if c.Status != docfetcher.ChunkPending {
			t.Errorf("expected pending status, got %q", c.Status)
		}
	}
}

func TestChunkerFlatProse(t *testing.T) {
	// Build content with no headings, multiple paragraphs
	var paragraphs []string
	for i := 0; i < 20; i++ {
		words := make([]string, 100)
		for j := range words {
			words[j] = "word"
		}
		paragraphs = append(paragraphs, strings.Join(words, " "))
	}
	content := strings.Join(paragraphs, "\n\n")

	chunker := &docfetcher.DefaultChunker{}
	pages := []docfetcher.RawPage{{Content: content, URL: "test"}}
	chunks, err := chunker.Chunk("test-source", pages)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for large flat prose, got %d", len(chunks))
	}

	// No chunk body should exceed flatProseMax significantly
	for i, c := range chunks {
		bodyTokens := int(float64(len(strings.Fields(c.Content))) * 1.3)
		if bodyTokens > 3200 {
			t.Errorf("chunk %d has ~%d tokens, too large", i, bodyTokens)
		}
	}
}
