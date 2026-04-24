package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// srcmapModulePath is the canonical import path users run `go install` against.
const srcmapModulePath = "github.com/morethancoder/srcmap/cmd/srcmap"

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Re-install srcmap from the latest GitHub release",
	Long: `Runs the Go toolchain to pull the latest published srcmap from GitHub:

    go install ` + srcmapModulePath + `@latest

Requires the Go toolchain on $PATH. The new binary lands in $GOBIN
(or $GOPATH/bin), which is where the shell picked up the current one.`,
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
		fmt.Println("✓ srcmap updated — run `srcmap version` to confirm")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
}
