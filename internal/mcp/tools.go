package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/morethancoder/srcmap/internal/docfetcher"
	"github.com/morethancoder/srcmap/internal/fetcher"
	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/internal/parser"
	"github.com/morethancoder/srcmap/pkg/fileformat"
)

// Verbose controls stderr logging of tool calls. Enabled by default.
// MCP stdio protocol uses stdout for JSON-RPC; logs MUST go to stderr.
var Verbose = true

// ToolHandler handles MCP tool calls backed by a real database and file system.
type ToolHandler struct {
	DB        *index.DB
	SrcmapDir string // .srcmap/ directory path

	// Optional: set these to enable srcmap_fetch and srcmap_docs_add tools.
	Orchestrator   *fetcher.Orchestrator
	ParserRegistry *parser.Registry
}

// NewToolHandler creates a tool handler.
func NewToolHandler(db *index.DB, srcmapDir string) *ToolHandler {
	return &ToolHandler{DB: db, SrcmapDir: srcmapDir}
}

// AllTools returns definitions for all MCP tools.
func (h *ToolHandler) AllTools() []Tool {
	tools := []Tool{
		{
			Name:        "srcmap_lookup",
			Description: "Look up a specific code symbol by source and name. Returns file path, line range, parameters, and return type.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID (e.g. 'zod')"},
					"symbol": map[string]string{"type": "string", "description": "Symbol name (e.g. 'ZodString.min')"},
				},
				"required": []string{"source", "symbol"},
			},
		},
		{
			Name:        "srcmap_search_code",
			Description: "Search code symbols by name pattern within a source.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
					"query":  map[string]string{"type": "string", "description": "Search query (name pattern)"},
				},
				"required": []string{"source", "query"},
			},
		},
		{
			Name:        "srcmap_doc_map",
			Description: "Return the root index.md for a source, showing all sections and their descriptions.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
				},
				"required": []string{"source"},
			},
		},
		{
			Name:        "srcmap_doc_section",
			Description: "Return a section.md showing all methods and their one-line summaries.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source":  map[string]string{"type": "string", "description": "Source ID"},
					"section": map[string]string{"type": "string", "description": "Section name"},
				},
				"required": []string{"source", "section"},
			},
		},
		{
			Name:        "srcmap_doc_lookup",
			Description: "Return the full content of a method.md doc file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
					"method": map[string]string{"type": "string", "description": "Method name"},
				},
				"required": []string{"source", "method"},
			},
		},
		{
			Name:        "srcmap_doc_concept",
			Description: "Return the full content of a concept.md doc file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source":  map[string]string{"type": "string", "description": "Source ID"},
					"concept": map[string]string{"type": "string", "description": "Concept name"},
				},
				"required": []string{"source", "concept"},
			},
		},
		{
			Name:        "srcmap_doc_search",
			Description: "Fuzzy search across all doc files for a source.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
					"query":  map[string]string{"type": "string", "description": "Search query"},
				},
				"required": []string{"source", "query"},
			},
		},
		{
			Name:        "srcmap_doc_gotchas",
			Description: "Return relevant gotchas for a source, optionally filtered by method.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
					"method": map[string]string{"type": "string", "description": "Optional method name to filter gotchas"},
				},
				"required": []string{"source"},
			},
		},
		{
			Name:        "srcmap_source_info",
			Description: "Return source metadata: name, version, last_updated, triggers, stats.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
				},
				"required": []string{"source"},
			},
		},
		{
			Name:        "srcmap_process_chunk",
			Description: "Process one pre-chunked text block. Fetches the chunk from the database, auto-classifies it, and writes the doc file. No content input needed — the tool handles everything.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"chunk_id": map[string]string{"type": "number", "description": "Chunk ID from the database"},
				},
				"required": []string{"chunk_id"},
			},
		},
		{
			Name:        "srcmap_process_all",
			Description: "Process ALL pending doc chunks for a source in one call. Auto-classifies each chunk, writes doc files, builds index and section files, and updates source counts. Use this after srcmap_docs_add.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
				},
				"required": []string{"source"},
			},
		},
		{
			Name:        "srcmap_process_status",
			Description: "Return pending/processed/failed chunk counts for a source.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID"},
				},
				"required": []string{"source"},
			},
		},
		{
			Name:        "srcmap_list_sources",
			Description: "List all indexed sources with their version, symbol count, doc stats, and scope (local or global).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"global_only": map[string]interface{}{"type": "boolean", "description": "If true, only list global sources. Default: false (list all)."},
				},
			},
		},
	}

	// Add fetch/docs-add tools only when orchestrator is configured (agent mode).
	if h.Orchestrator != nil {
		tools = append(tools,
			Tool{
				Name:        "srcmap_fetch",
				Description: "Fetch and index a package's source code. Supports npm packages, pypi:package, Go modules (github.com/...), and GitHub repos (owner/repo). After fetching, symbols are parsed and indexed automatically.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"packages": map[string]interface{}{
							"type":        "array",
							"items":       map[string]string{"type": "string"},
							"description": "Package names to fetch (e.g. ['zod', 'pypi:flask', 'github.com/gin-gonic/gin'])",
						},
					},
					"required": []string{"packages"},
				},
			},
			Tool{
				Name:        "srcmap_docs_add",
				Description: "Add documentation for a source by fetching from a URL. Automatically crawls the URL, chunks the content, classifies each chunk, writes structured doc files, and builds the index. Everything happens in one call — no manual processing needed.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"source": map[string]string{"type": "string", "description": "Source name (must match an existing source or will be created)"},
						"url":    map[string]string{"type": "string", "description": "URL to fetch documentation from (markdown, HTML, or raw text)"},
					},
					"required": []string{"source", "url"},
				},
			},
			Tool{
				Name:        "srcmap_ingest_local_docs",
				Description: "Offline fallback — ingest docs from the cloned source's own README and docs/ folder. Use only when no web docs URL is available. Source must already be fetched.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"source": map[string]string{"type": "string", "description": "Source ID (must be already fetched)"},
					},
					"required": []string{"source"},
				},
			},
		)
	}

	return tools
}

