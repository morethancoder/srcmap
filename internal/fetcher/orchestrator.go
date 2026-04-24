package fetcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/morethancoder/srcmap/internal/httpx"
	"github.com/morethancoder/srcmap/internal/logging"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// DefaultResolveTimeout caps registry metadata lookups.
const DefaultResolveTimeout = 20 * time.Second

// PackageType identifies the ecosystem of a package.
type PackageType string

const (
	PackageNPM    PackageType = "npm"
	PackagePyPI   PackageType = "pypi"
	PackageGoMod  PackageType = "go"
	PackageGitHub PackageType = "github"
)

// FetchRequest describes a package to fetch.
type FetchRequest struct {
	Name    string
	Type    PackageType
	Global  bool
	Version string // override version, empty = auto-detect
}

// FetchResult describes the outcome of a fetch.
type FetchResult struct {
	Request FetchRequest
	Source  Source
	Err     error
}

// Orchestrator coordinates parallel fetching of multiple packages.
type Orchestrator struct {
	Registries  map[PackageType]Registry
	GitFetcher  Fetcher
	ProjectDir  string
	GlobalDir   string
	Concurrency int
}

// NewOrchestrator creates an Orchestrator with default registries.
func NewOrchestrator(projectDir, globalDir string) *Orchestrator {
	client := httpx.Default()
	return &Orchestrator{
		Registries: map[PackageType]Registry{
			PackageNPM:   &NPMRegistry{Client: client},
			PackagePyPI:  &PyPIRegistry{Client: client},
			PackageGoMod: &GoModRegistry{Client: client},
		},
		GitFetcher:  &GitFetcher{},
		ProjectDir:  projectDir,
		GlobalDir:   globalDir,
		Concurrency: 4,
	}
}

// ParsePackageName parses "pypi:requests" or "github.com/owner/repo" into a FetchRequest.
func ParsePackageName(input string, global bool) FetchRequest {
	if strings.HasPrefix(input, "pypi:") {
		return FetchRequest{Name: strings.TrimPrefix(input, "pypi:"), Type: PackagePyPI, Global: global}
	}
	if strings.HasPrefix(input, "npm:") {
		return FetchRequest{Name: strings.TrimPrefix(input, "npm:"), Type: PackageNPM, Global: global}
	}
	if strings.HasPrefix(input, "crates:") {
		// Not implemented yet, but parsed
		return FetchRequest{Name: strings.TrimPrefix(input, "crates:"), Type: "crates", Global: global}
	}
	// Scoped npm packages (@scope/pkg) always contain '/' and may contain '.'
	// in the scope — route them to npm before the slash-based GitHub/Go-module
	// heuristic, which would otherwise misroute them.
	if strings.HasPrefix(input, "@") {
		return FetchRequest{Name: input, Type: PackageNPM, Global: global}
	}
	if strings.Contains(input, "/") {
		// Could be Go module or GitHub shorthand
		if strings.Contains(input, ".") {
			// Looks like a Go module path (github.com/owner/repo)
			return FetchRequest{Name: input, Type: PackageGoMod, Global: global}
		}
		// owner/repo shorthand
		return FetchRequest{Name: input, Type: PackageGitHub, Global: global}
	}
	// Default to npm
	return FetchRequest{Name: input, Type: PackageNPM, Global: global}
}

// LatestVersion resolves the latest upstream version for a package without
// cloning it. Returns (version, repoURL, error). Uses the registry for the
// ecosystem inferred from `req.Type`; GitHub refs always resolve to "HEAD".
func (o *Orchestrator) LatestVersion(ctx context.Context, req FetchRequest) (string, string, error) {
	if req.Type == PackageGitHub {
		return "HEAD", "https://github.com/" + req.Name, nil
	}
	reg, ok := o.Registries[req.Type]
	if !ok {
		return "", "", fmt.Errorf("unsupported package type: %s", req.Type)
	}
	resolveCtx, cancel := context.WithTimeout(ctx, DefaultResolveTimeout)
	defer cancel()
	res, err := reg.Resolve(resolveCtx, req.Name)
	if err != nil {
		return "", "", err
	}
	return res.Version, res.RepoURL, nil
}

// FetchAll fetches multiple packages concurrently.
func (o *Orchestrator) FetchAll(ctx context.Context, requests []FetchRequest) []FetchResult {
	results := make([]FetchResult, len(requests))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(o.Concurrency)

	for i, req := range requests {
		g.Go(func() error {
			t := logging.Stage("fetch", "pkg", req.Name, "type", string(req.Type), "global", req.Global)
			source, err := o.fetchOne(ctx, req)
			results[i] = FetchResult{Request: req, Err: err}
			if source != nil {
				results[i].Source = *source
			}
			if err != nil {
				t.Fail(err, "pkg", req.Name)
			} else {
				t.Done("pkg", req.Name, "version", source.Version, "path", source.Path)
			}
			return nil // keep fetching the rest on error
		})
	}

	g.Wait()
	return results
}

func (o *Orchestrator) fetchOne(ctx context.Context, req FetchRequest) (*Source, error) {
	var repoURL, version string

	switch req.Type {
	case PackageGitHub:
		repoURL = "https://github.com/" + req.Name
		version = req.Version
		if version == "" {
			version = "HEAD"
		}
	default:
		reg, ok := o.Registries[req.Type]
		if !ok {
			return nil, fmt.Errorf("unsupported package type: %s", req.Type)
		}

		resolveCtx, cancel := context.WithTimeout(ctx, DefaultResolveTimeout)
		t := logging.Stage("resolve", "pkg", req.Name, "type", string(req.Type))
		result, err := reg.Resolve(resolveCtx, req.Name)
		cancel()
		if err != nil {
			t.Fail(err, "pkg", req.Name)
			return nil, fmt.Errorf("resolving %s: %w", req.Name, err)
		}
		t.Done("pkg", req.Name, "repo", result.RepoURL, "version", result.Version)
		repoURL = result.RepoURL
		version = result.Version

		// Try lockfile override
		if req.Version != "" {
			version = req.Version
		} else if o.ProjectDir != "" {
			if lv, err := DetectVersion(o.ProjectDir, req.Name); err == nil {
				version = lv
			}
		}
	}

	destDir := o.destPath(req, version)

	// Skip if already fetched
	if _, err := os.Stat(destDir); err == nil {
		log.Info().Str("pkg", req.Name).Str("path", destDir).Msg("cache hit — skipping clone")
		return &Source{
			Name:    req.Name,
			Version: version,
			RepoURL: repoURL,
			Path:    destDir,
			Global:  req.Global,
		}, nil
	}

	if err := o.GitFetcher.Fetch(ctx, repoURL, version, destDir); err != nil {
		return nil, err
	}

	return &Source{
		Name:    req.Name,
		Version: version,
		RepoURL: repoURL,
		Path:    destDir,
		Global:  req.Global,
	}, nil
}

func (o *Orchestrator) destPath(req FetchRequest, version string) string {
	base := filepath.Join(o.ProjectDir, ".srcmap", "sources")
	if req.Global {
		base = filepath.Join(o.GlobalDir, "sources")
	}
	safeName := strings.ReplaceAll(req.Name, "/", string(filepath.Separator))
	return filepath.Join(base, safeName+"@"+version)
}
