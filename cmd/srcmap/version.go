package main

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// version is set via -ldflags "-X main.version=vX.Y.Z" at release time.
// Otherwise we compose a human-readable version from VCS build info.
var version = ""

// resolvedVersion returns a short, human-friendly version string.
//
// Priority:
//  1. ldflag override (e.g. "v0.1.2") — used by release builds.
//  2. Clean module tag from `go install` (e.g. "v0.1.2").
//  3. "dev <short-sha>, <date>[, modified]" from VCS info (source builds).
//  4. Same format parsed out of a Go pseudo-version (go-install builds
//     before any tag exists: v0.0.0-YYYYMMDDHHMMSS-<12-sha>).
//  5. "dev" if nothing is available.
func resolvedVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Clean release tag (not a pseudo-version like v0.0.0-<ts>-<sha>).
	if v := info.Main.Version; v != "" && v != "(devel)" && !strings.HasPrefix(v, "v0.0.0-") {
		return v
	}

	// Source-tree build (go build / go install ./...): use VCS settings.
	var sha, date string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			sha = shortSHA(s.Value)
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				date = t.Format("2006-01-02")
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}

	// Module-mode install (go install <path>@latest) has no VCS settings,
	// but the pseudo-version carries the same data: v0.0.0-<ts>-<sha>.
	if sha == "" {
		if s, d := parsePseudoVersion(info.Main.Version); s != "" {
			sha, date = s, d
		}
	}

	if sha == "" {
		return "dev"
	}

	parts := []string{sha}
	if date != "" {
		parts = append(parts, date)
	}
	if modified {
		parts = append(parts, "modified")
	}
	return "dev " + strings.Join(parts, ", ")
}

func shortSHA(full string) string {
	if len(full) >= 7 {
		return full[:7]
	}
	return full
}

// parsePseudoVersion extracts the short SHA and commit date from a Go
// pseudo-version like "v0.0.0-20260424141206-221bba465b55". Returns
// empty strings on any mismatch so the caller can fall through.
func parsePseudoVersion(v string) (sha, date string) {
	if !strings.HasPrefix(v, "v0.0.0-") {
		return "", ""
	}
	parts := strings.Split(v, "-")
	if len(parts) < 3 {
		return "", ""
	}
	ts := parts[len(parts)-2]  // "20260424141206"
	raw := parts[len(parts)-1] // "221bba465b55"
	if t, err := time.Parse("20060102150405", ts); err == nil {
		date = t.Format("2006-01-02")
	}
	return shortSHA(raw), date
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the srcmap version and how to upgrade it",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("srcmap %s\n\n", resolvedVersion())
		fmt.Println("Upgrade with:")
		fmt.Println("  srcmap upgrade")
		fmt.Println("  # or: go install " + srcmapModulePath + "@latest")
		return nil
	},
}

func init() {
	rootCmd.Version = resolvedVersion()
	rootCmd.AddCommand(versionCmd)
}