// CallTool dispatches a tool call by name.
func (h *ToolHandler) CallTool(ctx context.Context, name string, args map[string]interface{}) (res *ToolResult, err error) {
	if Verbose {
		logToolCall(name, args)
		start := time.Now()
		defer func() {
			logToolResult(name, res, time.Since(start))
		}()
	}
	return h.dispatchTool(ctx, name, args)
}

func (h *ToolHandler) dispatchTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error) {
	switch name {
	case "srcmap_lookup":
		return h.handleLookup(args)
	case "srcmap_search_code":
		return h.handleSearchCode(args)
	case "srcmap_doc_map":
		return h.handleDocMap(args)
	case "srcmap_doc_section":
		return h.handleDocSection(args)
	case "srcmap_doc_lookup":
		return h.handleDocLookup(args)
	case "srcmap_doc_concept":
		return h.handleDocConcept(args)
	case "srcmap_doc_search":
		return h.handleDocSearch(args)
	case "srcmap_doc_gotchas":
		return h.handleDocGotchas(args)
	case "srcmap_source_info":
		return h.handleSourceInfo(args)
	case "srcmap_process_chunk":
		return h.handleProcessChunk(args)
	case "srcmap_process_all":
		return h.handleProcessAll(args)
	case "srcmap_process_status":
		return h.handleProcessStatus(args)
	case "srcmap_fetch":
		return h.handleFetch(ctx, args)
	case "srcmap_docs_add":
		return h.handleDocsAdd(ctx, args)
	case "srcmap_ingest_local_docs":
		return h.handleIngestLocalDocs(ctx, args)
	case "srcmap_list_sources":
		return h.handleListSources(args)
	default:
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", name)}},
			IsError: true,
		}, nil
	}
}

func (h *ToolHandler) handleLookup(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	symbol, _ := args["symbol"].(string)

	sym, err := h.DB.LookupSymbol(source, symbol)
	if err != nil {
		return textError(err.Error()), nil
	}

	text := fmt.Sprintf("%s (%s)\nFile: %s:%d-%d", sym.Name, sym.Kind, sym.FilePath, sym.StartLine, sym.EndLine)
	if sym.Parameters != "" {
		text += fmt.Sprintf("\nParams: %s", sym.Parameters)
	}
	if sym.ReturnType != "" {
		text += fmt.Sprintf("\nReturns: %s", sym.ReturnType)
	}
	return textResult(text), nil
}

func (h *ToolHandler) handleSearchCode(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	query, _ := args["query"].(string)

	symbols, err := h.DB.SearchSymbols(source, query)
	if err != nil {
		return textError(err.Error()), nil
	}

	var lines []string
	for _, s := range symbols {
		lines = append(lines, fmt.Sprintf("%s (%s) — %s:%d-%d", s.Name, s.Kind, s.FilePath, s.StartLine, s.EndLine))
	}
	if len(lines) == 0 {
		return textResult("No symbols found."), nil
	}
	return textResult(strings.Join(lines, "\n")), nil
}

func (h *ToolHandler) handleDocMap(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	path := filepath.Join(h.SrcmapDir, "docs", source, "index.md")
	if _, err := os.Stat(path); err != nil {
		h.rebuildIndexFiles(source)
	}
	return readFileResult(path)
}

