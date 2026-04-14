package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/morethancoder/srcmap/internal/agent"
	"github.com/morethancoder/srcmap/internal/config"
	"github.com/morethancoder/srcmap/internal/docfetcher"
	"github.com/morethancoder/srcmap/internal/fetcher"
	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/internal/mcp"
	"github.com/morethancoder/srcmap/internal/parser"
	"github.com/morethancoder/srcmap/pkg/fileformat"
	"github.com/spf13/cobra"
)

func openDB() (*index.DB, error) {
	os.MkdirAll(".srcmap", 0o755)
	return index.Open(filepath.Join(".srcmap", "index.db"))
}

func ensureSrcmapDir() {
	dirs := []string{
		filepath.Join(".srcmap", "sources"),
		filepath.Join(".srcmap", "docs"),
	}
	for _, dir := range dirs {
		os.MkdirAll(dir, 0o755)
	}
	addToRootGitignore(".srcmap/")
}

func runFetch(cmd *cobra.Command, args []string) error {
	ensureSrcmapDir()
	global, _ := cmd.Flags().GetBool("global")

	cfg, err := config.Load(config.ConfigPath(false))
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	orch := fetcher.NewOrchestrator(cwd, cfg.GlobalPath)

	var requests []fetcher.FetchRequest
	for _, arg := range args {
		requests = append(requests, fetcher.ParsePackageName(arg, global))
	}

	ctx := context.Background()
	results := orch.FetchAll(ctx, requests)

	// Open DB for indexing
	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	reg := parser.NewRegistry()
	handler := mcp.NewToolHandler(db, ".srcmap")

	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s: %v\n", r.Request.Name, r.Err)
			continue
		}

		fmt.Printf("✓ %s@%s fetched to %s\n", r.Source.Name, r.Source.Version, r.Source.Path)

		// Register source in DB
		now := time.Now().UTC().Format(time.RFC3339)
		err := db.InsertSource(&index.SourceRecord{
			ID:          r.Source.Name,
			Name:        r.Source.Name,
			Version:     r.Source.Version,
			RepoURL:     r.Source.RepoURL,
			Path:        r.Source.Path,
			Global:      r.Source.Global,
			LastUpdated: now,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to register source: %v\n", err)
			continue
		}

		// Parse and index symbols
		symbols, err := reg.ParseDirectory(r.Source.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to parse source: %v\n", err)
			continue
		}

		indexed := 0
		for i := range symbols {
			symbols[i].SourceID = r.Source.Name
			if _, err := db.InsertSymbol(&symbols[i]); err != nil {
				continue
			}
			indexed++
		}
		fmt.Printf("  indexed %d symbols\n", indexed)

		summary, derr := handler.AutoIngestLocalDocs(ctx, r.Source.Name, r.Source.Path)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "  docs: %v\n", derr)
		} else {
			for _, ln := range strings.Split(summary, "\n") {
				fmt.Printf("  %s\n", ln)
			}
		}
	}

	return nil
}

