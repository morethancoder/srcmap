package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/morethancoder/srcmap/internal/docfetcher"
	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/pkg/fileformat"
)

// ToolHandler handles MCP tool calls backed by a real database and file system.
type ToolHandler struct {
	DB        *index.DB
	SrcmapDir string // .srcmap/ directory path
}

// NewToolHandler creates a tool handler.
func NewToolHandler(db *index.DB, srcmapDir string) *ToolHandler {
	return &ToolHandler{DB: db, SrcmapDir: srcmapDir}
}

// AllTools returns definitions for all MCP tools.
func (h *ToolHandler) AllTools() []Tool {
	return []Tool{
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
			Description: "Process one pre-chunked text block. The agent LLM classifies and structures the chunk into a doc file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"chunk_id":       map[string]string{"type": "number", "description": "Chunk ID from the database"},
					"classification": map[string]string{"type": "string", "description": "method | concept | gotcha | ignore"},
					"section":        map[string]string{"type": "string", "description": "Section name this chunk belongs to"},
					"content":        map[string]string{"type": "string", "description": "Structured markdown content produced by the agent"},
					"frontmatter":    map[string]string{"type": "string", "description": "JSON-encoded frontmatter fields"},
				},
				"required": []string{"chunk_id", "classification"},
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
	}
}

// CallTool dispatches a tool call by name.
func (h *ToolHandler) CallTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error) {
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
	case "srcmap_process_status":
		return h.handleProcessStatus(args)
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
	return readFileResult(path)
}

func (h *ToolHandler) handleDocSection(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	section, _ := args["section"].(string)
	path := filepath.Join(h.SrcmapDir, "docs", source, section, "section.md")
	return readFileResult(path)
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
	return readFileResult(path)
}

func (h *ToolHandler) handleSourceInfo(args map[string]interface{}) (*ToolResult, error) {
	source, _ := args["source"].(string)
	rec, err := h.DB.GetSource(source)
	if err != nil {
		return textError(err.Error()), nil
	}

	text := fmt.Sprintf("Name: %s\nVersion: %s\nLast Updated: %s\nMethods: %d\nSections: %d\nConcepts: %d\nGotchas: %d",
		rec.Name, rec.Version, rec.LastUpdated, rec.MethodCount, rec.SectionCount, rec.ConceptCount, rec.GotchaCount)
	return textResult(text), nil
}

func (h *ToolHandler) handleProcessChunk(args map[string]interface{}) (*ToolResult, error) {
	chunkIDFloat, ok := args["chunk_id"].(float64)
	if !ok {
		return textError("chunk_id is required and must be a number"), nil
	}
	chunkID := int64(chunkIDFloat)
	classification, ok := args["classification"].(string)
	if !ok || classification == "" {
		return textError("classification is required (method|concept|gotcha|ignore)"), nil
	}

	if classification == "ignore" {
		if err := h.DB.UpdateChunkStatus(chunkID, docfetcher.ChunkProcessed); err != nil {
			return textError(err.Error()), nil
		}
		return textResult("Chunk ignored."), nil
	}

	content, _ := args["content"].(string)
	section, _ := args["section"].(string)
	fmJSON, _ := args["frontmatter"].(string)

	var fm fileformat.Frontmatter
	if fmJSON != "" {
		if err := json.Unmarshal([]byte(fmJSON), &fm); err != nil {
			return textError(fmt.Sprintf("invalid frontmatter JSON: %v", err)), nil
		}
	}
	fm.AutoGenerated = true
	fm.LastUpdated = fileformat.Now()

	// Use explicit section param, fall back to frontmatter section
	sourceName := section
	if sourceName == "" {
		sourceName = fm.Section
	}

	hb := fileformat.NewHierarchyBuilder(h.SrcmapDir, fm.ID)

	switch classification {
	case "method":
		fm.Kind = fileformat.KindMethod
		if err := hb.WriteMethod(sourceName, &fileformat.DocFile{Frontmatter: fm, Body: "\n" + content + "\n"}); err != nil {
			h.DB.UpdateChunkStatus(chunkID, docfetcher.ChunkFailed)
			return textError(err.Error()), nil
		}
	case "concept":
		fm.Kind = fileformat.KindConcept
		if err := hb.WriteConcept(sourceName, &fileformat.DocFile{Frontmatter: fm, Body: "\n" + content + "\n"}); err != nil {
			h.DB.UpdateChunkStatus(chunkID, docfetcher.ChunkFailed)
			return textError(err.Error()), nil
		}
	case "gotcha":
		if err := hb.AppendGotcha(fm.ID, content); err != nil {
			h.DB.UpdateChunkStatus(chunkID, docfetcher.ChunkFailed)
			return textError(err.Error()), nil
		}
	}

	if err := h.DB.UpdateChunkStatus(chunkID, docfetcher.ChunkProcessed); err != nil {
		return textError(err.Error()), nil
	}

	return textResult(fmt.Sprintf("Chunk %d processed as %s.", chunkID, classification)), nil
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
	var found string

	filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		dir := filepath.Base(filepath.Dir(path))
		base := strings.TrimSuffix(filepath.Base(path), ".md")
		if dir == kind && strings.EqualFold(base, name) {
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

func textResult(text string) *ToolResult {
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}}
}

func textError(text string) *ToolResult {
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}, IsError: true}
}