func (h *ToolHandler) handleDocSection(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	section, _ := args["section"].(string)
	path := filepath.Join(h.SrcmapDir, "docs", source, section, "section.md")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(h.SrcmapDir, "docs", source, sanitizeSection(section), "section.md")
	}
	if _, err := os.Stat(path); err != nil {
		h.rebuildIndexFiles(source)
	}
	return readFileResult(path)
}

// rebuildIndexFiles regenerates index.md, per-section section.md, and the
// gotchas stub from whatever method/concept files already exist on disk.
// Used to repair sources processed before the writers existed.
func (h *ToolHandler) rebuildIndexFiles(source string) {
	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return
	}
	hb := fileformat.NewHierarchyBuilder(h.SrcmapDir, source)
	hb.EnsureStructure()

	var summaries []fileformat.SectionSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		summaries = append(summaries, fileformat.SectionSummary{
			Name:        e.Name(),
			Description: fmt.Sprintf("Documentation for %s", e.Name()),
		})
	}
	if len(summaries) > 0 {
		hb.WriteIndex(summaries)
	}
	h.writeSectionFiles(source, hb)
	h.ensureGotchasFile(source)
}

func (h *ToolHandler) handleDocLookup(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	method, _ := args["method"].(string)
	// Search for the method file across sections
	return h.findDocFile(source, "methods", method)
}

func (h *ToolHandler) handleDocConcept(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	concept, _ := args["concept"].(string)
	return h.findDocFile(source, "concepts", concept)
}

func (h *ToolHandler) handleDocSearch(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	query, _ := args["query"].(string)
	query = strings.ToLower(query)

	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	var matches []string

	filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(string(content)), query) {
			rel, _ := filepath.Rel(docsDir, path)
			matches = append(matches, rel)
		}
		return nil
	})

	if len(matches) == 0 {
		return textResult("No matching doc files found."), nil
	}
	return textResult(strings.Join(matches, "\n")), nil
}

func (h *ToolHandler) handleDocGotchas(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	path := filepath.Join(h.SrcmapDir, "docs", source, "gotchas.md")
	if _, err := os.Stat(path); err != nil {
		h.ensureGotchasFile(source)
	}
	return readFileResult(path)
}

func (h *ToolHandler) handleSourceInfo(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	rec, err := h.DB.GetSource(source)
	if err != nil {
		return textError(err.Error()), nil
	}

	// Also get chunk counts
	pending, processed, failed, _ := h.DB.ChunkCounts(source)

	var lines []string
	lines = append(lines, fmt.Sprintf("Source: %s", rec.Name))
	if rec.Version != "" {
		lines = append(lines, fmt.Sprintf("Version: %s", rec.Version))
	}
	lines = append(lines, fmt.Sprintf("Last Updated: %s", rec.LastUpdated))
	lines = append(lines, fmt.Sprintf("Symbols: %d", rec.MethodCount))
	lines = append(lines, fmt.Sprintf("Sections: %d | Concepts: %d | Gotchas: %d", rec.SectionCount, rec.ConceptCount, rec.GotchaCount))
	if pending+processed+failed > 0 {
		lines = append(lines, fmt.Sprintf("Doc Chunks: %d processed, %d pending, %d failed", processed, pending, failed))
	}

	// Check if doc files exist on disk
	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	if _, err := os.Stat(filepath.Join(docsDir, "index.md")); err == nil {
		lines = append(lines, fmt.Sprintf("Docs directory: %s (index.md present)", docsDir))
	} else {
		lines = append(lines, fmt.Sprintf("Docs directory: %s (no index.md yet)", docsDir))
	}

	return textResult(strings.Join(lines, "\n")), nil
}

func (h *ToolHandler) handleProcessChunk(args map[string]interface{}) (*ToolResult, error) {
	chunkIDFloat, ok := args["chunk_id"].(float64)
	if !ok {
		return textError("chunk_id is required and must be a number"), nil
	}
	chunkID := int64(chunkIDFloat)

	chunk, err := h.DB.GetChunk(chunkID)
	if err != nil {
		return textError(fmt.Sprintf("chunk %d not found: %v", chunkID, err)), nil
	}

	classification := classifyChunk(chunk)
	section := extractSection(chunk)
	docID := buildDocID(chunk)

	result, err := h.processOneChunk(chunk, classification, section, docID)
	if err != nil {
		return textError(err.Error()), nil
	}
	return textResult(result), nil
}

