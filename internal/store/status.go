package store

import (
	"context"
	"fmt"
)

// Status is a summary of the database's state, for diagnostics.
type Status struct {
	SchemaVersion int

	Files            int
	FilesWithDataDir int // found by the legacy pipeline on disk
	FilesWithChunks  int

	Chunks       int
	ChunksByType []Count

	// UnparseableChunks are cached chunks under a keyword that should carry
	// VRChat metadata but parsed as none of the known formats. Comment,
	// "parameters" (Stable Diffusion) and Microsoft.GameDVR.* chunks are
	// someone else's and excluded, as the legacy diagnostics did.
	UnparseableChunks []Count

	Taggers []TaggerStatus
}

// Count is one labelled row count.
type Count struct {
	Label string
	N     int
}

// TaggerStatus is one tagger's recorded state.
type TaggerStatus struct {
	ID string

	// Versions counts files by "extract/derive" version.
	Versions []Count

	TagSets      int // files with derived tags stored
	EmptyTagSets int // derived, but the tagger had nothing to say
	Pushed       int // files in the push ledger

	// Unpushed counts non-empty stored tag sets whose hash misses the
	// ledger; the next run pushes these.
	Unpushed int

	// ExtractedWithoutChunks counts files read off disk that carried no
	// iTXt at all (meaningful for the vrchat tagger only).
	ExtractedWithoutChunks int
}

// Status gathers the database summary.
func (db *DB) Status(ctx context.Context) (*Status, error) {
	s := &Status{}
	scalar := func(dst *int, query string, args ...any) error {
		return db.sql.QueryRowContext(ctx, query, args...).Scan(dst)
	}

	for _, q := range []struct {
		dst   *int
		query string
	}{
		{&s.SchemaVersion, "PRAGMA user_version"},
		{&s.Files, "SELECT COUNT(*) FROM files"},
		{&s.FilesWithDataDir, "SELECT COUNT(*) FROM files WHERE data_dir_id IS NOT NULL"},
		{&s.Chunks, "SELECT COUNT(*) FROM itxt_chunks"},
		{&s.FilesWithChunks, "SELECT COUNT(DISTINCT file_id) FROM itxt_chunks"},
	} {
		if err := scalar(q.dst, q.query); err != nil {
			return nil, fmt.Errorf("status: %w", err)
		}
	}

	var err error
	if s.ChunksByType, err = db.counts(ctx,
		"SELECT COALESCE(content_type, 'NULL'), COUNT(*) FROM itxt_chunks GROUP BY 1 ORDER BY 2 DESC"); err != nil {
		return nil, err
	}
	if s.UnparseableChunks, err = db.counts(ctx, `
		SELECT keyword, COUNT(*) FROM itxt_chunks
		WHERE (content_type = 'text' OR content_type IS NULL)
		  AND keyword IS NOT NULL
		  AND keyword NOT IN ('Comment', 'parameters')
		  AND keyword NOT LIKE 'Microsoft.GameDVR.%'
		GROUP BY keyword ORDER BY 2 DESC`); err != nil {
		return nil, err
	}

	ids, err := db.strings(ctx, `
		SELECT tagger_id FROM tagger_file_state
		UNION SELECT tagger_id FROM tagger_tags
		UNION SELECT tagger_id FROM pushes
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		ts, err := db.taggerStatus(ctx, id)
		if err != nil {
			return nil, err
		}
		s.Taggers = append(s.Taggers, *ts)
	}
	return s, nil
}

func (db *DB) taggerStatus(ctx context.Context, id string) (*TaggerStatus, error) {
	ts := &TaggerStatus{ID: id}
	var err error
	if ts.Versions, err = db.counts(ctx, `
		SELECT extract_version || '/' || derive_version, COUNT(*) FROM tagger_file_state
		WHERE tagger_id = ? GROUP BY extract_version, derive_version
		ORDER BY extract_version, derive_version`, id); err != nil {
		return nil, err
	}

	for _, q := range []struct {
		dst   *int
		query string
	}{
		{&ts.TagSets, "SELECT COUNT(*) FROM tagger_tags WHERE tagger_id = ?"},
		{&ts.EmptyTagSets, "SELECT COUNT(*) FROM tagger_tags WHERE tagger_id = ? AND tags = '" + emptyTagsJSON + "'"},
		{&ts.Pushed, "SELECT COUNT(*) FROM pushes WHERE tagger_id = ?"},
		{&ts.ExtractedWithoutChunks, `
			SELECT COUNT(*) FROM tagger_file_state s
			WHERE s.tagger_id = ? AND s.extract_version > 0
			  AND NOT EXISTS (SELECT 1 FROM itxt_chunks c WHERE c.file_id = s.file_id)`},
	} {
		if err := db.sql.QueryRowContext(ctx, q.query, id).Scan(q.dst); err != nil {
			return nil, fmt.Errorf("status of %s: %w", id, err)
		}
	}
	if id != "vrchat" {
		ts.ExtractedWithoutChunks = 0
	}

	// Same rule the host pushes by: one implementation, in UnpushedTags.
	ids, err := db.ints(ctx, "SELECT file_id FROM tagger_tags WHERE tagger_id = ?", id)
	if err != nil {
		return nil, err
	}
	unpushed, err := db.UnpushedTags(ctx, id, ids)
	if err != nil {
		return nil, fmt.Errorf("status of %s: %w", id, err)
	}
	ts.Unpushed = len(unpushed)
	return ts, nil
}

func (db *DB) ints(ctx context.Context, query string, args ...any) ([]int, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (db *DB) counts(ctx context.Context, query string, args ...any) ([]Count, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	defer rows.Close()
	var out []Count
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Label, &c.N); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) strings(ctx context.Context, query string) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
