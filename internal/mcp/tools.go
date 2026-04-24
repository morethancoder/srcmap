package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

	// Optional: a second index.db at ~/.srcmap/index.db for global sources.
	// When set, srcmap_list_sources merges across scopes so agents can see
	// both project-local and user-global sources in one response.
	GlobalDB *index.DB
}

// NewToolHandler creates a tool handler.
func NewToolHandler(db *index.DB, srcmapDir string) *ToolHandler {
	return &ToolHandler{DB: db, SrcmapDir: srcmapDir}
}

// AllTools returns definitions for all MCP tools.
//
// Tool descriptions follow a strict convention so calling agents can pick the
// right tool without reading the code:
//
//  1. First line is a one-sentence summary of WHAT the tool does.
//  2. Second line (when helpful) says WHEN to use it — and names the tool to
//     try first if this isn't the right one.
//  3. Descriptions mention the parameter spelling the agent actually needs
//     (e.g. "source ID such as 'zod' — lowercase"), not just the name.
func (h *ToolHandler) AllTools() []Tool {
	sourceProp := map[string]string{"type": "string", "description": "Source ID (e.g. 'zod', 'flask', 'gin') as reported by srcmap_list_sources."}

	tools := []Tool{
		{
			Name: "srcmap_list_sources",
			Description: "List every indexed source with its version, scope, symbol and section counts. " +
				"Start here whenever you are unsure what is available.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"global_only": map[string]interface{}{"type": "boolean", "description": "If true, only list sources from the user's global ~/.srcmap/ scope. Default false."},
				},
			},
		},
		{
			Name: "srcmap_source_info",
			Description: "Return metadata for one source: version, last_updated, symbol/section/concept/gotcha counts, and doc-chunk processing status. " +
				"Use this to confirm a source is ready before querying it.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"source": sourceProp},
				"required":   []string{"source"},
			},
		},
		{
			Name: "srcmap_find",
			Description: "Unified fast lookup — ALWAYS PREFER THIS when you are not certain the name is exact. " +
				"Tries exact symbol / doc-file name match first, then FTS5 ranked search across all chunks with BM25 snippets. " +
				"Falls back to srcmap_lookup / srcmap_doc_search when you already know the exact identifier.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": sourceProp,
					"query":  map[string]string{"type": "string", "description": "Name, phrase, or keyword. Camel/snake-case both work."},
					"limit":  map[string]interface{}{"type": "number", "description": "Max ranked results. Default 10."},
				},
				"required": []string{"source", "query"},
			},
		},
		{
			Name: "srcmap_lookup",
			Description: "Exact-name lookup of a code symbol (function / method / type / constant). " +
				"Returns file path, line range, parameters, return type, AND the actual source lines inline (capped at 300 lines). " +
				"You should NOT need to open the file separately — everything you need to read or paste is in the response. " +
				"If you don't know the exact name, use srcmap_find instead.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": sourceProp,
					"symbol": map[string]string{"type": "string", "description": "Exact symbol name (e.g. 'ZodString.min', 'bot.SendMessage')."},
				},
				"required": []string{"source", "symbol"},
			},
		},
		{
			Name: "srcmap_search_code",
			Description: "Substring search over parsed code symbols by name (SQL LIKE '%query%'). " +
				"Use this to enumerate candidates by naming pattern; use srcmap_find for semantic search over docs.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": sourceProp,
					"query":  map[string]string{"type": "string", "description": "Substring of the symbol name."},
				},
				"required": []string{"source", "query"},
			},
		},
		{
			Name: "srcmap_doc_map",
			Description: "Return the root index.md for a source: a top-level map of every documentation section. " +
				"Call this first when exploring a newly-added source to decide which section to drill into. " +
				"If no map exists, the response tells you to call srcmap_write_map to create one.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"source": sourceProp},
				"required":   []string{"source"},
			},
		},
		{
			Name: "srcmap_write_map",
			Description: "Create or overwrite the root index.md for a source. " +
				"Use this AFTER srcmap_fetch or srcmap_docs_add so future agents can navigate via map → section → method/concept → file. " +
				"Provide a short curated description per section instead of letting the auto-generated \"Documentation for X\" stubs stand. " +
				"Pass the same `source` you saw in srcmap_list_sources. Any existing <!-- custom --> blocks in the current index.md are preserved.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source":   sourceProp,
					"overview": map[string]string{"type": "string", "description": "Optional 1-3 sentence overview of what this source is and when to use it. Rendered above the section list."},
					"sections": map[string]interface{}{
						"type":        "array",
						"description": "Ordered list of sections to show in the map. Each entry becomes a `- **[name](name/section.md)** — description` line.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":        map[string]string{"type": "string", "description": "Section name — MUST match an existing directory under .srcmap/docs/<source>/, or the link will dead-end."},
								"description": map[string]string{"type": "string", "description": "One-line description of what's in this section. Think 'what would let an agent decide whether to drill in?'."},
							},
							"required": []string{"name", "description"},
						},
					},
				},
				"required": []string{"source", "sections"},
			},
		},
		{
			Name:        "srcmap_doc_section",
			Description: "Return a section's section.md — a listing of every method and concept within that section, with one-line summaries.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source":  sourceProp,
					"section": map[string]string{"type": "string", "description": "Section name as shown by srcmap_doc_map (case-insensitive)."},
				},
				"required": []string{"source", "section"},
			},
		},
		{
			Name:        "srcmap_doc_lookup",
			Description: "Return the full method.md doc body for one named method — use after srcmap_doc_section or srcmap_find surfaces the method name.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": sourceProp,
					"method": map[string]string{"type": "string", "description": "Method name (case-insensitive). Matches the file name under <section>/methods/."},
				},
				"required": []string{"source", "method"},
			},
		},
		{
			Name:        "srcmap_doc_concept",
			Description: "Return the full concept.md doc body for one named concept (non-callable topic like 'Middleware', 'Auth', 'Streaming').",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source":  sourceProp,
					"concept": map[string]string{"type": "string", "description": "Concept name (case-insensitive)."},
				},
				"required": []string{"source", "concept"},
			},
		},
		{
			Name: "srcmap_doc_search",
			Description: "FTS5 ranked search across all doc chunks for a source — returns highlighted BM25 snippets. " +
				"Prefer srcmap_find unless you specifically want doc-only hits (no symbols).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": sourceProp,
					"query":  map[string]string{"type": "string", "description": "Search query — supports multi-word phrases."},
					"limit":  map[string]interface{}{"type": "number", "description": "Max results. Default 10."},
				},
				"required": []string{"source", "query"},
			},
		},
		{
			Name: "srcmap_doc_gotchas",
			Description: "Return the gotchas.md file for a source — known footguns, breaking changes, common pitfalls. " +
				"Consult this before suggesting non-trivial code that uses a library.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": sourceProp,
					"method": map[string]string{"type": "string", "description": "Optional method filter (currently informational)."},
				},
				"required": []string{"source"},
			},
		},
		{
			Name: "srcmap_process_chunk",
			Description: "Process one pending doc chunk by ID — auto-classifies it and writes its doc file. " +
				"Used only for fine-grained control; prefer srcmap_process_all for the normal case.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"chunk_id": map[string]string{"type": "number", "description": "Chunk ID (returned by the database, shown in srcmap_process_status when --verbose)."},
				},
				"required": []string{"chunk_id"},
			},
		},
		{
			Name: "srcmap_process_all",
			Description: "Process every pending doc chunk for a source in a single call: classify, write method/concept/gotcha files, rebuild the index and section listings, and update counts. " +
				"srcmap_docs_add already calls this automatically — you only need it if a previous run was interrupted.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"source": sourceProp},
				"required":   []string{"source"},
			},
		},
		{
			Name:        "srcmap_process_status",
			Description: "Return the pending / processed / failed chunk counts for a source — use this to verify doc ingestion finished.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"source": sourceProp},
				"required":   []string{"source"},
			},
		},
		{
			Name: "srcmap_delete_source",
			Description: "Permanently delete a source from the database AND remove its .srcmap/docs/<source>/ directory. " +
				"Irreversible — only use when the user explicitly asks to remove a source.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]string{"type": "string", "description": "Source ID to delete."},
				},
				"required": []string{"source"},
			},
		},
	}

	// Fetch/docs-add/update tools are exposed only when the orchestrator is
	// wired up (i.e. we are running `srcmap mcp` from a project directory).
	if h.Orchestrator != nil {
		tools = append(tools,
			Tool{
				Name: "srcmap_fetch",
				Description: "Clone a package's source, parse its symbols, and auto-ingest local docs (README, docs/). " +
					"First step for any new source. Accepts npm names ('zod'), pypi ('pypi:flask'), Go modules ('github.com/gin-gonic/gin'), or GitHub shorthand ('owner/repo'). " +
					"After fetching, call srcmap_docs_add with an official docs URL for best-quality doc coverage.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"packages": map[string]interface{}{
							"type":        "array",
							"items":       map[string]string{"type": "string"},
							"description": "Package identifiers to fetch. Examples: ['zod'], ['pypi:flask'], ['github.com/gin-gonic/gin'], ['owner/repo'].",
						},
						"global": map[string]interface{}{"type": "boolean", "description": "If true, store in the user's global ~/.srcmap/ scope so other projects can reuse this source."},
					},
					"required": []string{"packages"},
				},
			},
			Tool{
				Name: "srcmap_docs_add",
				Description: "Ingest documentation for a source in one call. Auto-detects whether the URL is llms.txt / llms-full.txt, a plain markdown file, an OpenAPI spec, or an HTML site (in which case it crawls sub-pages up to depth 2). Chunks, stores, processes, and writes the full doc hierarchy. " +
					"The source must already exist (typically from srcmap_fetch). If you don't know the best URL, ask the user or search for llms.txt / llms-full.txt / /docs first.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"source": map[string]string{"type": "string", "description": "Source ID. Must match the name used with srcmap_fetch."},
						"url":    map[string]string{"type": "string", "description": "Root docs URL. Prefer llms.txt / llms-full.txt / docs.md when available."},
					},
					"required": []string{"source", "url"},
				},
			},
			Tool{
				Name: "srcmap_ingest_local_docs",
				Description: "Offline fallback: ingest docs only from the source's own README.md and docs/ folder. " +
					"Use when no upstream docs URL is available or the network is unreachable. srcmap_fetch calls this automatically on every fetch, so you usually don't need to call it yourself.",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"source": sourceProp},
					"required":   []string{"source"},
				},
			},
			Tool{
				Name: "srcmap_update_source",
				Description: "Update one already-indexed source: re-clone at the latest upstream version, re-parse symbols, and re-ingest local docs. " +
					"Use this when the user says 'update source X', 'refresh X', or 'pull latest X'. Combines srcmap_outdated → re-fetch → re-index in a single call.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"source":  sourceProp,
						"refetch": map[string]interface{}{"type": "boolean", "description": "If true (default), always re-clone from upstream. If false, only re-parse what is already on disk."},
					},
					"required": []string{"source"},
				},
			},
			Tool{
				Name: "srcmap_outdated",
				Description: "Check every indexed source against its upstream registry (npm / pypi / go proxy / git) and report which local versions are behind. Does NOT modify anything. " +
					"Use this when the user asks 'what's out of date?' or 'check for updates'. Follow up with srcmap_update_source to apply updates.",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
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
	case "srcmap_write_map":
		return h.handleWriteMap(args)
	case "srcmap_doc_section":
		return h.handleDocSection(args)
	case "srcmap_doc_lookup":
		return h.handleDocLookup(args)
	case "srcmap_doc_concept":
		return h.handleDocConcept(args)
	case "srcmap_doc_search":
		return h.handleDocSearch(args)
	case "srcmap_find":
		return h.handleFind(args)
	case "srcmap_doc_gotchas":
		return h.handleDocGotchas(args)
	case "srcmap_source_info":
		return h.handleSourceInfo(args)
	case "srcmap_process_chunk":
		return h.handleProcessChunk(args)
	case "srcmap_process_all":
		return h.handleProcessAll(ctx, args)
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
	case "srcmap_delete_source":
		return h.handleDeleteSource(args)
	case "srcmap_update_source":
		return h.handleUpdateSource(ctx, args)
	case "srcmap_outdated":
		return h.handleOutdated(ctx, args)
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

	// Inline the actual source lines so the agent doesn't have to open the
	// file. Cap the snippet at a few hundred lines for huge definitions so
	// the tool stays snappy.
	if snippet := readSnippet(sym.FilePath, sym.StartLine, sym.EndLine, 300); snippet != "" {
		text += "\n\n```\n" + snippet + "\n```"
	}
	return textResult(text), nil
}