func (h *ToolHandler) handleProcessAll(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	if source == "" {
		return textError("source parameter is required"), nil
	}

	chunks, err := h.DB.GetPendingChunks(source)
	if err != nil {
		return textError(fmt.Sprintf("failed to get pending chunks: %v", err)), nil
	}
	if len(chunks) == 0 {
		return textResult(fmt.Sprintf("No pending chunks for %s. Nothing to process.", source)), nil
	}

	var processed, failed, ignored int
	sections := make(map[string]bool)
	concepts := 0
	methods := 0
	gotchas := 0

	for i := range chunks {
		chunk := &chunks[i]
		classification := classifyChunk(chunk)
		section := extractSection(chunk)
		docID := buildDocID(chunk)

		if classification == "ignore" {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkProcessed)
			ignored++
			continue
		}

		_, err := h.processOneChunk(chunk, classification, section, docID)
		if err != nil {
			failed++
			continue
		}
		processed++
		sections[section] = true

		switch classification {
		case "method":
			methods++
		case "concept":
			concepts++
		case "gotcha":
			gotchas++
		}
	}

	// Build index.md + per-section section.md + gotchas stub so every
	// MCP doc tool has a file to read.
	hb := fileformat.NewHierarchyBuilder(h.SrcmapDir, source)
	hb.EnsureStructure()

	var sectionSummaries []fileformat.SectionSummary
	for sec := range sections {
		sectionSummaries = append(sectionSummaries, fileformat.SectionSummary{
			Name:        sec,
			Description: fmt.Sprintf("Documentation for %s", sec),
		})
	}
	if len(sectionSummaries) > 0 {
		hb.WriteIndex(sectionSummaries)
	}

	h.writeSectionFiles(source, hb)
	h.ensureGotchasFile(source)

	// Update source counts in DB
	h.DB.UpdateSourceCounts(source, len(sections), concepts, gotchas)

	var output []string
	output = append(output, fmt.Sprintf("✓ Processed %d chunks for %s", processed, source))
	if ignored > 0 {
		output = append(output, fmt.Sprintf("  Ignored: %d (too short or boilerplate)", ignored))
	}
	if failed > 0 {
		output = append(output, fmt.Sprintf("  Failed: %d", failed))
	}
	output = append(output, fmt.Sprintf("  Sections: %d", len(sections)))
	output = append(output, fmt.Sprintf("  Methods: %d | Concepts: %d | Gotchas: %d", methods, concepts, gotchas))
	output = append(output, fmt.Sprintf("\nDoc files written to .srcmap/docs/%s/", source))
	output = append(output, "Use srcmap_doc_map, srcmap_doc_section, srcmap_doc_lookup, srcmap_doc_concept to query.")

	return textResult(strings.Join(output, "\n")), nil
}

// processOneChunk writes a single chunk as a doc file and updates its status.
func (h *ToolHandler) processOneChunk(chunk *docfetcher.Chunk, classification, section, docID string) (string, error) {
	if classification == "ignore" {
		if err := h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkProcessed); err != nil {
			return "", err
		}
		return fmt.Sprintf("Chunk %d ignored.", chunk.ID), nil
	}

	// Strip the context header from content for the doc body
	body := stripContextHeader(chunk.Content)

	fm := fileformat.Frontmatter{
		ID:            docID,
		Section:       section,
		AutoGenerated: true,
		LastUpdated:   fileformat.Now(),
		Fingerprint:   chunk.Fingerprint,
	}

	hb := fileformat.NewHierarchyBuilder(h.SrcmapDir, chunk.SourceID)
	hb.EnsureStructure()

	switch classification {
	case "method":
		fm.Kind = fileformat.KindMethod
		fm.Symbol = chunk.Heading
		if err := hb.WriteMethod(section, &fileformat.DocFile{Frontmatter: fm, Body: "\n" + body + "\n"}); err != nil {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkFailed)
			return "", fmt.Errorf("writing method doc: %w", err)
		}
	case "concept":
		fm.Kind = fileformat.KindConcept
		if err := hb.WriteConcept(section, &fileformat.DocFile{Frontmatter: fm, Body: "\n" + body + "\n"}); err != nil {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkFailed)
			return "", fmt.Errorf("writing concept doc: %w", err)
		}
	case "gotcha":
		if err := hb.AppendGotcha(docID, body); err != nil {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkFailed)
			return "", fmt.Errorf("writing gotcha: %w", err)
		}
	}

	if err := h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkProcessed); err != nil {
		return "", err
	}

	return fmt.Sprintf("Chunk %d → %s/%s/%s.md", chunk.ID, chunk.SourceID, section, classification), nil
}

