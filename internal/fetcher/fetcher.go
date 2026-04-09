package fetcher

import "context"

// RegistryResult holds the resolved metadata from a package registry.
type RegistryResult struct {
	Name    string // package name
	RepoURL string // git clone URL
	Version string // resolved version
}

// Registry resolves a package name to a git repository URL and version.
type Registry interface {
	// Resolve looks up a package and returns its repository URL and version.
	Resolve(ctx context.Context, name string) (*RegistryResult, error)
}

// Source represents a fetched source stored on disk.
type Source struct {
	Name    string
	Version string
	RepoURL string
	Path    string // local path where source is stored
	Global  bool
}

// Fetcher clones and stores source code for a package.
type Fetcher interface {
	// Fetch clones the repository at the given version into the store.
	Fetch(ctx context.Context, repoURL, version, destPath string) error
}