// readSnippet reads lines [start,end] (1-indexed, inclusive) from path,
// capped at maxLines. Returns the snippet or "" on any error. Appends a
// truncation notice if the range was clipped.
func readSnippet(path string, start, end, maxLines int) string {
	if path == "" || start < 1 || end < start {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if start > len(lines) {
		return ""
	}
	// Convert to 0-indexed slice bounds.
	from := start - 1
	to := end
	if to > len(lines) {
		to = len(lines)
	}

	clipped := false
	if to-from > maxLines {
		to = from + maxLines
		clipped = true
	}
	snippet := strings.Join(lines[from:to], "\n")
	if clipped {
		snippet += fmt.Sprintf("\n// ... (truncated to %d lines; full range is %d-%d)", maxLines, start, end)
	}
	return snippet
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
	dir, err := h.safeSourceDir(source)
	if err != nil {
		return textError(err.Error()), nil
	}
	path := filepath.Join(dir, "index.md")
	if _, err := os.Stat(path); err != nil {
		h.rebuildIndexFiles(source)
	}
	if _, err := os.Stat(path); err != nil {
		// Still no map and nothing to rebuild from — tell the agent what to
		// do next instead of failing silently.
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf(
				"No map exists yet for source %q.\n\n"+
					"This source has no curated doc hierarchy. To create one, pick the path that fits:\n\n"+
					"  • Docs not yet ingested → call srcmap_docs_add(source=%q, url=<docs URL>) to fetch and chunk them.\n"+
					"  • Ingested but un-curated → call srcmap_write_map(source=%q, sections=[...]) to write a real index.md.\n"+
					"  • Code-only source       → call srcmap_list_sources to confirm scope, then srcmap_write_map with one section per top-level module.\n\n"+
					"After writing the map, retry srcmap_doc_map to navigate sections → methods/concepts.",
				source, source, source)}},
		}, nil
	}
	return readFileResult(path)
}

