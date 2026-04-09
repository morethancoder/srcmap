package parser

import (
	"regexp"
	"strings"
)

// PythonParser extracts symbols from Python files using regex patterns.
type PythonParser struct{}

func (p *PythonParser) SupportedExtensions() []string {
	return []string{".py", ".pyi"}
}

var (
	pyFuncRe  = regexp.MustCompile(`(?m)^def\s+(\w+)\s*\(([^)]*)\)(?:\s*->\s*(\w+))?`)
	pyClassRe = regexp.MustCompile(`(?m)^class\s+(\w+)(?:\(([^)]*)\))?`)
	// Method: indented def inside class
	pyMethodRe = regexp.MustCompile(`(?m)^[ \t]+def\s+(\w+)\s*\(([^)]*)\)(?:\s*->\s*(\w+))?`)
)

func (p *PythonParser) Parse(filePath string, content []byte) ([]Symbol, error) {
	lines := strings.Split(string(content), "\n")
	var symbols []Symbol
	var currentClass string

	for i, line := range lines {
		lineNum := i + 1

		// Class
		if m := pyClassRe.FindStringSubmatch(line); m != nil {
			end := findPythonBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolClass,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Fingerprint: fingerprint(content, lineNum, end),
			})
			currentClass = m[1]
			continue
		}

		// Top-level function
		if m := pyFuncRe.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			end := findPythonBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name:        m[1],
				Kind:        SymbolFunction,
				FilePath:    filePath,
				StartLine:   lineNum,
				EndLine:     end,
				Parameters:  m[2],
				ReturnType:  m[3],
				Fingerprint: fingerprint(content, lineNum, end),
			})
			currentClass = ""
			continue
		}

		// Method inside class
		if m := pyMethodRe.FindStringSubmatch(line); m != nil {
			// Check if we're still inside a class (line is indented)
			if currentClass != "" && (strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")) {
				name := m[1]
				end := findPythonBlockEnd(lines, i)

				// Remove 'self' or 'cls' from params
				params := cleanPythonParams(m[2])

				symbols = append(symbols, Symbol{
					Name:        currentClass + "." + name,
					Kind:        SymbolMethod,
					FilePath:    filePath,
					StartLine:   lineNum,
					EndLine:     end,
					Parameters:  params,
					ReturnType:  m[3],
					ParentScope: currentClass,
					Fingerprint: fingerprint(content, lineNum, end),
				})
			}
			continue
		}

		// Track when we leave a class (non-indented, non-empty line that isn't a decorator)
		if currentClass != "" && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "@") && !strings.HasPrefix(line, "#") {
			currentClass = ""
		}
	}

	return symbols, nil
}

func findPythonBlockEnd(lines []string, startIdx int) int {
	if startIdx+1 >= len(lines) {
		return startIdx + 1
	}

	// Determine the indentation of the block start
	startIndent := indentLevel(lines[startIdx])

	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // skip blank lines and comments
		}
		if indentLevel(line) <= startIndent {
			return i
		}
	}

	return len(lines)
}

func indentLevel(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}

func cleanPythonParams(params string) string {
	parts := strings.Split(params, ",")
	var clean []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "self" || p == "cls" {
			continue
		}
		if p != "" {
			clean = append(clean, p)
		}
	}
	return strings.Join(clean, ", ")
}