func runDocsAdd(cmd *cobra.Command, args []string) error {
	ensureSrcmapDir()
	sourceName := args[0]

	urlFlag, _ := cmd.Flags().GetString("url")
	openapiFlag, _ := cmd.Flags().GetString("openapi")
	markdownFlag, _ := cmd.Flags().GetString("markdown")

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Register source if not exists
	db.InsertSource(&index.SourceRecord{
		ID:          sourceName,
		Name:        sourceName,
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
	})

	var pages []docfetcher.RawPage
	var originURL string

	switch {
	case openapiFlag != "":
		content, err := os.ReadFile(openapiFlag)
		if err != nil {
			return fmt.Errorf("reading OpenAPI spec: %w", err)
		}
		p := &docfetcher.OpenAPIParser{}
		pages, err = p.Parse(content)
		if err != nil {
			return fmt.Errorf("parsing OpenAPI spec: %w", err)
		}
		fmt.Printf("Parsed %d operations from OpenAPI spec\n", len(pages))

	case markdownFlag != "":
		w := &docfetcher.MarkdownWalker{}
		pages, err = w.Walk(markdownFlag)
		if err != nil {
			return fmt.Errorf("walking markdown dir: %w", err)
		}
		fmt.Printf("Found %d markdown files\n", len(pages))

	case urlFlag != "":
		f := &docfetcher.SingleFileFetcher{}
		page, err := f.Fetch(context.Background(), urlFlag)
		if err != nil {
			return fmt.Errorf("fetching URL: %w", err)
		}
		pages = []docfetcher.RawPage{*page}
		originURL = urlFlag
		fmt.Printf("Fetched %d bytes from %s\n", len(page.Content), urlFlag)

	default:
		return fmt.Errorf("specify --url, --openapi, or --markdown")
	}

	// Chunk the content
	chunker := &docfetcher.DefaultChunker{}
	var chunks []docfetcher.Chunk
	if originURL != "" {
		chunks, err = chunker.ChunkWithOrigin(sourceName, originURL, pages)
	} else {
		chunks, err = chunker.Chunk(sourceName, pages)
	}
	if err != nil {
		return fmt.Errorf("chunking: %w", err)
	}

	// Store chunks in DB
	for i := range chunks {
		id, err := db.InsertChunk(&chunks[i])
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to insert chunk %d: %v\n", i, err)
			continue
		}
		chunks[i].ID = id
	}

	// Write source.yaml
	srcmapDir := ".srcmap"
	sy := &fileformat.SourceYAML{
		Name: sourceName,
	}
	if originURL != "" {
		sy.DocOrigin = &fileformat.DocOrigin{
			URL:          originURL,
			ContentType:  "single-markdown",
			DiscoveredAt: time.Now().UTC().Format(time.RFC3339),
			Validated:    true,
		}
	}
	hb := fileformat.NewHierarchyBuilder(srcmapDir, sourceName)
	hb.EnsureStructure()
	fileformat.WriteSourceYAML(filepath.Join(srcmapDir, "docs", sourceName, "source.yaml"), sy)

	fmt.Printf("✓ %d chunks stored (status: pending) for %s\n", len(chunks), sourceName)
	fmt.Printf("  Run agent to process chunks: srcmap agent\n")

	return nil
}

func runSearch(cmd *cobra.Command, args []string) error {
	sourceID := args[0]
	query := args[1]

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	symbols, err := db.SearchSymbols(sourceID, query)
	if err != nil {
		return fmt.Errorf("searching: %w", err)
	}

	if len(symbols) == 0 {
		fmt.Println("No symbols found.")
		return nil
	}

	for _, s := range symbols {
		fmt.Printf("%s (%s) — %s:%d-%d\n", s.Name, s.Kind, s.FilePath, s.StartLine, s.EndLine)
	}
	return nil
}

func runLookup(cmd *cobra.Command, args []string) error {
	sourceID := args[0]
	symbolName := args[1]

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	sym, err := db.LookupSymbol(sourceID, symbolName)
	if err != nil {
		return fmt.Errorf("symbol not found: %w", err)
	}

	fmt.Printf("%s (%s)\n", sym.Name, sym.Kind)
	fmt.Printf("  file: %s:%d-%d\n", sym.FilePath, sym.StartLine, sym.EndLine)
	if sym.Parameters != "" {
		fmt.Printf("  params: %s\n", sym.Parameters)
	}
	if sym.ReturnType != "" {
		fmt.Printf("  returns: %s\n", sym.ReturnType)
	}
	if sym.ParentScope != "" {
		fmt.Printf("  scope: %s\n", sym.ParentScope)
	}

	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	sources, err := db.ListSources(false)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}

	if len(sources) == 0 {
		fmt.Println("No sources indexed. Run 'srcmap fetch <package>' to get started.")
		return nil
	}

	for _, s := range sources {
		scope := "local"
		if s.Global {
			scope = "global"
		}
		fmt.Printf("  %s@%s [%s]\n", s.Name, s.Version, scope)
	}

	return nil
}