// handleWriteMap lets the agent write a curated root index.md for a source,
// replacing the auto-generated "Documentation for X" stubs with real
// descriptions. Existing <!-- custom --> blocks in the file are preserved.
func (h *ToolHandler) handleWriteMap(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	if source == "" {
		return textError("source is required"), nil
	}
	if _, err := h.safeSourceDir(source); err != nil {
		return textError(err.Error()), nil
	}

	overview, _ := args["overview"].(string)
	rawSections, _ := args["sections"].([]interface{})
	if len(rawSections) == 0 {
		return textError("sections is required and must be a non-empty array"), nil
	}

	var summaries []fileformat.SectionSummary
	var skipped []string
	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	for _, raw := range rawSections {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		desc, _ := obj["description"].(string)
		if name == "" || desc == "" {
			continue
		}
		// Warn the agent about sections that don't exist on disk — the link
		// would dead-end — but still write them so the agent can hand-author
		// sections it plans to create.
		if _, err := os.Stat(filepath.Join(docsDir, sanitizeSection(name))); err != nil {
			skipped = append(skipped, name)
		}
		summaries = append(summaries, fileformat.SectionSummary{
			Name:        name,
			Description: desc,
		})
	}
	if len(summaries) == 0 {
		return textError("no valid section entries provided (need name + description)"), nil
	}

	hb := fileformat.NewHierarchyBuilder(h.SrcmapDir, source)
	if err := hb.EnsureStructure(); err != nil {
		return textError(fmt.Sprintf("ensuring docs dir: %v", err)), nil
	}

	// Merge overview into a <!-- custom --> block if provided, so subsequent
	// auto-rebuilds of index.md don't wipe it.
	if strings.TrimSpace(overview) != "" {
		indexPath := filepath.Join(docsDir, "index.md")
		existing, _ := os.ReadFile(indexPath)
		overviewBlock := "<!-- custom -->\n## Overview\n\n" + strings.TrimSpace(overview) + "\n<!-- /custom -->\n"
		if !strings.Contains(string(existing), "<!-- custom -->") {
			// First time: write a minimal index with just the overview so
			// WriteIndex's subsequent run picks it up as a custom block.
			_ = os.WriteFile(indexPath, []byte(overviewBlock), 0o644)
		}
	}

	if err := hb.WriteIndex(summaries); err != nil {
		return textError(fmt.Sprintf("writing index: %v", err)), nil
	}

	msg := fmt.Sprintf("✓ Wrote curated map for %q with %d section(s) → %s/index.md", source, len(summaries), docsDir)
	if len(skipped) > 0 {
		msg += fmt.Sprintf("\n\nWarning: these section names have no matching directory and their links will 404 until you create them:\n  - %s", strings.Join(skipped, "\n  - "))
	}
	msg += "\n\nNext: call srcmap_doc_map(source=\"" + source + "\") to verify, then srcmap_doc_section on each section."
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
}

