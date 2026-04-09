package main

import (
	"strings"
	"testing"
)

func TestCLICommandsExist(t *testing.T) {
	expected := []string{
		"fetch", "docs", "lookup", "search", "list",
		"sources", "update", "check", "mcp", "agent", "init", "link",
	}

	// Get all registered command names
	var names []string
	for _, cmd := range rootCmd.Commands() {
		names = append(names, cmd.Name())
	}
	joined := strings.Join(names, " ")

	for _, want := range expected {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("command %q not registered (have: %s)", want, joined)
		}
	}
}

func TestDocsSubcommands(t *testing.T) {
	var names []string
	for _, cmd := range docsCmd.Commands() {
		names = append(names, cmd.Name())
	}
	found := false
	for _, name := range names {
		if name == "add" {
			found = true
			break
		}
	}
	if !found {
		t.Error("docs add subcommand not registered")
	}
}

func TestMCPSubcommands(t *testing.T) {
	var names []string
	for _, cmd := range mcpCmd.Commands() {
		names = append(names, cmd.Name())
	}
	found := false
	for _, name := range names {
		if name == "install" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mcp install subcommand not registered")
	}
}
