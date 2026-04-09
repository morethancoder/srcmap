package parser

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strings"
)

// GoParser extracts symbols from Go source files using go/ast.
type GoParser struct{}

func (p *GoParser) SupportedExtensions() []string {
	return []string{".go"}
}

func (p *GoParser) Parse(filePath string, content []byte) ([]Symbol, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, filePath, content, goparser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing Go file %s: %w", filePath, err)
	}

	var symbols []Symbol

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := goFuncSymbol(fset, filePath, d, content)
			symbols = append(symbols, sym)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					syms := goTypeSymbol(fset, filePath, s, content)
					symbols = append(symbols, syms...)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						kind := SymbolConstant
						pos := fset.Position(name.Pos())
						endPos := fset.Position(s.End())
						symbols = append(symbols, Symbol{
							Name:        name.Name,
							Kind:        kind,
							FilePath:    filePath,
							StartLine:   pos.Line,
							EndLine:     endPos.Line,
							Fingerprint: fingerprint(content, pos.Line, endPos.Line),
						})
					}
				}
			}
		}
	}

	return symbols, nil
}

func goFuncSymbol(fset *token.FileSet, filePath string, d *ast.FuncDecl, content []byte) Symbol {
	pos := fset.Position(d.Pos())
	endPos := fset.Position(d.End())

	sym := Symbol{
		Name:      d.Name.Name,
		Kind:      SymbolFunction,
		FilePath:  filePath,
		StartLine: pos.Line,
		EndLine:   endPos.Line,
	}

	// Method receiver
	if d.Recv != nil && len(d.Recv.List) > 0 {
		sym.Kind = SymbolMethod
		recv := d.Recv.List[0]
		sym.ParentScope = exprName(recv.Type)
		sym.Name = sym.ParentScope + "." + d.Name.Name
	}

	// Parameters
	if d.Type.Params != nil {
		sym.Parameters = fieldListString(d.Type.Params)
	}

	// Return type
	if d.Type.Results != nil {
		sym.ReturnType = fieldListString(d.Type.Results)
	}

	sym.Fingerprint = fingerprint(content, pos.Line, endPos.Line)
	return sym
}

func goTypeSymbol(fset *token.FileSet, filePath string, s *ast.TypeSpec, content []byte) []Symbol {
	pos := fset.Position(s.Pos())
	endPos := fset.Position(s.End())

	sym := Symbol{
		Name:        s.Name.Name,
		Kind:        SymbolType,
		FilePath:    filePath,
		StartLine:   pos.Line,
		EndLine:     endPos.Line,
		Fingerprint: fingerprint(content, pos.Line, endPos.Line),
	}

	if _, ok := s.Type.(*ast.InterfaceType); ok {
		sym.Kind = SymbolInterface
	}

	return []Symbol{sym}
}

func exprName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprName(t.X)
	case *ast.IndexExpr:
		return exprName(t.X)
	default:
		return ""
	}
}

func fieldListString(fl *ast.FieldList) string {
	var parts []string
	for _, f := range fl.List {
		parts = append(parts, fmt.Sprintf("%v", f.Type))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func fingerprint(content []byte, startLine, endLine int) string {
	lines := strings.Split(string(content), "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	section := strings.Join(lines[startLine-1:endLine], "\n")
	h := sha256.Sum256([]byte(section))
	return fmt.Sprintf("%x", h[:8])
}