// classifyChunk uses heuristics to classify a chunk as method, concept, or gotcha.
func classifyChunk(c *docfetcher.Chunk) string {
	content := strings.ToLower(c.Content)
	heading := strings.ToLower(c.Heading)

	// Too short = ignore (nav fragments, footers, etc.)
	if c.EstimatedTokens < 30 {
		return "ignore"
	}

	// Gotcha patterns
	gotchaPatterns := []string{"gotcha", "common mistake", "pitfall", "breaking change",
		"caution", "warning:", "deprecated", "known issue", "watch out", "be careful"}
	for _, p := range gotchaPatterns {
		if strings.Contains(heading, p) || strings.Contains(content[:min(500, len(content))], p) {
			return "gotcha"
		}
	}

	// Method/API patterns — functions, directives, properties with signatures
	methodPatterns := regexp.MustCompile(`(?i)(^|\s)(x-[\w-]+|v-[\w-]+|\w+\.\w+\(|function\s+\w+|def\s+\w+|fn\s+\w+|const\s+\w+\s*=|api\s+reference|method|directive|property|attribute|parameter|endpoint|route)`)
	if methodPatterns.MatchString(heading) || (len(heading) > 0 && methodPatterns.MatchString(c.Content[:min(300, len(c.Content))])) {
		return "method"
	}

	// Default to concept
	return "concept"
}

// extractSection extracts a section name from the chunk's context header or heading.
func extractSection(c *docfetcher.Chunk) string {
	// Try to extract [Section: ...] from context header
	if idx := strings.Index(c.Content, "[Section: "); idx >= 0 {
		end := strings.Index(c.Content[idx:], "]")
		if end > 0 {
			section := c.Content[idx+10 : idx+end]
			section = strings.TrimSpace(section)
			if section != "" {
				return sanitizeSection(section)
			}
		}
	}

	// Fall back to page URL path
	if c.PageURL != "" {
		parts := strings.Split(strings.TrimRight(c.PageURL, "/"), "/")
		if len(parts) >= 2 {
			section := parts[len(parts)-1]
			if section != "" && section != "docs" && section != "index" {
				return sanitizeSection(section)
			}
			if len(parts) >= 3 {
				return sanitizeSection(parts[len(parts)-2])
			}
		}
	}

	return "general"
}

// buildDocID creates a unique doc ID from the chunk.
func buildDocID(c *docfetcher.Chunk) string {
	if c.Heading != "" {
		return sanitizeSection(c.Heading)
	}
	return fmt.Sprintf("chunk-%d", c.ID)
}

// stripContextHeader removes the [Source: ...] header lines from chunk content.
func stripContextHeader(content string) string {
	lines := strings.SplitN(content, "\n", -1)
	// Skip lines that start with [ and are context headers
	start := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			start = i + 1
			continue
		}
		if trimmed == "" && start == i {
			start = i + 1
			continue
		}
		break
	}
	if start >= len(lines) {
		return content
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n"))
}

// sanitizeSection converts a string to a safe directory/file name.
func sanitizeSection(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		if r == ' ' || r == '/' || r == '.' {
			return '-'
		}
		return -1
	}, s)
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// writeSectionFiles scans each section dir under .srcmap/docs/{source}/ and
// writes a section.md listing every method + concept found. Run after
// processOneChunk has populated the method/concept files.
func (h *ToolHandler) writeSectionFiles(source string, hb *fileformat.HierarchyBuilder) {
	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sectionName := e.Name()
		sectionDir := filepath.Join(docsDir, sectionName)

		var summaries []fileformat.MethodSummary

		for _, kind := range []string{"methods", "concepts"} {
			kdir := filepath.Join(sectionDir, kind)
			files, err := os.ReadDir(kdir)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() || filepath.Ext(f.Name()) != ".md" {
					continue
				}
				name := strings.TrimSuffix(f.Name(), ".md")
				summaries = append(summaries, fileformat.MethodSummary{
					Name:    name,
					Summary: kind[:len(kind)-1], // "method" or "concept"
				})
			}
		}

		if len(summaries) == 0 {
			continue
		}
		_ = hb.WriteSection(sectionName, summaries)
	}
}

// ensureGotchasFile writes an empty gotchas.md stub if none exists so
// srcmap_doc_gotchas always returns readable content.
func (h *ToolHandler) ensureGotchasFile(source string) {
	path := filepath.Join(h.SrcmapDir, "docs", source, "gotchas.md")
	if _, err := os.Stat(path); err == nil {
		return
	}
	_ = os.WriteFile(path, []byte("# Gotchas\n\n_No gotchas recorded for this source yet._\n"), 0o644)
}

