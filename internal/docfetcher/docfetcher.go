package docfetcher

import "context"

// ContentType classifies how documentation was discovered.
type ContentType string

const (
	ContentSingleMarkdown ContentType = "single-markdown"
	ContentLLMSIndex      ContentType = "llms-index"
	ContentOpenAPI        ContentType = "openapi"
	ContentScrape         ContentType = "scrape"
)

// DiscoveryResult holds the result of LLM-powered doc discovery.
type DiscoveryResult struct {
	Found       bool
	URL         string      // best URL the LLM found, empty if none
	ContentType ContentType // classification of what was found
	Reason      string      // LLM's explanation for why it chose this URL
	Validated   bool        // true = HEAD check confirmed URL is reachable
	FallbackURL string      // homepage URL to scrape if Found is false
}

// RawPage represents a single fetched page of documentation before chunking.
type RawPage struct {
	URL         string
	Title       string
	Content     string // extracted text content
	Fingerprint string // SHA-256 of content
}

// Chunk represents a pre-chunked documentation block ready for LLM processing.
type Chunk struct {
	ID              int64
	SourceID        string
	PageURL         string
	ChunkIndex      int
	Heading         string
	Content         string // text with context header prepended
	EstimatedTokens int
	Fingerprint     string
	Status          ChunkStatus
	Kind            ChunkKind // semantic class: doc|changelog|schema|nav
	AnchorID        string    // HTML id anchor if chunk maps to a single named entity
}

// ChunkKind is the semantic class of a chunk, used to weight/filter search.
type ChunkKind string

const (
	ChunkKindDoc       ChunkKind = "doc"
	ChunkKindChangelog ChunkKind = "changelog"
	ChunkKindSchema    ChunkKind = "schema"
	ChunkKindNav       ChunkKind = "nav"
)

// ChunkStatus tracks the processing state of a chunk.
type ChunkStatus string

const (
	ChunkPending   ChunkStatus = "pending"
	ChunkProcessed ChunkStatus = "processed"
	ChunkFailed    ChunkStatus = "failed"
)

// Crawler fetches documentation pages from a URL.
type Crawler interface {
	Crawl(ctx context.Context, startURL string, depth int) ([]RawPage, error)
}

// Chunker splits raw pages into token-bounded chunks.
type Chunker interface {
	Chunk(sourceName string, pages []RawPage) ([]Chunk, error)
}
