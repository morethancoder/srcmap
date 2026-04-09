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

// InsertChunk stores a pre-chunked documentation block.
func (db *DB) InsertChunk(c *docfetcher.Chunk) (int64, error) {
	res, err := db.conn.Exec(
		`INSERT INTO chunks (source_id, page_url, chunk_index, heading, content, estimated_tokens, fingerprint, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.SourceID, c.PageURL, c.ChunkIndex, c.Heading, c.Content, c.EstimatedTokens, c.Fingerprint, string(c.Status),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting chunk: %w", err)
	}
	return res.LastInsertId()
}

// UpdateChunkStatus sets the processing status of a chunk.
func (db *DB) UpdateChunkStatus(chunkID int64, status docfetcher.ChunkStatus) error {
	_, err := db.conn.Exec("UPDATE chunks SET status = ? WHERE id = ?", string(status), chunkID)
	if err != nil {
		return fmt.Errorf("updating chunk status: %w", err)
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