func (h *ToolHandler) handleProcessStatus(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	pending, processed, failed, err := h.DB.ChunkCounts(source)
	if err != nil {
		return textError(err.Error()), nil
	}
	return textResult(fmt.Sprintf("Pending: %d\nProcessed: %d\nFailed: %d", pending, processed, failed)), nil
}

func (h *ToolHandler) findDocFile(source, kind, name string) (*ToolResult, error) {
	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	wanted := sanitizeSection(name) // match the same transform used when writing
	var found string

	filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		dir := filepath.Base(filepath.Dir(path))
		base := strings.TrimSuffix(filepath.Base(path), ".md")
		if dir == kind && (strings.EqualFold(base, name) || strings.EqualFold(base, wanted)) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})

	if found == "" {
		return textError(fmt.Sprintf("%s %q not found in source %q", kind, name, source)), nil
	}
	return readFileResult(found)
}

func readFileResult(path string) (*ToolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return textError(fmt.Sprintf("file not found: %s", filepath.Base(path))), nil
	}
	return textResult(string(data)), nil
}

func (h *ToolHandler) handleFetch(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if h.Orchestrator == nil {
		return textError("fetch not available: orchestrator not configured"), nil
	}

	packagesRaw, ok := args["packages"]
	if !ok {
		return textError("packages parameter is required"), nil
	}

	// Parse the packages array (comes as []interface{} from JSON)
	var packageNames []string
	switch v := packagesRaw.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				packageNames = append(packageNames, s)
			}
		}
	case string:
		packageNames = []string{v}
	}

	if len(packageNames) == 0 {
		return textError("at least one package name is required"), nil
	}

	var requests []fetcher.FetchRequest
	for _, name := range packageNames {
		requests = append(requests, fetcher.ParsePackageName(name, false))
	}

	results := h.Orchestrator.FetchAll(ctx, requests)

	reg := h.ParserRegistry
	if reg == nil {
		reg = parser.NewRegistry()
	}

	var output []string
	for _, r := range results {
		if r.Err != nil {
			output = append(output, fmt.Sprintf("✗ %s: %v", r.Request.Name, r.Err))
			continue
		}

		now := time.Now().UTC().Format(time.RFC3339)
		err := h.DB.InsertSource(&index.SourceRecord{
			ID:          r.Source.Name,
			Name:        r.Source.Name,
			Version:     r.Source.Version,
			RepoURL:     r.Source.RepoURL,
			Path:        r.Source.Path,
			Global:      r.Source.Global,
			LastUpdated: now,
		})
		if err != nil {
			output = append(output, fmt.Sprintf("✗ %s: failed to register: %v", r.Source.Name, err))
			continue
		}

		symbols, err := reg.ParseDirectory(r.Source.Path)
		if err != nil {
			output = append(output, fmt.Sprintf("✓ %s@%s fetched (parse warning: %v)", r.Source.Name, r.Source.Version, err))
			continue
		}

		indexed := 0
		for i := range symbols {
			symbols[i].SourceID = r.Source.Name
			if _, err := h.DB.InsertSymbol(&symbols[i]); err == nil {
				indexed++
			}
		}
		output = append(output, fmt.Sprintf("✓ %s@%s fetched and indexed %d symbols", r.Source.Name, r.Source.Version, indexed))

		// Doc ingestion directive for the calling LLM agent.
		// The agent should research the best docs URL (web search, llms.txt,
		// official docs) and then call srcmap_docs_add to fetch+map them to
		// this source. If the agent has no web access, it may fall back to
		// calling srcmap_ingest_local_docs for README-based ingestion.
		output = append(output, "")
		output = append(output, fmt.Sprintf("▸ NEXT STEP — discover docs for %q:", r.Source.Name))
		output = append(output, "  "+docfetcher.SearchPrompt(r.Source.Name))
		output = append(output, fmt.Sprintf("  Then call: srcmap_docs_add(source=%q, url=<discovered_url>)", r.Source.Name))
		output = append(output, fmt.Sprintf("  Offline fallback: srcmap_ingest_local_docs(source=%q)", r.Source.Name))
	}

	return textResult(strings.Join(output, "\n")), nil
}

// handleIngestLocalDocs is the offline fallback — walks the cloned source
// for README / docs folders and ingests markdown locally.
func (h *ToolHandler) handleIngestLocalDocs(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	sourceName, _ := args["source"].(string)
	if sourceName == "" {
		return textError("source parameter is required"), nil
	}
	rec, err := h.DB.GetSource(sourceName)
	if err != nil || rec == nil {
		return textError(fmt.Sprintf("source %q not found — run srcmap_fetch first", sourceName)), nil
	}
	if rec.Path == "" {
		return textError(fmt.Sprintf("source %q has no local path", sourceName)), nil
	}
	summary, err := h.AutoIngestLocalDocs(ctx, sourceName, rec.Path)
	if err != nil {
		return textError(err.Error()), nil
	}
	return textResult(summary), nil
}

