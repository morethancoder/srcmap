package parser

import (
	"regexp"
	"strings"
)

// TypeScriptParser extracts symbols from TypeScript/JavaScript files using regex patterns.
type TypeScriptParser struct{}

func (p *TypeScriptParser) SupportedExtensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mts", ".mjs"}
}

var (
	// Exported function: export function name(...) or export default function name(...)
	tsExportFuncRe = regexp.MustCompile(`(?m)^(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+(\w+)\s*(<[^>]*>)?\s*\(([^)]*)\)(?:\s*:\s*([^\s{]+))?`)
	// Arrow function: export const name = (...) =>
	tsArrowRe = regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*(?::\s*[^=]+)?\s*=\s*(?:async\s+)?(?:\([^)]*\)|[^=])\s*=>`)
	// Class: export class Name
	tsClassRe = regexp.MustCompile(`(?m)^(?:export\s+(?:default\s+)?)?(?:abstract\s+)?class\s+(\w+)(?:\s+extends\s+(\w+))?`)
	// Interface: export interface Name
	tsInterfaceRe = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+(\w+)`)
	// Type alias: export type Name =
	tsTypeRe = regexp.MustCompile(`(?m)^(?:export\s+)?type\s+(\w+)(?:\s*<[^>]*>)?\s*=`)
	// Method inside class: name(...) { or async name(...)
	tsMethodRe = regexp.MustCompile(`(?m)^\s+(?:(?:public|private|protected|static|async|readonly|override|abstract)\s+)*(\w+)\s*(<[^>]*>)?\s*\(([^)]*)\)(?:\s*:\s*([^\s{]+))?`)
	// Const export: export const NAME =
	tsConstRe = regexp.MustCompile(`(?m)^(?:export\s+)?const\s+([A-Z][A-Z0-9_]*)\s*(?::\s*[^=]+)?\s*=`)
)

func (p *TypeScriptParser) Parse(filePath string, content []byte) ([]Symbol, error) {
	lines := strings.Split(string(content), "\n")
	var symbols []Symbol
	var currentClass string
	braceDepth := 0
	classDepth := -1

	for i, line := range lines {
		lineNum := i + 1

		// Track brace depth for class scope
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
		if classDepth >= 0 && braceDepth <= classDepth {
			currentClass = ""
			classDepth = -1
		}

		// Class
		if m := tsClassRe.FindStringSubmatch(line); m != nil {
			end := findBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolClass,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Fingerprint: fingerprint(content, lineNum, end),
			})
			currentClass = m[1]
			classDepth = braceDepth - 1
			continue
		}

		// Interface
		if m := tsInterfaceRe.FindStringSubmatch(line); m != nil {
			end := findBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolInterface,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Fingerprint: fingerprint(content, lineNum, end),
			})
			continue
		}

		// Type alias
		if m := tsTypeRe.FindStringSubmatch(line); m != nil {
			end := findStatementEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolType,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Fingerprint: fingerprint(content, lineNum, end),
			})
			continue
		}

		// Exported function
		if m := tsExportFuncRe.FindStringSubmatch(line); m != nil {
			end := findBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolFunction,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Parameters:  m[3],
				ReturnType:  m[4],
				Fingerprint: fingerprint(content, lineNum, end),
			})
			continue
		}

		// Arrow function
		if m := tsArrowRe.FindStringSubmatch(line); m != nil {
			end := findBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolFunction,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Fingerprint: fingerprint(content, lineNum, end),
			})
			continue
		}

		// Constant
		if m := tsConstRe.FindStringSubmatch(line); m != nil {
			end := findStatementEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolConstant,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Fingerprint: fingerprint(content, lineNum, end),
			})
			continue
		}

		// Method inside class
		if currentClass != "" {
			if m := tsMethodRe.FindStringSubmatch(line); m != nil {
				name := m[1]
				if name == "constructor" || name == "if" || name == "for" || name == "while" || name == "switch" {
					continue
				}
				end := findBlockEnd(lines, i)
				symbols = append(symbols, Symbol{
					Name:        currentClass + "." + name,
					Kind:        SymbolMethod,
					FilePath:    filePath,
					StartLine:   lineNum,
					EndLine:     end,
					Parameters:  m[3],
					ReturnType:  m[4],
					ParentScope: currentClass,
					Fingerprint: fingerprint(content, lineNum, end),
				})
			}
		}
	}

	return symbols, nil
}

func findBlockEnd(lines []string, startIdx int) int {
	depth := 0
	for i := startIdx; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth <= 0 && i > startIdx {
			return i + 1
		}
	}
	return startIdx + 1
}

func findStatementEnd(lines []string, startIdx int) int {
	for i := startIdx; i < len(lines); i++ {
		if strings.Contains(lines[i], ";") || (i > startIdx && strings.TrimSpace(lines[i]) == "") {
			return i + 1
		}
	}
	return startIdx + 1
}
