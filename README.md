# srcmap

Give AI coding agents surgical, structured context about any dependency or API.

srcmap maintains a local, versioned, incrementally-updated knowledge base per dependency — combining real source code, structured docs, and semantic concepts — queryable with surgical precision.

> If this project is useful to you, please consider giving it a star on GitHub.

## How It Works

srcmap runs in two modes:

1. **MCP Server** — exposes tools to AI coding agents (Claude Code, Cursor, Windsurf) so they can look up symbols, search code, and query documentation without loading entire repositories into context.
2. **Standalone Agent** — a terminal chat powered by any OpenRouter model that drives all srcmap tools autonomously.

When you fetch a package, srcmap clones the repo at the exact installed version, parses every file using Go's `go/ast` (for Go) or regex-based extraction (for TypeScript/Python), and indexes every function, method, class, type, interface, and constant into a local SQLite database.

When you add documentation, srcmap splits it into token-bounded chunks (max 3000 tokens each) with context headers, ready for LLM processing.

Agents query this index through MCP tools that return results in under 10ms — no LLM calls or network at query time.

## Installation

```bash
go install github.com/morethancoder/srcmap/cmd/srcmap@latest
```

Or build from source:

```bash
git clone https://github.com/morethancoder/srcmap.git
cd srcmap
go build -o srcmap ./cmd/srcmap
```

## Quick Start

### Initialize a project

```bash
srcmap init
```

This creates `.srcmap/` and auto-adds it to your `.gitignore`.

### Fetch and index a package

```bash
# npm package (default)
srcmap fetch zod

# Go module
srcmap fetch github.com/mymmrac/telego

# PyPI package
srcmap fetch pypi:requests

# Multiple packages at once (fetched in parallel)
srcmap fetch zod typescript react

# Store globally (~/.srcmap/) and reuse across projects
srcmap fetch github.com/mymmrac/telego --global
```

### Look up a symbol

```bash
srcmap lookup github.com/mymmrac/telego "Bot.SendMessage"
# Bot.SendMessage (method)
#   file: methods.go:276-283
#   params: (Context, SendMessageParams)
#   returns: (Message, error)
#   scope: Bot
```

### Search symbols

```bash
srcmap search github.com/mymmrac/telego "Webhook"
# WebhookHandler (type) — webhook.go:19
# Bot.SetWebhook (method) — methods.go:133
# Bot.DeleteWebhook (method) — methods.go:149
# Bot.UpdatesViaWebhook (method) — webhook.go:47
# ...
```

### Add documentation

```bash
# From a single URL (markdown, llms.txt, etc.)
srcmap docs add data-star --url https://data-star.dev/docs.md

# From an OpenAPI spec
srcmap docs add petstore --openapi petstore.yaml

# From a local markdown directory
srcmap docs add mylib --markdown ./docs
```

### Check status

```bash
srcmap sources        # list all sources with version and last updated
srcmap list           # compact source listing
srcmap check          # show pending/processed/failed chunk counts
```

## Using as an MCP Server (Claude Code, Cursor, Windsurf)

### Claude Code

Run the auto-installer:

```bash
srcmap mcp install
```

Or manually add to `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "srcmap": {
      "command": "srcmap",
      "args": ["mcp"]
    }
  }
}
```

### Cursor

Add to `.cursor/mcp.json` in your project:

```json
{
  "mcpServers": {
    "srcmap": {
      "command": "srcmap",
      "args": ["mcp"]
    }
  }
}
```

### Windsurf

Add to `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "srcmap": {
      "command": "srcmap",
      "args": ["mcp"]
    }
  }
}
```

### Available MCP Tools

Once connected, the agent has access to these tools:

| Tool | Description |
|------|-------------|
| `srcmap_lookup` | Look up a specific symbol — returns file, line range, params, return type |
| `srcmap_search_code` | Search symbols by name pattern |
| `srcmap_doc_map` | Get the root doc index for a source |
| `srcmap_doc_section` | Get a section's method listing |
| `srcmap_doc_lookup` | Get full method documentation |
| `srcmap_doc_concept` | Get concept documentation |
| `srcmap_doc_search` | Fuzzy search across all doc files |
| `srcmap_doc_gotchas` | Get known gotchas for a source |
| `srcmap_source_info` | Get source metadata and stats |
| `srcmap_process_chunk` | Process a pre-chunked doc block into structured output |
| `srcmap_process_status` | Check pending/processed/failed chunk counts |

