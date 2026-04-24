package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is set via -ldflags "-X main.version=vX.Y.Z" at release time.
// When built with `go install`, runtime/debug.ReadBuildInfo() supplies the
// module version automatically, so this fallback is only hit for `go run`
// or a plain `go build` from source.
var version = "dev"

func resolvedVersion() string {
	if version != "dev" && version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the srcmap version and how to update it",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("srcmap %s\n\n", resolvedVersion())
		fmt.Println("Update with:")
		fmt.Println("  go install github.com/morethancoder/srcmap/cmd/srcmap@latest")
		return nil
	},
}

func init() {
	rootCmd.Version = resolvedVersion()
	rootCmd.AddCommand(versionCmd)
}
