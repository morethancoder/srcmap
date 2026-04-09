package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the srcmap configuration.
type Config struct {
	// OpenRouterAPIKey is the API key for OpenRouter (agent mode).
	OpenRouterAPIKey string `yaml:"openrouter_api_key,omitempty"`
	// Model is the OpenRouter model ID for agent mode.
	Model string `yaml:"model,omitempty"`
	// GlobalPath is the path to the global srcmap directory.
	GlobalPath string `yaml:"global_path,omitempty"`
}

// DefaultGlobalPath returns the default global srcmap directory (~/.srcmap/).
func DefaultGlobalPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".srcmap")
	}
	return filepath.Join(home, ".srcmap")
}

// Load reads config from the given path. Returns defaults if the file doesn't exist.
func Load(path string) (*Config, error) {
	cfg := &Config{
		GlobalPath: DefaultGlobalPath(),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.GlobalPath == "" {
		cfg.GlobalPath = DefaultGlobalPath()
	}

	return cfg, nil
}

// Save writes the config to the given path, creating directories as needed.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

// ConfigPath returns the default config file path for the given scope.
func ConfigPath(global bool) string {
	if global {
		return filepath.Join(DefaultGlobalPath(), "config.yaml")
	}
	return filepath.Join(".srcmap", "config.yaml")
}
