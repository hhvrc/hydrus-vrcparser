package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Chunk is one cached iTXt chunk, as stored in itxt_chunks. Nullable columns
// are pointers: a null keyword and an empty one are treated differently by
// the parsers.
type Chunk struct {
	Seq               int
	Keyword           *string
	CompressionFlag   *int
	CompressionMethod *int
	LanguageTag       *string
	TranslatedKeyword *string
	Text              *string

	// ContentType is one of json, xml, line, text.
	ContentType string
}

// Chunks returns a file's cached chunks in file order.
func (db *DB) Chunks(ctx context.Context, fileID int) ([]Chunk, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT seq, keyword, compression_flag, compression_method, language_tag, translated_keyword, text, content_type
		FROM itxt_chunks WHERE file_id = ? ORDER BY seq`, fileID)
	if err != nil {
		return nil, fmt.Errorf("load chunks for file %d: %w", fileID, err)
	}
	defer rows.Close()

	var out []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.Seq, &c.Keyword, &c.CompressionFlag, &c.CompressionMethod,
			&c.LanguageTag, &c.TranslatedKeyword, &c.Text, &c.ContentType); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReplaceChunks swaps a file's cached chunks for a fresh read. Written even
// when empty: "this PNG carries no iTXt" is an answer, and caching it is what
// stops the next run re-reading the file off the share.
func (db *DB) ReplaceChunks(ctx context.Context, fileID int, chunks []Chunk) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM itxt_chunks WHERE file_id = ?", fileID); err != nil {
			return err
		}
		for _, c := range chunks {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO itxt_chunks (file_id, seq, keyword, compression_flag, compression_method,
				                         language_tag, translated_keyword, text, content_type)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				fileID, c.Seq, c.Keyword, c.CompressionFlag, c.CompressionMethod,
				c.LanguageTag, c.TranslatedKeyword, c.Text, c.ContentType)
			if err != nil {
				return fmt.Errorf("chunk %d: %w", c.Seq, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("cache chunks for file %d: %w", fileID, err)
	}
	return nil
}

// DataDirectory returns the directory the legacy pipeline recorded the file
// under, or "" when none was recorded.
func (db *DB) DataDirectory(ctx context.Context, fileID int) (string, error) {
	var dir sql.NullString
	err := db.sql.QueryRowContext(ctx, `
		SELECT d.path FROM files f JOIN data_dirs d ON d.id = f.data_dir_id
		WHERE f.file_id = ?`, fileID).Scan(&dir)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("data directory for file %d: %w", fileID, err)
	}
	return dir.String, nil
}
