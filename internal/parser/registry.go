package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Registry holds all available parsers indexed by file extension.
type Registry struct {
	parsers map[string]Parser
}

// NewRegistry creates a parser registry with all built-in parsers.
func NewRegistry() *Registry {
	r := &Registry{parsers: make(map[string]Parser)}

	for _, p := range []Parser{&GoParser{}, &TypeScriptParser{}, &PythonParser{}} {
		for _, ext := range p.SupportedExtensions() {
			r.parsers[ext] = p
		}
	}

	return r
}

// ParseFile parses a single file, selecting the parser by extension.
func (r *Registry) ParseFile(filePath string) ([]Symbol, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	p, ok := r.parsers[ext]
	if !ok {
		return nil, nil // unsupported file type, skip silently
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filePath, err)
	}

	return p.Parse(filePath, content)
}

// ParseDirectory walks a directory and parses all supported files.
func (r *Registry) ParseDirectory(dir string) ([]Symbol, error) {
	var allSymbols []Symbol

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == ".git" || name == "__pycache__" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		symbols, err := r.ParseFile(path)
		if err != nil {
			return nil // skip parse errors
		}
		allSymbols = append(allSymbols, symbols...)
		return nil
	})

	return allSymbols, err
}

// Supported returns true if the given file extension has a parser.
func (r *Registry) Supported(ext string) bool {
	_, ok := r.parsers[strings.ToLower(ext)]
	return ok
}
