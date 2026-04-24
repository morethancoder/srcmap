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
//  3. "dev <short-sha> (<date>[, modified])" composed from VCS info.
//  4. "dev" if nothing is available.
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

	var sha, date string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				sha = s.Value[:7]
			} else {
				sha = s.Value
			}
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				date = t.Format("2006-01-02")
			}
		case "vcs.modified":
			modified = s.Value == "true"
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
	return fmt.Sprintf("dev %s", strings.Join(parts, ", "))
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the srcmap version and how to update it",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("srcmap %s\n\n", resolvedVersion())
		fmt.Println("Update with:")
		fmt.Println("  srcmap self-update")
		fmt.Println("  # or: go install " + srcmapModulePath + "@latest")
		return nil
	},
}

func init() {
	rootCmd.Version = resolvedVersion()
	rootCmd.AddCommand(versionCmd)
}
