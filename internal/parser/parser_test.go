package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/morethancoder/srcmap/internal/parser"
)

func TestTypeScriptParser(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sample.ts"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	p := &parser.TypeScriptParser{}
	symbols, err := p.Parse("sample.ts", content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Expected: ZodType (interface), ZodTypeAny (type), ZodString (class),
	// ZodString.min, ZodString.max, ZodString.email (methods),
	// parse (function), DEFAULT_ERROR (constant)
	expected := map[string]parser.SymbolKind{
		"ZodType":          parser.SymbolInterface,
		"ZodTypeAny":       parser.SymbolType,
		"ZodString":        parser.SymbolClass,
		"ZodString.min":    parser.SymbolMethod,
		"ZodString.max":    parser.SymbolMethod,
		"ZodString.email":  parser.SymbolMethod,
		"parse":            parser.SymbolFunction,
		"DEFAULT_ERROR":    parser.SymbolConstant,
	}

	found := make(map[string]parser.SymbolKind)
	for _, s := range symbols {
		found[s.Name] = s.Kind
	}

	for name, kind := range expected {
		got, ok := found[name]
		if !ok {
			t.Errorf("missing symbol %q", name)
			continue
		}
		if got != kind {
			t.Errorf("symbol %q: got kind %q, want %q", name, got, kind)
		}
	}
}

func TestPythonParser(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sample.py"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	p := &parser.PythonParser{}
	symbols, err := p.Parse("sample.py", content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	expected := map[string]parser.SymbolKind{
		"HttpClient":          parser.SymbolClass,
		"HttpClient.__init__": parser.SymbolMethod,
		"HttpClient.get":      parser.SymbolMethod,
		"HttpClient.post":     parser.SymbolMethod,
		"create_session":      parser.SymbolFunction,
		"Response":            parser.SymbolClass,
		"Response.json":       parser.SymbolMethod,
		"Response.text":       parser.SymbolMethod,
	}

	found := make(map[string]parser.SymbolKind)
	for _, s := range symbols {
		found[s.Name] = s.Kind
	}

	for name, kind := range expected {
		got, ok := found[name]
		if !ok {
			t.Errorf("missing symbol %q", name)
			continue
		}
		if got != kind {
			t.Errorf("symbol %q: got kind %q, want %q", name, got, kind)
		}
	}

	// Verify 'self' is stripped from method params
	for _, s := range symbols {
		if s.Name == "HttpClient.get" {
			if s.Parameters == "" {
				t.Error("HttpClient.get should have params")
			}
			if s.ReturnType != "dict" {
				t.Errorf("HttpClient.get return type: got %q, want %q", s.ReturnType, "dict")
			}
		}
	}
}

func TestGoParser(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sample.go"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	p := &parser.GoParser{}
	symbols, err := p.Parse("sample.go", content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	expected := map[string]parser.SymbolKind{
		"Server":       parser.SymbolType,
		"Server.Start": parser.SymbolMethod,
		"Server.Stop":  parser.SymbolMethod,
		"Handler":      parser.SymbolInterface,
		"Request":      parser.SymbolType,
		"Response":     parser.SymbolType,
		"NewServer":    parser.SymbolFunction,
		"DefaultPort":  parser.SymbolConstant,
	}

	found := make(map[string]parser.SymbolKind)
	for _, s := range symbols {
		found[s.Name] = s.Kind
	}

	for name, kind := range expected {
		got, ok := found[name]
		if !ok {
			t.Errorf("missing symbol %q", name)
			continue
		}
		if got != kind {
			t.Errorf("symbol %q: got kind %q, want %q", name, got, kind)
		}
	}
}

func TestParserRegistry(t *testing.T) {
	reg := parser.NewRegistry()

	if !reg.Supported(".go") {
		t.Error("expected .go to be supported")
	}
	if !reg.Supported(".ts") {
		t.Error("expected .ts to be supported")
	}
	if !reg.Supported(".py") {
		t.Error("expected .py to be supported")
	}
	if reg.Supported(".rs") {
		t.Error("expected .rs to not be supported")
	}
}

func TestSymbolFingerprints(t *testing.T) {
	content := []byte(`package test

func Hello() string {
	return "hello"
}
`)
	p := &parser.GoParser{}
	symbols, err := p.Parse("test.go", content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected at least one symbol")
	}

	for _, s := range symbols {
		if s.Fingerprint == "" {
			t.Errorf("symbol %q has empty fingerprint", s.Name)
		}
	}
}