func (h *ToolHandler) handleDocSection(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	section, _ := args["section"].(string)
	dir, err := h.safeSourceDir(source)
	if err != nil {
		return textError(err.Error()), nil
	}
	secSan := sanitizeSection(section)
	if secSan == "" {
		return textError("section is required"), nil
	}
	path := filepath.Join(dir, secSan, "section.md")
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
	// Search for the method file across sections. If nothing matches, fall
	// through to concepts/ since section.md links mix both kinds and the
	// agent may not know which one a given name is.
	res, _ := h.findDocFile(source, "methods", method)
	if res != nil && !res.IsError {
		return res, nil
	}
	return h.findDocFile(source, "concepts", method)
}

func (h *ToolHandler) handleDocConcept(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	concept, _ := args["concept"].(string)
	return h.findDocFile(source, "concepts", concept)
}

func (h *ToolHandler) handleDocSearch(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	query, _ := args["query"].(string)
	limit := parseLimit(args["limit"], 10)

	if source == "" || query == "" {
		return textError("source and query are required"), nil
	}

	fts := ftsQuery(query)
	var matches []index.DocMatch
	if fts != "" {
		var err error
		matches, err = h.DB.SearchDocs(source, fts, limit)
		if err != nil {
			return textError(fmt.Sprintf("search failed: %v", err)), nil
		}
	}
	if len(matches) > 0 {
		var lines []string
		for i, m := range matches {
			title := m.Heading
			if title == "" {
				title = fmt.Sprintf("chunk-%d", m.ChunkID)
			}
			lines = append(lines, fmt.Sprintf("%d. %s  (rank %.2f)", i+1, title, m.Rank))
			lines = append(lines, "   "+singleLine(m.Snippet))
		}
		return textResult(strings.Join(lines, "\n")), nil
	}

	// FTS empty — fall back to a file walk so doc files written outside the
	// chunk pipeline (CHANGELOG, hand-authored custom blocks, tests) remain
	// discoverable.
	if files := h.grepDocFiles(source, query, limit); len(files) > 0 {
		return textResult(strings.Join(files, "\n")), nil
	}
	return textResult(fmt.Sprintf("No matches for %q in %q.", query, source)), nil
}

func (h *ToolHandler) grepDocFiles(source, query string, limit int) []string {
	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	needle := strings.ToLower(query)
	var hits []string
	filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if len(hits) >= limit {
			return filepath.SkipAll
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(string(data)), needle) {
			rel, _ := filepath.Rel(docsDir, path)
			hits = append(hits, rel)
		}
		return nil
	})
	return hits
}

// ftsQuery wraps a freeform user query in FTS5 syntax. Terms are quoted so
// camelCase / punctuation tokens work unchanged. Returns "" when the input
// has no usable tokens — callers must skip FTS in that case.
func ftsQuery(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	for i, f := range fields {
		fields[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
	}
	return strings.Join(fields, " ")
}

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, 240)
}

func parseLimit(v interface{}, def int) int {
	if f, ok := v.(float64); ok && f > 0 {
		return int(f)
	}
	return def
}

// handleFind is the unified, fast lookup: tries exact symbol/method/concept
// name matches first, then falls back to FTS5 ranked search with snippets.
func (h *ToolHandler) handleFind(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	query, _ := args["query"].(string)
	limit := parseLimit(args["limit"], 10)

	if source == "" || query == "" {
		return textError("source and query are required"), nil
	}

	var out []string

	if sym, err := h.DB.LookupSymbol(source, query); err == nil && sym != nil {
		out = append(out, fmt.Sprintf("✓ symbol %s (%s) — %s:%d-%d", sym.Name, sym.Kind, sym.FilePath, sym.StartLine, sym.EndLine))
	}

	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	target := sanitizeSection(query)
	if target != "" {
		filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			if len(out) >= limit {
				return filepath.SkipAll
			}
			base := strings.TrimSuffix(info.Name(), ".md")
			if base == target {
				rel, _ := filepath.Rel(h.SrcmapDir, path)
				out = append(out, fmt.Sprintf("✓ doc file .srcmap/%s", rel))
			}
			return nil
		})
	}

	fts := ftsQuery(query)
	var matches []index.DocMatch
	if fts != "" {
		var err error
		matches, err = h.DB.SearchDocs(source, fts, limit)
		if err != nil {
			return textError(fmt.Sprintf("fts search failed: %v", err)), nil
		}
	}

	if len(out) == 0 && len(matches) == 0 {
		return textResult(fmt.Sprintf("No hits for %q in %q.", query, source)), nil
	}

	if len(matches) > 0 {
		out = append(out, "", fmt.Sprintf("Top %d ranked matches:", len(matches)))
		for i, m := range matches {
			title := m.Heading
			if title == "" {
				title = fmt.Sprintf("chunk-%d", m.ChunkID)
			}
			out = append(out, fmt.Sprintf("%d. %s  (rank %.2f)", i+1, title, m.Rank))
			out = append(out, "   "+singleLine(m.Snippet))
		}
	}
	return textResult(strings.Join(out, "\n")), nil
}