func (h *ToolHandler) handleDocsAdd(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	sourceName, _ := args["source"].(string)
	rawURL, _ := args["url"].(string)

	if sourceName == "" {
		return textError("source parameter is required"), nil
	}
	if rawURL == "" {
		return textError("url parameter is required"), nil
	}

	// Register source if not exists
	h.DB.InsertSource(&index.SourceRecord{
		ID:          sourceName,
		Name:        sourceName,
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
	})

	var output []string

	// Step 1: Check if URL is LLM-friendly (markdown, llms.txt, openapi)
	disc := docfetcher.NewDiscoveryService()
	result, err := disc.ValidateAndClassify(ctx, rawURL, "")
	if err != nil {
		return textError(fmt.Sprintf("failed to validate URL: %v", err)), nil
	}

	var pages []docfetcher.RawPage
	var contentType string

	switch {
	case result.Found && (result.ContentType == docfetcher.ContentSingleMarkdown || result.ContentType == docfetcher.ContentLLMSIndex):
		// LLM-friendly: fetch single file
		f := &docfetcher.SingleFileFetcher{}
		page, err := f.Fetch(ctx, rawURL)
		if err != nil {
			return textError(fmt.Sprintf("failed to fetch: %v", err)), nil
		}
		pages = []docfetcher.RawPage{*page}
		contentType = string(result.ContentType)
		output = append(output, fmt.Sprintf("✓ Fetched %d bytes (type: %s)", len(page.Content), result.ContentType))

	case result.Found && result.ContentType == docfetcher.ContentOpenAPI:
		// OpenAPI spec — fetch and parse
		f := &docfetcher.SingleFileFetcher{}
		page, err := f.Fetch(ctx, rawURL)
		if err != nil {
			return textError(fmt.Sprintf("failed to fetch: %v", err)), nil
		}
		p := &docfetcher.OpenAPIParser{}
		parsed, err := p.Parse([]byte(page.Content))
		if err != nil {
			return textError(fmt.Sprintf("failed to parse OpenAPI: %v", err)), nil
		}
		pages = parsed
		contentType = "openapi"
		output = append(output, fmt.Sprintf("✓ Parsed %d operations from OpenAPI spec", len(parsed)))

	default:
		// HTML or unknown — crawl the page and follow sub-URLs
		crawler := docfetcher.NewWebCrawler()
		crawled, err := crawler.Crawl(ctx, rawURL, 2)
		if err != nil {
			return textError(fmt.Sprintf("failed to crawl: %v", err)), nil
		}
		pages = crawled
		contentType = "scrape"
		output = append(output, fmt.Sprintf("✓ Crawled %d pages from %s", len(crawled), rawURL))
	}

	if len(pages) == 0 {
		return textError("no content fetched"), nil
	}

	// Chunk the content
	chunker := &docfetcher.DefaultChunker{}
	chunks, err := chunker.ChunkWithOrigin(sourceName, rawURL, pages)
	if err != nil {
		return textError(fmt.Sprintf("failed to chunk: %v", err)), nil
	}

	stored := 0
	for i := range chunks {
		id, err := h.DB.InsertChunk(&chunks[i])
		if err != nil {
			continue
		}
		chunks[i].ID = id
		stored++
	}

	// Write source.yaml
	sy := &fileformat.SourceYAML{
		Name: sourceName,
		DocOrigin: &fileformat.DocOrigin{
			URL:          rawURL,
			ContentType:  contentType,
			DiscoveredAt: time.Now().UTC().Format(time.RFC3339),
			Validated:    result.Found,
		},
	}
	hb := fileformat.NewHierarchyBuilder(h.SrcmapDir, sourceName)
	hb.EnsureStructure()
	fileformat.WriteSourceYAML(filepath.Join(h.SrcmapDir, "docs", sourceName, "source.yaml"), sy)

	output = append(output, fmt.Sprintf("✓ %d chunks stored for %s — processing now...", stored, sourceName))

	// Auto-process all chunks
	processResult, _ := h.handleProcessAll(map[string]interface{}{"source": sourceName})
	if processResult != nil && !processResult.IsError {
		for _, block := range processResult.Content {
			if block.Text != "" {
				output = append(output, block.Text)
			}
		}
	}

	return textResult(strings.Join(output, "\n")), nil
}

