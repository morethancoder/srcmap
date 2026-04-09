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

// Install writes the MCP server configuration for the given target.
func Install(target InstallTarget) error {
	binaryPath, err := findBinary()
	if err != nil {
		return fmt.Errorf("finding srcmap binary: %w", err)
	}

	switch target {
	case TargetClaudeCode:
		return installClaudeCode(binaryPath)
	case TargetCursor:
		return installCursor(binaryPath)
	case TargetWindsurf:
		return installWindsurf(binaryPath)
	default:
		return fmt.Errorf("unknown target: %s", target)
	}
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

func installClaudeCode(binaryPath string) error {
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".claude", "settings.json")
	return writeMCPConfig(configPath, binaryPath)
}

func installCursor(binaryPath string) error {
	configPath := filepath.Join(".cursor", "mcp.json")
	return writeMCPConfig(configPath, binaryPath)
}

func installWindsurf(binaryPath string) error {
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	return writeMCPConfig(configPath, binaryPath)
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