func (h *ToolHandler) handleDocGotchas(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	dir, err := h.safeSourceDir(source)
	if err != nil {
		return textError(err.Error()), nil
	}
	path := filepath.Join(dir, "gotchas.md")
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

	cls := classifyChunk(chunk)
	section := extractSection(chunk, chunk.SourceID)
	docID := sanitizeSection(cls.subject)
	if docID == "" {
		docID = fmt.Sprintf("chunk-%d", chunk.ID)
	}

	result, err := h.processOneChunk(chunk, cls.kind, section, docID)
	if err != nil {
		return textError(err.Error()), nil
	}
	return textResult(result), nil
}

func (h *ToolHandler) handleProcessAll(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
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

	total := float64(len(chunks))
	reportN(ctx, 0, total, fmt.Sprintf("processing %d chunks for %s", len(chunks), source))

	// Emit ~10 progress events max so clients aren't flooded on big sources.
	step := len(chunks) / 10
	if step < 1 {
		step = 1
	}

	for i := range chunks {
		chunk := &chunks[i]
		if i%step == 0 || i == len(chunks)-1 {
			reportN(ctx, float64(i), total, fmt.Sprintf("chunk %d/%d", i+1, len(chunks)))
		}
		cls := classifyChunk(chunk)
		if cls.kind == kindIgnore {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkProcessed)
			ignored++
			continue
		}

		section := extractSection(chunk, source)
		docID := sanitizeSection(cls.subject)
		if docID == "" {
			docID = fmt.Sprintf("chunk-%d", chunk.ID)
		}

		if _, err := h.processOneChunk(chunk, cls.kind, section, docID); err != nil {
			failed++
			continue
		}
		processed++
		sections[section] = true

		switch cls.kind {
		case kindMethod:
			methods++
		case kindConcept:
			concepts++
		case kindGotcha:
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
	output = append(output, fmt.Sprintf("\n▸ index.md + per-section maps (re)built at .srcmap/docs/%s/", source))
	output = append(output, fmt.Sprintf("▸ Navigate: srcmap_doc_map(source=%q) → srcmap_doc_section → srcmap_doc_lookup / srcmap_doc_concept.", source))
	output = append(output, fmt.Sprintf("▸ Index descriptions are auto-stubbed (\"Documentation for X\"). Replace with real ones via srcmap_write_map(source=%q, sections=[...]).", source))

	return textResult(strings.Join(output, "\n")), nil
}

// processOneChunk writes a single chunk as a doc file and updates its status.
func (h *ToolHandler) processOneChunk(chunk *docfetcher.Chunk, kind chunkKind, section, docID string) (string, error) {
	if kind == kindIgnore {
		if err := h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkProcessed); err != nil {
			return "", err
		}
		return fmt.Sprintf("Chunk %d ignored.", chunk.ID), nil
	}

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

	switch kind {
	case kindMethod:
		fm.Kind = fileformat.KindMethod
		fm.Symbol = docID
		if err := hb.WriteMethod(section, &fileformat.DocFile{Frontmatter: fm, Body: "\n" + body + "\n"}); err != nil {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkFailed)
			return "", fmt.Errorf("writing method doc: %w", err)
		}
	case kindConcept:
		fm.Kind = fileformat.KindConcept
		if err := hb.WriteConcept(section, &fileformat.DocFile{Frontmatter: fm, Body: "\n" + body + "\n"}); err != nil {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkFailed)
			return "", fmt.Errorf("writing concept doc: %w", err)
		}
	case kindGotcha:
		if err := hb.AppendGotcha(docID, body); err != nil {
			h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkFailed)
			return "", fmt.Errorf("writing gotcha: %w", err)
		}
	}

	if err := h.DB.UpdateChunkStatus(chunk.ID, docfetcher.ChunkProcessed); err != nil {
		return "", err
	}

	return fmt.Sprintf("Chunk %d → %s/%s/%s.md", chunk.ID, chunk.SourceID, section, kind), nil
}

type chunkKind string

const (
	kindMethod  chunkKind = "method"
	kindConcept chunkKind = "concept"
	kindGotcha  chunkKind = "gotcha"
	kindIgnore  chunkKind = "ignore"
)

// classification is the output of classifyChunk: kind + the best subject name
// to use as the doc file basename (e.g. "sendMessage" for methods).
type classification struct {
	kind    chunkKind
	subject string
}

var (
	// "<camelCase word>\n ... Returns <X> on success" — Telegram-style method signature.
	methodSigRe = regexp.MustCompile(`(?m)^([a-z][a-zA-Z0-9_]{2,})\s*\n[\s\S]{0,400}?Returns\b[\s\S]{0,80}?\bon success`)
	// Parameter table header row.
	paramTableRe = regexp.MustCompile(`(?i)Parameter\s*\n\s*Type\s*\n\s*Required`)
	// Directive/function-style header (Alpine, Vue, etc.).
	directiveHeadingRe = regexp.MustCompile(`^(x-[\w-]+|v-[\w-]+|@[\w-]+)\b`)
	// Code-like signature inside heading: requires a closed paren so
	// narrative parentheticals like "Note (important)" don't match.
	codeSigHeadingRe = regexp.MustCompile(`\w+\s*\([^)]*\)|^[A-Z][a-zA-Z]+\.[a-z]\w*\s*\(`)
	// First camelCase identifier anywhere near the top of the body.
	firstCamelIdentRe = regexp.MustCompile(`(?m)^\s*([a-z][a-zA-Z0-9_]{2,})\s*$`)
)

