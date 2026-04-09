package index

import (
	"fmt"

	"github.com/morethancoder/srcmap/internal/parser"
)

// InsertSymbol adds a symbol to the database.
func (db *DB) InsertSymbol(s *parser.Symbol) (int64, error) {
	res, err := db.conn.Exec(
		`INSERT INTO symbols (source_id, name, kind, file_path, start_line, end_line, parameters, return_type, parent_scope, fingerprint)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.SourceID, s.Name, string(s.Kind), s.FilePath, s.StartLine, s.EndLine,
		s.Parameters, s.ReturnType, s.ParentScope, s.Fingerprint,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting symbol: %w", err)
	}
	return res.LastInsertId()
}

// LookupSymbol finds a symbol by source and name.
func (db *DB) LookupSymbol(sourceID, name string) (*parser.Symbol, error) {
	row := db.conn.QueryRow(
		`SELECT name, kind, file_path, start_line, end_line, parameters, return_type, parent_scope, fingerprint, source_id
		 FROM symbols WHERE source_id = ? AND name = ? LIMIT 1`,
		sourceID, name,
	)

	var s parser.Symbol
	var kind string
	if err := row.Scan(&s.Name, &kind, &s.FilePath, &s.StartLine, &s.EndLine,
		&s.Parameters, &s.ReturnType, &s.ParentScope, &s.Fingerprint, &s.SourceID); err != nil {
		return nil, fmt.Errorf("looking up symbol: %w", err)
	}
	s.Kind = parser.SymbolKind(kind)
	return &s, nil
}

// SearchSymbols searches symbols by name pattern (LIKE) within a source.
func (db *DB) SearchSymbols(sourceID, query string) ([]parser.Symbol, error) {
	rows, err := db.conn.Query(
		`SELECT name, kind, file_path, start_line, end_line, parameters, return_type, parent_scope, fingerprint, source_id
		 FROM symbols WHERE source_id = ? AND name LIKE ?`,
		sourceID, "%"+query+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("searching symbols: %w", err)
	}
	defer rows.Close()

	var symbols []parser.Symbol
	for rows.Next() {
		var s parser.Symbol
		var kind string
		if err := rows.Scan(&s.Name, &kind, &s.FilePath, &s.StartLine, &s.EndLine,
			&s.Parameters, &s.ReturnType, &s.ParentScope, &s.Fingerprint, &s.SourceID); err != nil {
			return nil, fmt.Errorf("scanning symbol: %w", err)
		}
		s.Kind = parser.SymbolKind(kind)
		symbols = append(symbols, s)
	}
	return symbols, rows.Err()
}
