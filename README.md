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
srcmap outdated       # query upstream registries for newer versions
```

### Update a source

```bash
srcmap update zod                  # re-parse whatever is on disk
srcmap update zod --refetch        # pull the latest upstream version and re-index
srcmap update --all --refetch      # update every indexed source to latest
```

Exactly the same flow is exposed via MCP as `srcmap_update_source` and `srcmap_outdated`, so an agent can do "update source X" in one tool call.

## Using as an MCP Server (Claude Code, Cursor, Windsurf)

First make sure the `srcmap` binary is on your `$PATH` — if you haven't already, run the `go install` command from [Installation](#installation). The MCP config below points to the `srcmap` command by name, so it has to be resolvable from the shell your AI tool launches.

The auto-installer detects your AI tool (Claude Code, Cursor, or Windsurf) and writes the correct config file:

```bash
# Install at user scope — available in every project (default)
srcmap mcp install

# Install at project scope — only for the current directory
srcmap mcp install --scope project

# Force a specific tool instead of auto-detecting
srcmap mcp install --target claude-code
srcmap mcp install --target cursor --scope project
```

Scopes per tool:

| Tool        | `--scope user` (default)                       | `--scope project`          |
| ----------- | ---------------------------------------------- | -------------------------- |
| Claude Code | `~/.claude/settings.json`                      | `./.mcp.json`              |
| Cursor      | `~/.cursor/mcp.json`                           | `./.cursor/mcp.json`       |
| Windsurf    | `~/.codeium/windsurf/mcp_config.json`          | _not supported_            |

Restart your AI tool after running the installer, then verify srcmap is connected (in Claude Code, run `/mcp`).

### Manual config

If you'd rather edit the config file yourself, add this block (paths are the ones in the table above):

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

Once connected, the agent has access to these tools. Descriptions encode the
intended workflow so the agent picks the right tool without reading the code.

**Discovery** (always available)

| Tool | Description |
|------|-------------|
| `srcmap_list_sources` | List every indexed source — start here when unsure what is available |
| `srcmap_source_info` | Metadata + doc-ingest status for one source |
| `srcmap_find` | **Preferred search** — exact name match + FTS5 ranked snippets in one call |
| `srcmap_lookup` | Exact-name symbol lookup (file, line range, params, return type) |
| `srcmap_search_code` | Substring search over parsed code symbols |
| `srcmap_doc_map` | Root `index.md` — the top-level doc map |
| `srcmap_doc_section` | Listing of methods + concepts in a section |
| `srcmap_doc_lookup` | Full method documentation |
| `srcmap_doc_concept` | Full concept documentation |
| `srcmap_doc_search` | FTS5 ranked doc search (prefer `srcmap_find`) |
| `srcmap_doc_gotchas` | Known footguns and breaking changes |
| `srcmap_process_chunk` / `srcmap_process_all` / `srcmap_process_status` | Finer-grained chunk processing controls |
| `srcmap_delete_source` | Permanently remove a source (destructive) |

**Write / update** (available only when the server runs from a project directory)

| Tool | Description |
|------|-------------|
| `srcmap_fetch` | Clone a package, parse symbols, auto-ingest local docs |
| `srcmap_docs_add` | One-call ingestion of a docs URL — auto-detects llms.txt / OpenAPI / HTML |
| `srcmap_ingest_local_docs` | Offline fallback — ingest only README + docs/ folder |
| `srcmap_update_source` | Re-fetch latest upstream version, re-parse, re-ingest (handles "update X" in one call) |
| `srcmap_outdated` | Check every source against its registry and report which are behind |

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
srcmap update <source> [--refetch] [--full] [--all]  # refresh a source (optionally pull latest from upstream)
srcmap outdated                                      # show which sources are behind upstream
srcmap check                                         # show pending/processed/failed chunk counts
srcmap link <source>                           # link global source to local project
srcmap mcp                                     # start MCP server (stdio)
srcmap mcp install [--scope user|project]      # auto-install MCP config (default: user)
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