// classifyChunk returns the kind + subject for a chunk.
func classifyChunk(c *docfetcher.Chunk) classification {
	if c.EstimatedTokens < 30 {
		return classification{kind: kindIgnore}
	}

	body := stripContextHeader(c.Content)
	low := strings.ToLower(body)
	heading := strings.TrimSpace(c.Heading)
	lowHeading := strings.ToLower(heading)

	gotchaMarkers := []string{"gotcha", "common mistake", "pitfall", "breaking change",
		"caution", "warning:", "deprecated", "known issue", "watch out", "be careful"}
	for _, p := range gotchaMarkers {
		if strings.Contains(lowHeading, p) || strings.Contains(low[:min(500, len(low))], p) {
			return classification{kind: kindGotcha, subject: pickSubject(heading, body, c)}
		}
	}

	if m := methodSigRe.FindStringSubmatch(body); m != nil && paramTableRe.MatchString(body) {
		return classification{kind: kindMethod, subject: m[1]}
	}
	if directiveHeadingRe.MatchString(heading) || codeSigHeadingRe.MatchString(heading) {
		return classification{kind: kindMethod, subject: pickSubject(heading, body, c)}
	}

	return classification{kind: kindConcept, subject: pickSubject(heading, body, c)}
}

// pickSubject returns the best human-readable name for a chunk:
// chunk heading → first camelCase identifier in body → chunk-N fallback.
func pickSubject(heading, body string, c *docfetcher.Chunk) string {
	if heading != "" {
		return heading
	}
	if m := firstCamelIdentRe.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return fmt.Sprintf("chunk-%d", c.ID)
}

