package parser

// SymbolKind represents the type of a code symbol.
type SymbolKind string

const (
	SymbolFunction  SymbolKind = "function"
	SymbolMethod    SymbolKind = "method"
	SymbolClass     SymbolKind = "class"
	SymbolType      SymbolKind = "type"
	SymbolInterface SymbolKind = "interface"
	SymbolConstant  SymbolKind = "constant"
)

// Symbol represents a parsed code symbol.
type Symbol struct {
	Name        string
	Kind        SymbolKind
	FilePath    string
	StartLine   int
	EndLine     int
	Parameters  string // serialized parameter list
	ReturnType  string
	ParentScope string // enclosing class/module name
	Fingerprint string // content hash for incremental updates
	SourceID    string // which source this belongs to
}

// Parser extracts symbols from source files.
type Parser interface {
	// Parse extracts all symbols from the given file content.
	Parse(filePath string, content []byte) ([]Symbol, error)

	// SupportedExtensions returns the file extensions this parser handles.
	SupportedExtensions() []string
}
