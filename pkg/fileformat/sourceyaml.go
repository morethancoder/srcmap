package fileformat

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SourceYAML represents the source.yaml file for a documentation source.
type SourceYAML struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description,omitempty"`
	Version     string       `yaml:"version,omitempty"`
	DocOrigin   *DocOrigin   `yaml:"doc_origin,omitempty"`
	Update      *UpdateConfig `yaml:"update,omitempty"`
	Triggers    []string     `yaml:"triggers,omitempty"`
	Stats       SourceStats  `yaml:"stats"`
}

// DocOrigin records how documentation was discovered and fetched.
type DocOrigin struct {
	URL          string `yaml:"url"`
	ContentType  string `yaml:"content_type"`
	Reason       string `yaml:"reason,omitempty"`
	DiscoveredAt string `yaml:"discovered_at,omitempty"`
	Validated    bool   `yaml:"validated"`
}

// UpdateConfig defines how a source should be updated.
type UpdateConfig struct {
	Strategy      string `yaml:"strategy,omitempty"`       // incremental | full | manual
	CheckInterval string `yaml:"check_interval,omitempty"` // e.g. "24h"
	Fingerprint   string `yaml:"fingerprint,omitempty"`    // hash | etag | last_modified
}

// SourceStats holds auto-maintained counts.
type SourceStats struct {
	Sections int `yaml:"sections"`
	Methods  int `yaml:"methods"`
	Concepts int `yaml:"concepts"`
	Gotchas  int `yaml:"gotchas"`
}

// ReadSourceYAML reads a source.yaml file.
func ReadSourceYAML(path string) (*SourceYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading source.yaml: %w", err)
	}

	var s SourceYAML
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing source.yaml: %w", err)
	}

	return &s, nil
}

// WriteSourceYAML writes a source.yaml file.
func WriteSourceYAML(path string, s *SourceYAML) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling source.yaml: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}