func runSources(cmd *cobra.Command, args []string) error {
	globalOnly, _ := cmd.Flags().GetBool("global")

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	sources, err := db.ListSources(globalOnly)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}

	if len(sources) == 0 {
		fmt.Println("No sources found.")
		return nil
	}

	for _, s := range sources {
		scope := "local"
		if s.Global {
			scope = "global"
		}
		fmt.Printf("%-30s %-12s %-8s %s\n", s.Name, s.Version, scope, s.LastUpdated)
	}

	return nil
}

func runMCP(cmd *cobra.Command, args []string) error {
	ensureSrcmapDir()

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	cwd, _ := os.Getwd()
	cfg, _ := config.Load(config.ConfigPath(true))
	globalPath := ""
	if cfg != nil {
		globalPath = cfg.GlobalPath
	}
	if globalPath == "" {
		globalPath = config.DefaultGlobalPath()
	}

	handler := mcp.NewToolHandler(db, ".srcmap")
	handler.Orchestrator = fetcher.NewOrchestrator(cwd, globalPath)
	handler.ParserRegistry = parser.NewRegistry()

	server := mcp.NewStdioServer(handler, os.Stdin, os.Stdout)
	return server.Serve(context.Background())
}

func runAgent(cmd *cobra.Command, args []string) error {
	ensureSrcmapDir()

	// Try env var first, then config file
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	cfgPath := config.ConfigPath(true)
	cfg, _ := config.Load(cfgPath)

	if apiKey == "" && cfg != nil {
		apiKey = cfg.OpenRouterAPIKey
	}

	if apiKey == "" {
		fmt.Println("No OpenRouter API key found.")
		fmt.Print("Enter your OpenRouter API key: ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("no API key provided")
		}
		apiKey = strings.TrimSpace(scanner.Text())
		if apiKey == "" {
			return fmt.Errorf("no API key provided")
		}

		// Save to global config for future use
		if cfg == nil {
			cfg = &config.Config{}
		}
		cfg.OpenRouterAPIKey = apiKey
		if err := cfg.Save(cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save API key to config: %v\n", err)
		} else {
			fmt.Printf("API key saved to %s\n\n", cfgPath)
		}
	}

	modelID := os.Getenv("OPENROUTER_MODEL")
	if modelID == "" && cfg != nil && cfg.Model != "" {
		modelID = cfg.Model
	}
	if modelID == "" {
		modelID = "minimax/minimax-m2.5"
	}

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	cwd, _ := os.Getwd()
	localCfg, _ := config.Load(config.ConfigPath(false))
	globalPath := cfg.GlobalPath
	if globalPath == "" && localCfg != nil {
		globalPath = localCfg.GlobalPath
	}
	if globalPath == "" {
		globalPath = config.DefaultGlobalPath()
	}

	handler := mcp.NewToolHandler(db, ".srcmap")
	handler.Orchestrator = fetcher.NewOrchestrator(cwd, globalPath)
	handler.ParserRegistry = parser.NewRegistry()

	client := agent.NewOpenRouterClient(apiKey)
	costTracker := agent.NewCostTracker(0, 0)
	chat := agent.NewChat(client, handler, modelID, costTracker)
	chat.ConfigPath = cfgPath

	return chat.Run(context.Background())
}

func runMCPInstall(cmd *cobra.Command, args []string) error {
	target := mcp.DetectTarget()
	if err := mcp.Install(target); err != nil {
		return fmt.Errorf("installing MCP config: %w", err)
	}
	fmt.Printf("✓ MCP config written for %s\n", target)
	return nil
}

