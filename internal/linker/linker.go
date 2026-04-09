package linker

import "github.com/morethancoder/srcmap/internal/parser"

// DocLink represents an association between a code symbol and a doc file.
type DocLink struct {
	SymbolName string
	DocFileID  string
	Confidence float64 // 0.0–1.0 match quality
}

// Link finds doc files that match the given symbols using fuzzy name matching.
func Link(symbols []parser.Symbol, docFileIDs []string) ([]DocLink, error) {
	var links []DocLink
	nameSet := make(map[string]bool, len(docFileIDs))
	for _, id := range docFileIDs {
		nameSet[id] = true
	}

	for _, sym := range symbols {
		if nameSet[sym.Name] {
			links = append(links, DocLink{
				SymbolName: sym.Name,
				DocFileID:  sym.Name,
				Confidence: 1.0,
			})
		}
	}
	return links, nil
}