// extractSection extracts a section name from the chunk's context header or
// heading, guarding against the common pitfall where the page title sanitizes
// to the source name (would produce .srcmap/docs/<src>/<src>/ nesting).
func extractSection(c *docfetcher.Chunk, sourceName string) string {
	sourceSan := sanitizeSection(sourceName)

	if idx := strings.Index(c.Content, "[Section: "); idx >= 0 {
		end := strings.Index(c.Content[idx:], "]")
		if end > 0 {
			s := sanitizeSection(strings.TrimSpace(c.Content[idx+10 : idx+end]))
			if s != "" && s != sourceSan {
				return s
			}
		}
	}

	if c.PageURL != "" {
		if u, err := url.Parse(c.PageURL); err == nil {
			segs := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i := len(segs) - 1; i >= 0; i-- {
				s := sanitizeSection(segs[i])
				if s != "" && s != "docs" && s != "index" && s != sourceSan {
					return s
				}
			}
		}
	}

	return "general"
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

// safeSourceDir returns the absolute path to .srcmap/docs/<source> guaranteed
// to stay under the srcmap docs root, or "" and an error if the input tries
// to escape (via "..", absolute paths, or weird separators).
func (h *ToolHandler) safeSourceDir(source string) (string, error) {
	if source == "" {
		return "", fmt.Errorf("source is required")
	}
	if strings.ContainsAny(source, `/\`) || source == ".." || strings.Contains(source, "..") {
		return "", fmt.Errorf("invalid source name %q", source)
	}
	root, err := filepath.Abs(filepath.Join(h.SrcmapDir, "docs"))
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(filepath.Join(root, source))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(dir+string(filepath.Separator), root+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid source name %q", source)
	}
	return dir, nil
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
			singular := kind[:len(kind)-1] // "method" or "concept"
			for _, f := range files {
				if f.IsDir() || filepath.Ext(f.Name()) != ".md" {
					continue
				}
				name := strings.TrimSuffix(f.Name(), ".md")
				summaries = append(summaries, fileformat.MethodSummary{
					Name:    name,
					Summary: singular,
					Kind:    singular,
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

// writeStubMap writes a minimal index.md for a freshly-fetched source so
// srcmap_doc_map has something to serve immediately. Lists symbols grouped
// by kind. Skipped when a curated map (non-stub or with <!-- custom --> block)
// already exists. Overwritten by srcmap_write_map or srcmap_process_all.
func (h *ToolHandler) writeStubMap(source string, symbols []parser.Symbol) {
	if source == "" || len(symbols) == 0 {
		return
	}
	docsDir := filepath.Join(h.SrcmapDir, "docs", source)
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return
	}
	indexPath := filepath.Join(docsDir, "index.md")
	if existing, err := os.ReadFile(indexPath); err == nil {
		// Don't clobber a curated or customized map.
		if strings.Contains(string(existing), "<!-- custom -->") ||
			strings.Contains(string(existing), "stub: false") {
			return
		}
	}

	const maxPerKind = 15
	buckets := map[parser.SymbolKind][]string{}
	for _, s := range symbols {
		if len(buckets[s.Kind]) >= maxPerKind {
			continue
		}
		name := s.Name
		if s.ParentScope != "" {
			name = s.ParentScope + "." + s.Name
		}
		buckets[s.Kind] = append(buckets[s.Kind], name)
	}

	order := []parser.SymbolKind{
		parser.SymbolType, parser.SymbolInterface, parser.SymbolClass,
		parser.SymbolFunction, parser.SymbolMethod, parser.SymbolConstant,
	}

	var body strings.Builder
	body.WriteString("\n# Index (stub)\n\n")
	body.WriteString("_Auto-generated placeholder listing the top symbols in this source._\n\n")
	body.WriteString(fmt.Sprintf("**Total symbols indexed:** %d\n\n", len(symbols)))
	body.WriteString("**To curate this map:**\n")
	body.WriteString(fmt.Sprintf("- `srcmap_write_map(source=%q, sections=[...])` — write a real, section-level overview\n", source))
	body.WriteString(fmt.Sprintf("- `srcmap_docs_add(source=%q, url=<docs URL>)` — ingest upstream docs first for richer sections\n\n", source))

	for _, kind := range order {
		entries := buckets[kind]
		if len(entries) == 0 {
			continue
		}
		label := strings.ToUpper(string(kind)[:1]) + string(kind)[1:] + "s"
		body.WriteString(fmt.Sprintf("## %s\n\n", label))
		for _, name := range entries {
			body.WriteString(fmt.Sprintf("- `%s`\n", name))
		}
		body.WriteString("\n")
	}

	content := "---\nid: index\nkind: index\nauto_generated: true\nstub: true\nlast_updated: \"" + fileformat.Now() + "\"\n---\n" + body.String()
	_ = os.WriteFile(indexPath, []byte(content), 0o644)
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

	global, _ := args["global"].(bool)
	var requests []fetcher.FetchRequest
	for _, name := range packageNames {
		requests = append(requests, fetcher.ParsePackageName(name, global))
	}

	report(ctx, fmt.Sprintf("fetching %d package(s): %s", len(requests), strings.Join(packageNames, ", ")))
	results := h.Orchestrator.FetchAll(ctx, requests)

	reg := h.ParserRegistry
	if reg == nil {
		reg = parser.NewRegistry()
	}

	var output []string
	total := float64(len(results))
	for idx, r := range results {
		reportN(ctx, float64(idx), total, fmt.Sprintf("indexing %s", r.Request.Name))
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

		// Clear any previously-indexed symbols so repeated fetches don't
		// accumulate duplicate rows.
		_ = h.DB.ClearSymbolsForSource(r.Source.Name)
		indexed := 0
		for i := range symbols {
			symbols[i].SourceID = r.Source.Name
			if _, err := h.DB.InsertSymbol(&symbols[i]); err == nil {
				indexed++
			}
		}
		report(ctx, fmt.Sprintf("%s: indexed %d symbols", r.Source.Name, indexed))
		output = append(output, fmt.Sprintf("✓ %s@%s fetched and indexed %d symbols", r.Source.Name, r.Source.Version, indexed))

		// Write a stub index.md so srcmap_doc_map has something to serve
		// immediately, and so srcmap_write_map has something to overwrite.
		h.writeStubMap(r.Source.Name, symbols)
		output = append(output, fmt.Sprintf("  stub map → .srcmap/docs/%s/index.md", r.Source.Name))

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
		output = append(output, "")
		output = append(output, "▸ OR curate the map right now (no docs ingestion required):")
		output = append(output, fmt.Sprintf("  srcmap_write_map(source=%q, overview=\"<1-3 sentences>\", sections=[{name, description}, ...])", r.Source.Name))
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
	report(ctx, "discovering content type: "+rawURL)
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
		report(ctx, fmt.Sprintf("fetching %s (%s)", rawURL, result.ContentType))
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
		report(ctx, fmt.Sprintf("crawling %s (depth=2, max %d pages, %s cap)", rawURL, crawler.MaxPages, crawler.Timeout))
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
	report(ctx, fmt.Sprintf("chunking %d page(s)", len(pages)))
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
	processResult, _ := h.handleProcessAll(ctx, map[string]interface{}{"source": sourceName})
	if processResult != nil && !processResult.IsError {
		for _, block := range processResult.Content {
			if block.Text != "" {
				output = append(output, block.Text)
			}
		}
	}

	return textResult(strings.Join(output, "\n")), nil
}

func (h *ToolHandler) handleDeleteSource(args map[string]interface{}) (*ToolResult, error) {
	sourceName, _ := args["source"].(string)
	docsDir, err := h.safeSourceDir(sourceName)
	if err != nil {
		return textError(err.Error()), nil
	}

	if err := h.DB.DeleteSource(sourceName); err != nil {
		return textError(fmt.Sprintf("deleting source: %v", err)), nil
	}
	if err := os.RemoveAll(docsDir); err != nil {
		return textResult(fmt.Sprintf("✓ deleted DB rows for %q; docs dir removal failed: %v", sourceName, err)), nil
	}
	return textResult(fmt.Sprintf("✓ deleted source %q (rows + %s)", sourceName, docsDir)), nil
}

func (h *ToolHandler) handleListSources(args map[string]interface{}) (*ToolResult, error) {
	globalOnly, _ := args["global_only"].(bool)

	var sources []index.SourceRecord
	// Local DB — only if caller wants everything.
	if !globalOnly {
		local, err := h.DB.ListSources(false)
		if err != nil {
			return textError(fmt.Sprintf("failed to list local sources: %v", err)), nil
		}
		sources = append(sources, local...)
	}
	// Global DB lives at ~/.srcmap/index.db (see cmd/srcmap/commands.go:
	// runMCP). All rows in that DB are global by definition, but the
	// Global column may or may not be set; tag the scope in the display
	// layer below so it's always correct.
	if h.GlobalDB != nil {
		global, err := h.GlobalDB.ListSources(false)
		if err == nil {
			for i := range global {
				global[i].Global = true
			}
			sources = append(sources, global...)
		}
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

// scanPerComponentDocs looks for per-component markdown in common monorepo
// layouts. Picks up antd-mobile's src/components/<x>/index.{en,zh}.md,
// npm workspaces' packages/<x>/README.md, and plain components/<x>/*.md.
// Filters out tests, fixtures, and changelogs so the auto-ingest stays
// focused on API-level content.
func scanPerComponentDocs(sourcePath string) []docfetcher.RawPage {
	var pages []docfetcher.RawPage
	roots := []string{
		filepath.Join(sourcePath, "src", "components"),
		filepath.Join(sourcePath, "packages"),
		filepath.Join(sourcePath, "components"),
	}
	wanted := map[string]bool{
		"index.en.md": true, "index.md": true, "index.zh.md": true,
		"README.md": true, "readme.md": true, "README.MD": true,
	}
	for _, root := range roots {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			comp := e.Name()
			// Skip obvious non-component dirs.
			switch comp {
			case "tests", "test", "__tests__", "demos", "fixtures", "utils", "node_modules":
				continue
			}
			compDir := filepath.Join(root, comp)
			compEntries, err := os.ReadDir(compDir)
			if err != nil {
				continue
			}
			for _, f := range compEntries {
				if f.IsDir() || !wanted[f.Name()] {
					continue
				}
				fp := filepath.Join(compDir, f.Name())
				b, err := os.ReadFile(fp)
				if err != nil {
					continue
				}
				pages = append(pages, docfetcher.RawPage{
					URL:     filepath.Join(filepath.Base(root), comp, f.Name()),
					Title:   comp,
					Content: string(b),
				})
			}
		}
	}
	return pages
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

	// Per-component docs in monorepos: src/components/<name>/index*.md,
	// packages/<name>/README.md, components/<name>/README.md, etc. These
	// hold the real API tables and prop tables that top-level docs/ don't.
	pages = append(pages, scanPerComponentDocs(sourcePath)...)

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
	res, _ := h.handleProcessAll(ctx, map[string]interface{}{"source": sourceName})
	summary := fmt.Sprintf("ingested %d local doc pages → %d chunks stored", len(pages), stored)
	if res != nil && len(res.Content) > 0 {
		summary += "\n" + res.Content[0].Text
	}
	return summary, nil
}

// handleUpdateSource re-fetches a single source at its latest upstream
// version, re-parses symbols, and re-ingests local docs.
func (h *ToolHandler) handleUpdateSource(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if h.Orchestrator == nil {
		return textError("update not available: orchestrator not configured"), nil
	}
	sourceName, _ := args["source"].(string)
	if sourceName == "" {
		return textError("source parameter is required"), nil
	}
	refetch := true
	if v, ok := args["refetch"].(bool); ok {
		refetch = v
	}

	rec, err := h.DB.GetSource(sourceName)
	if err != nil || rec == nil {
		return textError(fmt.Sprintf("source %q not found — run srcmap_fetch first", sourceName)), nil
	}

	src := *rec
	var output []string
	if refetch || src.Path == "" {
		req := fetcher.ParsePackageName(src.Name, src.Global)
		report(ctx, fmt.Sprintf("re-fetching %s…", src.Name))
		results := h.Orchestrator.FetchAll(ctx, []fetcher.FetchRequest{req})
		if len(results) == 0 || results[0].Err != nil {
			msg := "unknown error"
			if len(results) > 0 && results[0].Err != nil {
				msg = results[0].Err.Error()
			}
			return textError(fmt.Sprintf("re-fetch failed: %s", msg)), nil
		}
		r := results[0]
		previous := src.Version
		src.Version = r.Source.Version
		src.RepoURL = r.Source.RepoURL
		src.Path = r.Source.Path
		if previous != "" && previous != r.Source.Version {
			output = append(output, fmt.Sprintf("✓ upgraded %s: %s → %s", src.Name, previous, r.Source.Version))
		} else {
			output = append(output, fmt.Sprintf("✓ re-fetched %s @ %s", src.Name, r.Source.Version))
		}
	}

	reg := h.ParserRegistry
	if reg == nil {
		reg = parser.NewRegistry()
	}
	symbols, err := reg.ParseDirectory(src.Path)
	if err != nil {
		return textError(fmt.Sprintf("parse error: %v", err)), nil
	}
	_ = h.DB.ClearSymbolsForSource(src.ID)
	indexed := 0
	for i := range symbols {
		symbols[i].SourceID = src.ID
		if _, err := h.DB.InsertSymbol(&symbols[i]); err == nil {
			indexed++
		}
	}
	src.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	if err := h.DB.InsertSource(&src); err != nil {
		return textError(fmt.Sprintf("persisting source: %v", err)), nil
	}

	// Re-ingest docs from the freshly-cloned tree.
	if summary, err := h.AutoIngestLocalDocs(ctx, src.ID, src.Path); err == nil && summary != "" {
		output = append(output, summary)
	}

	output = append(output, fmt.Sprintf("✓ %s: re-indexed %d symbols", src.Name, indexed))
	return textResult(strings.Join(output, "\n")), nil
}

// handleOutdated compares each indexed source's stored version against what
// the upstream registry reports, and returns a short report.
func (h *ToolHandler) handleOutdated(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if h.Orchestrator == nil {
		return textError("outdated check not available: orchestrator not configured"), nil
	}
	sources, err := h.DB.ListSources(false)
	if err != nil {
		return textError(fmt.Sprintf("failed to list sources: %v", err)), nil
	}
	if len(sources) == 0 {
		return textResult("No sources indexed yet."), nil
	}

	var lines []string
	outdated := 0
	for _, s := range sources {
		req := fetcher.ParsePackageName(s.Name, s.Global)
		latest, _, err := h.Orchestrator.LatestVersion(ctx, req)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s local:%s upstream: error (%v)", s.Name, valueOrDashStr(s.Version), err))
			continue
		}
		local := strings.TrimPrefix(s.Version, "v")
		remote := strings.TrimPrefix(latest, "v")
		status := "up to date"
		if local != "" && remote != "" && local != remote {
			status = "outdated → " + latest
			outdated++
		} else if local == "" && remote != "" {
			status = "unknown → " + latest
			outdated++
		}
		lines = append(lines, fmt.Sprintf("%s local:%s %s", s.Name, valueOrDashStr(s.Version), status))
	}

	if outdated == 0 {
		lines = append(lines, "", "All sources up to date.")
	} else {
		lines = append(lines, "",
			fmt.Sprintf("%d source(s) behind upstream. Call srcmap_update_source(source=<name>) to update.", outdated))
	}
	return textResult(strings.Join(lines, "\n")), nil
}

func valueOrDashStr(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func textResult(text string) *ToolResult {
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}}
}

func textError(text string) *ToolResult {
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}, IsError: true}
}