func runUpdate(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	full, _ := cmd.Flags().GetBool("full")

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	var sources []index.SourceRecord
	if all {
		sources, err = db.ListSources(false)
		if err != nil {
			return fmt.Errorf("listing sources: %w", err)
		}
	} else if len(args) > 0 {
		src, err := db.GetSource(args[0])
		if err != nil {
			return fmt.Errorf("source %q: %w", args[0], err)
		}
		sources = []index.SourceRecord{*src}
	} else {
		return fmt.Errorf("specify a source name or use --all")
	}

	reg := parser.NewRegistry()
	for _, src := range sources {
		if src.Path == "" {
			fmt.Fprintf(os.Stderr, "⚠ %s: no source path stored, skipping code re-index\n", src.Name)
			continue
		}

		if full {
			fmt.Printf("Re-indexing %s (full)...\n", src.Name)
		} else {
			fmt.Printf("Updating %s...\n", src.Name)
		}

		symbols, err := reg.ParseDirectory(src.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ %s: parse error: %v\n", src.Name, err)
			continue
		}

		indexed := 0
		for i := range symbols {
			symbols[i].SourceID = src.ID
			if _, err := db.InsertSymbol(&symbols[i]); err != nil {
				continue
			}
			indexed++
		}

		now := time.Now().UTC().Format(time.RFC3339)
		src.LastUpdated = now
		db.InsertSource(&src)

		fmt.Printf("✓ %s: re-indexed %d symbols\n", src.Name, indexed)
	}

	return nil
}

func runCheck(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	sources, err := db.ListSources(false)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}

	if len(sources) == 0 {
		fmt.Println("No sources to check.")
		return nil
	}

	for _, src := range sources {
		// Check for pending chunks
		pending, processed, failed, err := db.ChunkCounts(src.ID)
		if err != nil {
			continue
		}

		status := "✓ up to date"
		if pending > 0 {
			status = fmt.Sprintf("⚠ %d pending chunks", pending)
		}
		if failed > 0 {
			status = fmt.Sprintf("✗ %d failed chunks", failed)
		}

		fmt.Printf("%-30s %s (processed: %d)\n", src.Name, status, processed)
	}

	return nil
}

func runLink(cmd *cobra.Command, args []string) error {
	sourceID := args[0]

	cfg, err := config.Load(config.ConfigPath(false))
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Check global source exists
	globalDBPath := filepath.Join(cfg.GlobalPath, "index.db")
	globalDB, err := index.Open(globalDBPath)
	if err != nil {
		return fmt.Errorf("opening global database: %w", err)
	}

	globalSrc, err := globalDB.GetSource(sourceID)
	_ = globalDB.Close()
	if err != nil {
		return fmt.Errorf("source %q not found in global scope: %w", sourceID, err)
	}
	if globalSrc == nil {
		return fmt.Errorf("source %q returned nil from global scope", sourceID)
	}

	// Link into local DB
	ensureSrcmapDir()
	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening local database: %w", err)
	}
	defer db.Close()

	localSrc := *globalSrc
	localSrc.Global = false
	if err := db.InsertSource(&localSrc); err != nil {
		return fmt.Errorf("linking source: %w", err)
	}

	fmt.Printf("✓ Linked %s@%s from global scope\n", sourceID, globalSrc.Version)
	return nil
}

func runInit(cmd *cobra.Command, args []string) error {
	dirs := []string{
		filepath.Join(".srcmap", "sources"),
		filepath.Join(".srcmap", "docs"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	// Create index.db
	db, err := openDB()
	if err != nil {
		return fmt.Errorf("creating database: %w", err)
	}
	db.Close()

	// Write internal .gitignore for .srcmap/
	internalGitignore := filepath.Join(".srcmap", ".gitignore")
	if _, err := os.Stat(internalGitignore); os.IsNotExist(err) {
		os.WriteFile(internalGitignore, []byte("index.db\nsources/\n"), 0o644)
	}

	// Auto-add .srcmap to project root .gitignore
	addToRootGitignore(".srcmap/")

	fmt.Println("✓ .srcmap/ directory initialized")
	return nil
}

// addToRootGitignore adds an entry to the project root .gitignore if not already present.
func addToRootGitignore(entry string) {
	const gitignorePath = ".gitignore"

	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return
	}

	// Check if entry already present
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
	}

	// Append entry
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	// Add newline before entry if file doesn't end with one
	if len(content) > 0 && content[len(content)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(entry + "\n")
}
