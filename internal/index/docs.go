package index

import (
	"fmt"

	"github.com/morethancoder/srcmap/internal/docfetcher"
	"github.com/morethancoder/srcmap/pkg/fileformat"
)

// InsertDocFile adds a doc file record to the database.
func (db *DB) InsertDocFile(sourceID string, fm *fileformat.Frontmatter, filePath string) error {
	_, err := db.conn.Exec(
		`INSERT OR REPLACE INTO doc_files (id, source_id, kind, section, file_path, fingerprint, last_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		fm.ID, sourceID, string(fm.Kind), fm.Section, filePath, fm.Fingerprint, fm.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("inserting doc file: %w", err)
	}
	return nil
}

// InsertChunk stores a pre-chunked documentation block and indexes it in FTS5.
// Both writes run in one transaction so chunks and chunks_fts never drift.
// If a chunk with the same (source_id, fingerprint) already exists, returns
// the existing ID and skips the insert so re-ingestion is idempotent.
func (db *DB) InsertChunk(c *docfetcher.Chunk) (int64, error) {
	if c.Fingerprint != "" {
		var existing int64
		err := db.conn.QueryRow(
			`SELECT id FROM chunks WHERE source_id = ? AND fingerprint = ? LIMIT 1`,
			c.SourceID, c.Fingerprint,
		).Scan(&existing)
		if err == nil {
			return existing, nil
		}
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	kind := c.Kind
	if kind == "" {
		kind = docfetcher.ChunkKindDoc
	}
	res, err := tx.Exec(
		`INSERT INTO chunks (source_id, page_url, chunk_index, heading, content, estimated_tokens, fingerprint, status, kind, anchor_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.SourceID, c.PageURL, c.ChunkIndex, c.Heading, c.Content, c.EstimatedTokens, c.Fingerprint, string(c.Status), string(kind), c.AnchorID,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting chunk: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(
		`INSERT INTO chunks_fts (content, heading, source_id, chunk_id) VALUES (?, ?, ?, ?)`,
		c.Content, c.Heading, c.SourceID, id,
	); err != nil {
		return 0, fmt.Errorf("indexing chunk in fts: %w", err)
	}
	return id, tx.Commit()
}

// DocMatch is one ranked FTS5 result with a snippet.
type DocMatch struct {
	ChunkID int64
	Heading string
	Snippet string
	Rank    float64
}

// SearchDocs runs an FTS5 query scoped to a single source, BM25-ranked,
// returning a highlighted snippet for each hit.
func (db *DB) SearchDocs(sourceID, query string, limit int) ([]DocMatch, error) {
	if limit <= 0 {
		limit = 10
	}
	// snippet(col, 0, prefix, suffix, ellipsis, numTokens)
	rows, err := db.conn.Query(
		`SELECT chunk_id, heading,
		        snippet(chunks_fts, 0, '«', '»', '…', 24) AS sn,
		        bm25(chunks_fts) AS rank
		 FROM chunks_fts
		 WHERE chunks_fts MATCH ? AND source_id = ?
		 ORDER BY rank ASC
		 LIMIT ?`,
		query, sourceID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("fts5 search: %w", err)
	}
	defer rows.Close()

	var out []DocMatch
	for rows.Next() {
		var m DocMatch
		if err := rows.Scan(&m.ChunkID, &m.Heading, &m.Snippet, &m.Rank); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateChunkStatus sets the processing status of a chunk.
func (db *DB) UpdateChunkStatus(chunkID int64, status docfetcher.ChunkStatus) error {
	_, err := db.conn.Exec("UPDATE chunks SET status = ? WHERE id = ?", string(status), chunkID)
	if err != nil {
		return fmt.Errorf("updating chunk status: %w", err)
	}
	return nil
}

// GetChunk retrieves a single chunk by ID.
func (db *DB) GetChunk(chunkID int64) (*docfetcher.Chunk, error) {
	row := db.conn.QueryRow(
		`SELECT id, source_id, page_url, chunk_index, heading, content,
		        estimated_tokens, fingerprint, status, kind, anchor_id
		 FROM chunks WHERE id = ?`, chunkID,
	)
	var c docfetcher.Chunk
	var status, kind string
	if err := row.Scan(&c.ID, &c.SourceID, &c.PageURL, &c.ChunkIndex, &c.Heading,
		&c.Content, &c.EstimatedTokens, &c.Fingerprint, &status, &kind, &c.AnchorID); err != nil {
		return nil, fmt.Errorf("getting chunk: %w", err)
	}
	c.Status = docfetcher.ChunkStatus(status)
	c.Kind = docfetcher.ChunkKind(kind)
	return &c, nil
}

// GetPendingChunks retrieves all pending chunks for a source.
func (db *DB) GetPendingChunks(sourceID string) ([]docfetcher.Chunk, error) {
	rows, err := db.conn.Query(
		`SELECT id, source_id, page_url, chunk_index, heading, content,
		        estimated_tokens, fingerprint, status, kind, anchor_id
		 FROM chunks WHERE source_id = ? AND status = ?
		 ORDER BY id ASC`,
		sourceID, string(docfetcher.ChunkPending),
	)
	if err != nil {
		return nil, fmt.Errorf("querying pending chunks: %w", err)
	}
	defer rows.Close()

	var chunks []docfetcher.Chunk
	for rows.Next() {
		var c docfetcher.Chunk
		var status, kind string
		if err := rows.Scan(&c.ID, &c.SourceID, &c.PageURL, &c.ChunkIndex, &c.Heading,
			&c.Content, &c.EstimatedTokens, &c.Fingerprint, &status, &kind, &c.AnchorID); err != nil {
			return nil, fmt.Errorf("scanning pending chunk: %w", err)
		}
		c.Status = docfetcher.ChunkStatus(status)
		c.Kind = docfetcher.ChunkKind(kind)
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// UpdateSourceCounts refreshes section/concept/gotcha counts from the doc files on disk.
func (db *DB) UpdateSourceCounts(sourceID string, sections, concepts, gotchas int) error {
	_, err := db.conn.Exec(
		`UPDATE sources SET section_count = ?, concept_count = ?, gotcha_count = ? WHERE id = ?`,
		sections, concepts, gotchas, sourceID,
	)
	if err != nil {
		return fmt.Errorf("updating source counts: %w", err)
	}
	return nil
}

// ChunkCounts returns the number of chunks in each status for a source.
func (db *DB) ChunkCounts(sourceID string) (pending, processed, failed int, err error) {
	rows, err := db.conn.Query(
		`SELECT status, COUNT(*) FROM chunks WHERE source_id = ? GROUP BY status`,
		sourceID,
	)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("counting chunks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, 0, fmt.Errorf("scanning chunk count: %w", err)
		}
		switch docfetcher.ChunkStatus(status) {
		case docfetcher.ChunkPending:
			pending = count
		case docfetcher.ChunkProcessed:
			processed = count
		case docfetcher.ChunkFailed:
			failed = count
		}
	}
	return pending, processed, failed, rows.Err()
}
