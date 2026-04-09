package config_test

import (
	"path/filepath"
	"testing"

	"github.com/morethancoder/srcmap/internal/config"
)

func TestConfigLoadEmpty(t *testing.T) {
	// Load from a nonexistent path — should return sane defaults
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GlobalPath == "" {
		t.Error("expected non-empty GlobalPath default")
	}
	if cfg.OpenRouterAPIKey != "" {
		t.Error("expected empty OpenRouterAPIKey by default")
	}
	if cfg.Model != "" {
		t.Error("expected empty Model by default")
	}
}

func TestConfigRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &config.Config{
		OpenRouterAPIKey: "test-key",
		Model:            "anthropic/claude-3.5-sonnet",
		GlobalPath:       "/custom/path",
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.OpenRouterAPIKey != original.OpenRouterAPIKey {
		t.Errorf("api key: got %q, want %q", loaded.OpenRouterAPIKey, original.OpenRouterAPIKey)
	}
	if loaded.Model != original.Model {
		t.Errorf("model: got %q, want %q", loaded.Model, original.Model)
	}
	if loaded.GlobalPath != original.GlobalPath {
		t.Errorf("global path: got %q, want %q", loaded.GlobalPath, original.GlobalPath)
	}
}
