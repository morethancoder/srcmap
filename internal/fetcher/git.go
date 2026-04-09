package fetcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// GitFetcher clones repositories using go-git.
type GitFetcher struct{}

// Fetch clones the repository at the given version tag into destPath.
func (f *GitFetcher) Fetch(ctx context.Context, repoURL, version, destPath string) error {
	// Try common tag formats
	tagRefs := versionToTagRefs(version)

	var lastErr error
	for _, ref := range tagRefs {
		_, err := git.PlainCloneContext(ctx, destPath, false, &git.CloneOptions{
			URL:           repoURL,
			ReferenceName: ref,
			Depth:         1,
			SingleBranch:  true,
		})
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("cloning %s at version %s: %w", repoURL, version, lastErr)
}

// versionToTagRefs generates common tag reference names for a version.
func versionToTagRefs(version string) []plumbing.ReferenceName {
	if version == "" || strings.EqualFold(version, "HEAD") {
		return []plumbing.ReferenceName{plumbing.HEAD}
	}
	version = strings.TrimPrefix(version, "v")
	return []plumbing.ReferenceName{
		plumbing.NewTagReferenceName("v" + version),
		plumbing.NewTagReferenceName(version),
	}
}
