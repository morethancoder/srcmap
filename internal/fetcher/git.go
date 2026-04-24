package fetcher

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/morethancoder/srcmap/internal/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// DefaultCloneTimeout caps a single clone attempt.
const DefaultCloneTimeout = 3 * time.Minute

// GitFetcher clones repositories using go-git.
type GitFetcher struct {
	// Timeout per clone attempt; 0 → DefaultCloneTimeout.
	Timeout time.Duration
	// Progress receives go-git's progress output (git pack phases). Defaults
	// to a zerolog-backed writer so users see clone progress.
	Progress io.Writer
}

// Fetch clones the repository at the given version tag into destPath.
func (f *GitFetcher) Fetch(ctx context.Context, repoURL, version, destPath string) error {
	timeout := f.Timeout
	if timeout == 0 {
		timeout = DefaultCloneTimeout
	}
	progress := f.Progress
	if progress == nil {
		progress = defaultGitProgress
	}

	tagRefs := versionToTagRefs(version)

	var lastErr error
	for _, ref := range tagRefs {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		t := logging.Stage("clone", "repo", repoURL, "ref", string(ref), "timeout", timeout)

		_, err := git.PlainCloneContext(attemptCtx, destPath, false, &git.CloneOptions{
			URL:           repoURL,
			ReferenceName: ref,
			Depth:         1,
			SingleBranch:  true,
			Progress:      progress,
		})
		cancel()

		if err == nil {
			t.Done("repo", repoURL, "ref", string(ref))
			return nil
		}
		t.Warn(err, "attempt failed", "repo", repoURL, "ref", string(ref))
		lastErr = err
	}

	return fmt.Errorf("cloning %s at version %s: %w", repoURL, version, lastErr)
}

var defaultGitProgress io.Writer = &logWriter{prefix: "git"}

type logWriter struct{ prefix string }

func (w *logWriter) Write(p []byte) (int, error) {
	// Skip the string alloc when trace is disabled; go-git emits one Write
	// per progress line, so this is a hot loop during clone.
	if zerolog.GlobalLevel() > zerolog.TraceLevel {
		return len(p), nil
	}
	msg := strings.TrimRight(string(p), "\r\n")
	if msg != "" {
		log.Trace().Str("source", w.prefix).Msg(msg)
	}
	return len(p), nil
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
