package index

import (
	"database/sql"
	"errors"
	"fmt"
)

// SourceRecord represents a source in the database.
type SourceRecord struct {
	ID           string
	Name         string
	Version      string
	RepoURL      string
	Path         string
	Global       bool
	LastUpdated  string
	SectionCount int
	MethodCount  int
	ConceptCount int
	GotchaCount  int
}

// InsertSource adds or replaces a source record.
func (db *DB) InsertSource(s *SourceRecord) error {
	globalInt := 0
	if s.Global {
		globalInt = 1
	}
	_, err := db.conn.Exec(
		`INSERT OR REPLACE INTO sources (id, name, version, repo_url, path, global, last_updated, section_count, method_count, concept_count, gotcha_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Version, s.RepoURL, s.Path, globalInt, s.LastUpdated,
		s.SectionCount, s.MethodCount, s.ConceptCount, s.GotchaCount,
	)
	if err != nil {
		return fmt.Errorf("inserting source: %w", err)
	}
	return nil
}

// GetSource retrieves a source by ID. Symbol count is computed live.
func (db *DB) GetSource(id string) (*SourceRecord, error) {
	row := db.conn.QueryRow(`
		SELECT s.id, s.name, s.version, s.repo_url, s.path, s.global, s.last_updated,
			s.section_count, s.concept_count, s.gotcha_count,
			COALESCE(sym.total, 0) AS method_count
		FROM sources s
		LEFT JOIN (
			SELECT source_id, COUNT(*) AS total FROM symbols WHERE source_id = ? GROUP BY source_id
		) sym ON sym.source_id = s.id
		WHERE s.id = ?`, id, id,
	)

	var s SourceRecord
	var globalInt int
	if err := row.Scan(&s.ID, &s.Name, &s.Version, &s.RepoURL, &s.Path, &globalInt,
		&s.LastUpdated, &s.SectionCount, &s.ConceptCount, &s.GotchaCount, &s.MethodCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("source not found: %s", id)
		}
		return nil, fmt.Errorf("getting source: %w", err)
	}
	s.Global = globalInt == 1
	return &s, nil
}

// ListSources returns all sources, optionally filtered by scope.
// Symbol counts are computed live from the symbols table.
func (db *DB) ListSources(globalOnly bool) ([]SourceRecord, error) {
	query := `
		SELECT s.id, s.name, s.version, s.repo_url, s.path, s.global, s.last_updated,
			s.section_count, s.concept_count, s.gotcha_count,
			COALESCE(sym.total, 0) AS method_count
		FROM sources s
		LEFT JOIN (
			SELECT source_id, COUNT(*) AS total FROM symbols GROUP BY source_id
		) sym ON sym.source_id = s.id`
	if globalOnly {
		query += " WHERE s.global = 1"
	}
	query += " ORDER BY s.name"

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("listing sources: %w", err)
	}
	defer rows.Close()

	var sources []SourceRecord
	for rows.Next() {
		var s SourceRecord
		var globalInt int
		if err := rows.Scan(&s.ID, &s.Name, &s.Version, &s.RepoURL, &s.Path, &globalInt,
			&s.LastUpdated, &s.SectionCount, &s.ConceptCount, &s.GotchaCount, &s.MethodCount); err != nil {
			return nil, fmt.Errorf("scanning source: %w", err)
		}
		s.Global = globalInt == 1
		sources = append(sources, s)
	}
	return sources, rows.Err()
}
