package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// srcmapModulePath is the canonical import path users run `go install` against.
const srcmapModulePath = "github.com/morethancoder/srcmap/cmd/srcmap"

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the srcmap binary to the latest GitHub release",
	Long: `Pull the latest published srcmap from GitHub and re-install it:

    go install ` + srcmapModulePath + `@latest

Requires the Go toolchain on $PATH. The new binary lands in $GOBIN
(or $GOPATH/bin), which is where the shell picked up the current one.

Note: "srcmap update <source>" updates an indexed package's symbols and
docs — that's a different command. This one upgrades the srcmap tool
itself.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		goBin, err := exec.LookPath("go")
		if err != nil {
			return fmt.Errorf("go toolchain not found on PATH — install Go or run the command below by hand:\n  go install %s@latest", srcmapModulePath)
		}

		target := srcmapModulePath + "@latest"
		fmt.Printf("→ go install %s\n\n", target)

		run := exec.Command(goBin, "install", target)
		run.Stdout = os.Stdout
		run.Stderr = os.Stderr
		if err := run.Run(); err != nil {
			return fmt.Errorf("go install failed: %w", err)
		}

		fmt.Println()
		fmt.Println("✓ srcmap upgraded — run `srcmap version` to confirm")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
