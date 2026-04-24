package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "srcmap",
	Short: "Structured context about any dependency or API for AI coding agents",
	Long:  "srcmap maintains a local, versioned, incrementally-updated knowledge base per dependency — combining real source code + structured docs + semantic concepts — queryable with surgical precision.",
}

var fetchCmd = &cobra.Command{
	Use:   "fetch [packages...]",
	Short: "Fetch and index source code for one or more packages",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runFetch,
}

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Manage documentation sources",
}

var docsAddCmd = &cobra.Command{
	Use:   "add [source]",
	Short: "Add documentation for a source",
	Args:  cobra.ExactArgs(1),
	RunE:  runDocsAdd,
}

var lookupCmd = &cobra.Command{
	Use:   "lookup [source] [symbol]",
	Short: "Look up a specific symbol in a source",
	Args:  cobra.ExactArgs(2),
	RunE:  runLookup,
}

var searchCmd = &cobra.Command{
	Use:   "search [source] [query]",
	Short: "Search symbols by name, tag, or kind",
	Args:  cobra.ExactArgs(2),
	RunE:  runSearch,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all indexed sources in the current project",
	RunE:  runList,
}

var sourcesCmd = &cobra.Command{
	Use:   "sources",
	Short: "List all sources with version, last_updated, and staleness",
	RunE:  runSources,
}

var updateCmd = &cobra.Command{
	Use:   "update [source]",
	Short: "Incrementally update a source (or all with --all)",
	RunE:  runUpdate,
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check all sources for available updates without modifying anything",
	RunE:  runCheck,
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server (JSON-RPC 2.0 over stdio)",
	RunE:  runMCP,
}

var mcpInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Auto-detect and write MCP config for Claude Code, Cursor, or Windsurf",
	Long: `Write the srcmap MCP server config for an AI coding tool.

By default, installs at user scope so srcmap is available in every project.
Use --scope project to install only for the current directory.

Examples:
  srcmap mcp install                            # user scope, auto-detected tool
  srcmap mcp install --scope project            # current project only
  srcmap mcp install --target claude-code       # force Claude Code
  srcmap mcp install --target cursor --scope project`,
	RunE: runMCPInstall,
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Start the interactive terminal chat powered by OpenRouter",
	RunE:  runAgent,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold .srcmap/ directory in the current project",
	RunE:  runInit,
}

var linkCmd = &cobra.Command{
	Use:   "link [source]",
	Short: "Link an existing global source into the current project",
	Args:  cobra.ExactArgs(1),
	RunE:  runLink,
}

func init() {
	// fetch flags
	fetchCmd.Flags().Bool("global", false, "Store in global ~/.srcmap/ scope")

	// docs subcommands and flags
	docsAddCmd.Flags().String("url", "", "URL to crawl for documentation")
	docsAddCmd.Flags().String("openapi", "", "Path to OpenAPI/Swagger spec file")
	docsAddCmd.Flags().String("markdown", "", "Path to local markdown docs directory")
	docsCmd.AddCommand(docsAddCmd)

	// update flags
	updateCmd.Flags().Bool("all", false, "Update all sources")
	updateCmd.Flags().Bool("full", false, "Force full re-crawl")

	// sources flags
	sourcesCmd.Flags().Bool("global", false, "List global sources")

	// mcp subcommands
	mcpInstallCmd.Flags().String("scope", "user", "Install scope: 'user' (every project) or 'project' (current directory only)")
	mcpInstallCmd.Flags().String("target", "auto", "Target tool: claude-code, cursor, windsurf, or auto")
	mcpCmd.AddCommand(mcpInstallCmd)

	// register all commands
	rootCmd.AddCommand(
		fetchCmd,
		docsCmd,
		lookupCmd,
		searchCmd,
		listCmd,
		sourcesCmd,
		updateCmd,
		checkCmd,
		mcpCmd,
		agentCmd,
		initCmd,
		linkCmd,
	)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
