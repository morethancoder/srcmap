# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

srcmap is a Go CLI tool + MCP server that gives AI coding agents structured, surgical context about any dependency or API. It maintains a local, versioned knowledge base per dependency — combining source code, structured docs, and semantic concepts — queryable via MCP tools or a standalone terminal chat (`srcmap agent`).

Two modes of operation:
1. **MCP server** — exposes tools to external agents (Claude Code, Cursor, Windsurf)
2. **`srcmap agent`** — standalone terminal chat powered by any OpenRouter model

## Build and test commands

```bash
go build ./...                    # build all packages
go test ./...                     # run all tests
go test ./... -race               # run all tests with race detector (required for CI)
go test ./internal/fetcher/...    # run tests for a single package
go test -run TestFunctionName ./internal/fetcher/  # run a single test
golangci-lint run                 # lint
```

## Architecture

```
cmd/srcmap/main.go          — CLI entrypoint (cobra)
internal/
  config/                   — config loading, global (~/.srcmap/) vs local (.srcmap/) resolution
  fetcher/                  — parallel source fetching (npm, pypi, go modules, git clone via go-git)
  parser/                   — symbol extraction: go/ast for Go, regex-based for TypeScript/Python
  docfetcher/               — raw doc ingestion pipeline:
    discovery.go            — LLM-powered doc source discovery (prefers llms.txt/docs.md over scraping)
    crawler.go              — URL crawl fallback with CSS selector + depth
    openapi.go              — OpenAPI/Swagger spec parser
    markdown.go             — local/GitHub markdown directory walker
    chunker.go              — pure-Go pre-chunking (no LLM): splits raw content into token-bounded
                              chunks with context headers before any LLM processing
  linker/                   — joins doc chunks to code symbols (fuzzy name + tag matching)
  index/                    — SQLite read/write layer (symbols, docs, sources, chunks, fingerprints)
  updater/                  — incremental updates via three-layer fingerprinting (page → chunk → symbol)
  mcp/                      — MCP protocol server (JSON-RPC 2.0 over stdio)
  agent/                    — terminal chat UI, OpenRouter client, tool-use loop, cost tracking
pkg/fileformat/             — doc file read/write, YAML frontmatter parsing
```

## Key design decisions

- **SQLite via `modernc.org/sqlite`** — pure Go, no CGO required
- **Go `go/ast` for Go files, regex-based for TS/Python** — no CGO dependency for parsing
- **Git operations via `go-git`** — no shelling out to git
- **Pre-chunking is pure Go, deterministic, no LLM** — the LLM never sees full pages; all content is split into token-bounded chunks (max 3000 tokens) with context headers before LLM processing
- **LLM-powered doc discovery** runs before any crawling — searches for llms.txt, docs.md, or similar LLM-friendly formats first; falls back to web scraping only when none found
- **All MCP tools are pure retrieval** — no LLM calls or network at query time, sub-10ms responses
- **Two scopes**: local (`.srcmap/`, committed to repo) and global (`~/.srcmap/`, reused across projects)
- **`<!-- custom -->` blocks** in doc files are never overwritten by updates
- **All external HTTP calls mocked with `httptest.NewServer`** in tests — no real network calls
- **All filesystem test operations use `t.TempDir()`** — no writes to real user dirs

## Data flow

1. `srcmap fetch` → clone repo at lockfile version → AST/regex parse → symbols to SQLite
2. `srcmap docs add` → LLM discovery finds best doc URL → fetch raw content → pre-chunker splits into token-bounded chunks → chunks stored in DB as `pending`
3. Agent calls `srcmap_process_chunk` per chunk → structured markdown files written to `.srcmap/docs/{source}/` hierarchy → chunk status updated to `processed`
4. MCP tools query SQLite + read markdown files to serve agent requests

## Doc file hierarchy

Each source produces: `index.md` (root map) → `{section}/section.md` → `{section}/methods/{method}.md` + `{section}/concepts/{concept}.md` + `gotchas.md`. All files have YAML frontmatter with id, kind, version, fingerprint, and related entries.
