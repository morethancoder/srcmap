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

type mcpConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func configPathFor(target InstallTarget, scope InstallScope) (string, error) {
	home, _ := os.UserHomeDir()

	switch target {
	case TargetClaudeCode:
		switch scope {
		case ScopeUser:
			return filepath.Join(home, ".claude", "settings.json"), nil
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

func writeMCPConfig(configPath, binaryPath string) error {
	// Read existing config if present
	var config mcpConfig
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &config) // best-effort parse of existing config
	}
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]mcpServerConfig)
	}

	config.MCPServers["srcmap"] = mcpServerConfig{
		Command: binaryPath,
		Args:    []string{"mcp"},
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0o644)
}