func (h *ToolHandler) handleListSources(args map[string]interface{}) (*ToolResult, error) {
	globalOnly, _ := args["global_only"].(bool)
	sources, err := h.DB.ListSources(globalOnly)
	if err != nil {
		return textError(fmt.Sprintf("failed to list sources: %v", err)), nil
	}

	if len(sources) == 0 {
		return textResult("No sources indexed yet."), nil
	}

	var lines []string
	for _, s := range sources {
		scope := "local"
		if s.Global {
			scope = "global"
		}
		ver := s.Version
		if ver == "" {
			ver = "-"
		}
		lines = append(lines, fmt.Sprintf("%-20s %s  %s  symbols:%d sections:%d concepts:%d",
			s.Name, ver, scope, s.MethodCount, s.SectionCount, s.ConceptCount))
	}
	return textResult(strings.Join(lines, "\n")), nil
}

// logToolCall prints the incoming tool name + args to stderr.
func logToolCall(name string, args map[string]interface{}) {
	argsJSON, _ := json.Marshal(args)
	fmt.Fprintf(os.Stderr, "\n┌─ tool → %s\n│  args: %s\n", name, truncate(string(argsJSON), 500))
}

// logToolResult prints the tool result summary to stderr.
func logToolResult(name string, res *ToolResult, dur time.Duration) {
	if res == nil {
		fmt.Fprintf(os.Stderr, "└─ %s (%s) <nil>\n", name, dur)
		return
	}
	var buf strings.Builder
	for _, b := range res.Content {
		buf.WriteString(b.Text)
	}
	status := "ok"
	if res.IsError {
		status = "error"
	}
	out := truncate(buf.String(), 800)
	fmt.Fprintf(os.Stderr, "│  out (%d bytes, %s):\n", buf.Len(), status)
	for _, ln := range strings.Split(out, "\n") {
		fmt.Fprintf(os.Stderr, "│    %s\n", ln)
	}
	fmt.Fprintf(os.Stderr, "└─ %s %s\n", name, dur)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// AutoIngestLocalDocs scans a freshly-fetched source directory for
// README / docs / doc folders, chunks markdown, stores chunks, and
// auto-processes them into the srcmap doc hierarchy. Returns a summary.
func (h *ToolHandler) AutoIngestLocalDocs(ctx context.Context, sourceName, sourcePath string) (string, error) {
	if sourcePath == "" {
		return "", fmt.Errorf("sourcePath is empty")
	}

	var pages []docfetcher.RawPage

	// README at root
	for _, fn := range []string{"README.md", "README.MD", "readme.md"} {
		p := filepath.Join(sourcePath, fn)
		if b, err := os.ReadFile(p); err == nil {
			pages = append(pages, docfetcher.RawPage{
				URL:     fn,
				Title:   sourceName + " README",
				Content: string(b),
			})
			break
		}
	}

	// docs/ or doc/ subfolders
	walker := &docfetcher.MarkdownWalker{}
	for _, sub := range []string{"docs", "doc", "documentation"} {
		d := filepath.Join(sourcePath, sub)
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			ps, err := walker.Walk(d)
			if err == nil {
				pages = append(pages, ps...)
			}
		}
	}

	if len(pages) == 0 {
		return "no local docs found (no README or docs/ folder)", nil
	}

	chunker := &docfetcher.DefaultChunker{}
	chunks, err := chunker.Chunk(sourceName, pages)
	if err != nil {
		return "", fmt.Errorf("chunking: %w", err)
	}

	stored := 0
	for i := range chunks {
		id, err := h.DB.InsertChunk(&chunks[i])
		if err != nil {
			continue
		}
		chunks[i].ID = id
		stored++
	}

	// Write source.yaml marking local origin
	sy := &fileformat.SourceYAML{
		Name: sourceName,
		DocOrigin: &fileformat.DocOrigin{
			URL:          "local:" + sourcePath,
			ContentType:  "local-markdown",
			DiscoveredAt: time.Now().UTC().Format(time.RFC3339),
			Validated:    true,
		},
	}
	hb := fileformat.NewHierarchyBuilder(h.SrcmapDir, sourceName)
	hb.EnsureStructure()
	fileformat.WriteSourceYAML(filepath.Join(h.SrcmapDir, "docs", sourceName, "source.yaml"), sy)

	// Auto-process
	res, _ := h.handleProcessAll(map[string]interface{}{"source": sourceName})
	summary := fmt.Sprintf("ingested %d local doc pages → %d chunks stored", len(pages), stored)
	if res != nil && len(res.Content) > 0 {
		summary += "\n" + res.Content[0].Text
	}
	return summary, nil
}

func textResult(text string) *ToolResult {
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}}
}

func textError(text string) *ToolResult {
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}, IsError: true}
}