## Using as a Standalone Agent

The agent mode gives you a terminal chat that can autonomously call all srcmap tools.

### Setup

```bash
export OPENROUTER_API_KEY=your-api-key
export OPENROUTER_MODEL=anthropic/claude-sonnet-4-20250514  # or any OpenRouter model
```

### Run

```bash
srcmap agent
```

### Example session

```
srcmap agent (model: anthropic/claude-sonnet-4-20250514)
Type /help for commands, /clear to reset, Ctrl+C to exit.

> What methods does Bot have for sending messages in telego?

Based on the search results, here are the Bot methods for sending messages:
1. Bot.SendMessage — methods.go:276-283
2. Bot.SendPhoto — methods.go:598-605
3. Bot.SendVideo — methods.go:937-944
4. Bot.SendDocument — methods.go:808-815
5. Bot.SendAudio — methods.go:706-713
...

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ↑ 22840 in   ↓ 620 out    $0.0031  this response
  Session: 26049 tokens              $0.0089 total
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Slash commands

| Command | Description |
|---------|-------------|
| `/clear` | Clear chat history and reset session cost |
| `/model` | Show current model |
| `/cost` | Show session cost breakdown |
| `/help` | Show available commands |

## CLI Reference

```
srcmap init                                    # scaffold .srcmap/ directory
srcmap fetch <packages...> [--global]          # fetch and index source code
srcmap docs add <source> --url <url>           # add docs from URL
srcmap docs add <source> --openapi <file>      # add docs from OpenAPI spec
srcmap docs add <source> --markdown <dir>      # add docs from markdown directory
srcmap lookup <source> <symbol>                # look up a specific symbol
srcmap search <source> <query>                 # search symbols by name
srcmap list                                    # list indexed sources
srcmap sources [--global]                      # list sources with details
srcmap update <source> [--full] [--all]        # re-index a source
srcmap check                                   # check for pending updates
srcmap link <source>                           # link global source to local project
srcmap mcp                                     # start MCP server (stdio)
srcmap mcp install                             # auto-install MCP config
srcmap agent                                   # start interactive terminal chat
```

## Supported Ecosystems

| Ecosystem | Fetch syntax | Lockfile detection |
|-----------|-------------|-------------------|
| npm | `srcmap fetch zod` | package-lock.json, yarn.lock, pnpm-lock.yaml |
| PyPI | `srcmap fetch pypi:requests` | requirements.txt, poetry.lock |
| Go modules | `srcmap fetch github.com/owner/repo` | go.sum |
| GitHub | `srcmap fetch owner/repo` | — |

## How Documentation Works

1. **Fetch** — `srcmap docs add` fetches raw content from a URL, OpenAPI spec, or markdown directory.
2. **Chunk** — A pure-Go pre-chunker splits content into token-bounded blocks (max 3000 tokens) with context headers including source name, section breadcrumb, and chunk position.
3. **Process** — An agent calls `srcmap_process_chunk` per chunk to classify it (method/concept/gotcha) and produce structured markdown with YAML frontmatter.
4. **Query** — MCP tools read the structured files and SQLite index to serve agent requests in under 10ms.

The doc hierarchy for each source:

```
.srcmap/docs/{source}/
├── source.yaml          — identity, version, doc origin
├── CHANGELOG.md         — auto-maintained update log
├── gotchas.md           — known pitfalls indexed by ID
├── index.md             — root map with sections
└── {section}/
    ├── section.md       — method listing with summaries
    ├── methods/
    │   └── {method}.md  — full method documentation
    └── concepts/
        └── {concept}.md — cross-cutting knowledge
```

Hand-written content inside `<!-- custom --> ... <!-- /custom -->` blocks is preserved across updates.

## License

MIT
