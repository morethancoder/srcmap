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

// GetSource retrieves a source by ID.
func (db *DB) GetSource(id string) (*SourceRecord, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, version, repo_url, path, global, last_updated, section_count, method_count, concept_count, gotcha_count
		 FROM sources WHERE id = ?`, id,
	)

	var s SourceRecord
	var globalInt int
	if err := row.Scan(&s.ID, &s.Name, &s.Version, &s.RepoURL, &s.Path, &globalInt,
		&s.LastUpdated, &s.SectionCount, &s.MethodCount, &s.ConceptCount, &s.GotchaCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("source not found: %s", id)
		}
		return nil, fmt.Errorf("getting source: %w", err)
	}
	s.Global = globalInt == 1
	return &s, nil
}

// ListSources returns all sources, optionally filtered by scope.
func (db *DB) ListSources(globalOnly bool) ([]SourceRecord, error) {
	query := "SELECT id, name, version, repo_url, path, global, last_updated, section_count, method_count, concept_count, gotcha_count FROM sources"
	if globalOnly {
		query += " WHERE global = 1"
	}
	query += " ORDER BY name"

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
			&s.LastUpdated, &s.SectionCount, &s.MethodCount, &s.ConceptCount, &s.GotchaCount); err != nil {
			return nil, fmt.Errorf("scanning source: %w", err)
		}
		s.Global = globalInt == 1
		sources = append(sources, s)
	}
	return sources, rows.Err()
}
