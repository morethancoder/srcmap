package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// InstallTarget represents an MCP client to configure.
type InstallTarget string

const (
	TargetClaudeCode InstallTarget = "claude-code"
	TargetCursor     InstallTarget = "cursor"
	TargetWindsurf   InstallTarget = "windsurf"
)

// InstallScope controls whether the MCP config is written at the user level
// (available in every project) or at the current project level.
type InstallScope string

const (
	ScopeUser    InstallScope = "user"
	ScopeProject InstallScope = "project"
)

// Install writes the MCP server configuration for the given target and scope.
// Returns the path that was written.
func Install(target InstallTarget, scope InstallScope) (string, error) {
	if scope == "" {
		scope = ScopeUser
	}
	if scope != ScopeUser && scope != ScopeProject {
		return "", fmt.Errorf("unknown scope: %s (expected %q or %q)", scope, ScopeUser, ScopeProject)
	}

	binaryPath, err := findBinary()
	if err != nil {
		return "", fmt.Errorf("finding srcmap binary: %w", err)
	}

	path, err := configPathFor(target, scope)
	if err != nil {
		return "", err
	}
	if err := writeMCPConfig(path, binaryPath); err != nil {
		return "", err
	}
	return path, nil
}

// DetectTarget tries to detect which MCP client is available.
func DetectTarget() InstallTarget {
	home, _ := os.UserHomeDir()

	// Check for Claude Code
	if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
		return TargetClaudeCode
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
		return TargetClaudeCode
	}
	// Check for Cursor
	if _, err := os.Stat(filepath.Join(home, ".cursor")); err == nil {
		return TargetCursor
	}
	// Check for Windsurf
	if _, err := os.Stat(filepath.Join(home, ".codeium")); err == nil {
		return TargetWindsurf
	}
	return TargetClaudeCode // default
}

func findBinary() (string, error) {
	// Try to find srcmap in PATH
	path, err := exec.LookPath("srcmap")
	if err == nil {
		return path, nil
	}
	// Fall back to go build output
	if runtime.GOOS == "windows" {
		return "srcmap.exe", nil
	}
	return "srcmap", nil
}

func configPathFor(target InstallTarget, scope InstallScope) (string, error) {
	home, _ := os.UserHomeDir()

	switch target {
	case TargetClaudeCode:
		switch scope {
		case ScopeUser:
			// Claude Code reads user-scope MCP servers from ~/.claude.json
			// top-level "mcpServers" key, NOT ~/.claude/settings.json.
			return filepath.Join(home, ".claude.json"), nil
		case ScopeProject:
			return ".mcp.json", nil
		}
	case TargetCursor:
		switch scope {
		case ScopeUser:
			return filepath.Join(home, ".cursor", "mcp.json"), nil
		case ScopeProject:
			return filepath.Join(".cursor", "mcp.json"), nil
		}
	case TargetWindsurf:
		if scope == ScopeProject {
			return "", fmt.Errorf("windsurf does not support project-scope MCP config; use --scope user")
		}
		return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), nil
	}
	return "", fmt.Errorf("unknown target: %s", target)
}

// writeMCPConfig merges the srcmap entry into the target JSON file's
// "mcpServers" map, preserving every other top-level field. This matters
// especially for ~/.claude.json, which holds unrelated Claude Code state.
func writeMCPConfig(configPath, binaryPath string) error {
	raw := map[string]interface{}{}
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing existing config %s: %w", configPath, err)
		}
	}

	servers, _ := raw["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	servers["srcmap"] = map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"mcp"},
	}
	raw["mcpServers"] = servers

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0o644)
}
