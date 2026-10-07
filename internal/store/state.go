package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
)

// Compile-time check that DB satisfies the host's store contract.
var _ tagging.Store = (*DB)(nil)

// emptyTagsJSON is how an empty tag set is stored. Queries compare against it
// to skip files a tagger had nothing to say about.
const emptyTagsJSON = "[]"

// queryByIDs runs query once per IN-list-sized chunk of ids. The query ends in
// "file_id IN (%s)"; prefix holds the arguments that come before the ids.
func (db *DB) queryByIDs(ctx context.Context, query string, prefix []any, ids []int, scan func(*sql.Rows) error) error {
	return inChunks(ids, func(chunk []int) error {
		marks, args := placeholders(chunk)
		rows, err := db.sql.QueryContext(ctx, fmt.Sprintf(query, marks), append(append([]any(nil), prefix...), args...)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// FileRefs returns known identity for the given ids.
func (db *DB) FileRefs(ctx context.Context, fileIDs []int) (map[int]tagging.FileRef, error) {
	out := make(map[int]tagging.FileRef, len(fileIDs))
	err := db.queryByIDs(ctx, "SELECT file_id, hash, file_ext FROM files WHERE file_id IN (%s)", nil, fileIDs,
		func(rows *sql.Rows) error {
			var f tagging.FileRef
			if err := rows.Scan(&f.FileID, &f.Hash, &f.Ext); err != nil {
				return err
			}
			out[f.FileID] = f
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("load file identity: %w", err)
	}
	return out, nil
}

// UpsertFileRefs records identity for newly seen files. A row the legacy
// pipeline wrote keeps its data directory and size; Hydrus never changes a
// file id's hash.
//
// A hash already stored under a different file id means this cache was built
// against another Hydrus database. That is refused with both ids named rather
// than absorbed: silently skipping the file would leave it untaggable.
func (db *DB) UpsertFileRefs(ctx context.Context, files []tagging.FileRef) error {
	if len(files) == 0 {
		return nil
	}
	now := formatTime(db.now())
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		lookup, err := tx.PrepareContext(ctx, "SELECT file_id FROM files WHERE hash = ?")
		if err != nil {
			return err
		}
		defer lookup.Close()
		insert, err := tx.PrepareContext(ctx, `
			INSERT INTO files (file_id, hash, file_ext, created_at, size)
			VALUES (?, ?, ?, ?, 0)
			ON CONFLICT (file_id) DO NOTHING`)
		if err != nil {
			return err
		}
		defer insert.Close()

		for _, f := range files {
			var existing int
			switch err := lookup.QueryRowContext(ctx, f.Hash).Scan(&existing); {
			case err == sql.ErrNoRows:
			case err != nil:
				return err
			case existing != f.FileID:
				return fmt.Errorf("file %d has hash %s, which the cache already holds as file %d; "+
					"the database belongs to a different Hydrus client", f.FileID, f.Hash, existing)
			}
			if _, err := insert.ExecContext(ctx, f.FileID, f.Hash, f.Ext, now); err != nil {
				return fmt.Errorf("file %d: %w", f.FileID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record file identity: %w", err)
	}
	return nil
}

// FileStates returns per-file versions for one tagger.
func (db *DB) FileStates(ctx context.Context, taggerID string, fileIDs []int) (map[int]tagging.FileState, error) {
	out := make(map[int]tagging.FileState, len(fileIDs))
	err := db.queryByIDs(ctx,
		"SELECT file_id, extract_version, derive_version FROM tagger_file_state WHERE tagger_id = ? AND file_id IN (%s)",
		[]any{taggerID}, fileIDs,
		func(rows *sql.Rows) error {
			var id int
			var s tagging.FileState
			if err := rows.Scan(&id, &s.ExtractVersion, &s.DeriveVersion); err != nil {
				return err
			}
			out[id] = s
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("load %s file states: %w", taggerID, err)
	}
	return out, nil
}

// PushedHashes returns the hash of the tag set last pushed, per file.
func (db *DB) PushedHashes(ctx context.Context, taggerID string, fileIDs []int) (map[int]string, error) {
	out := make(map[int]string, len(fileIDs))
	err := db.queryByIDs(ctx,
		"SELECT file_id, tag_hash FROM pushes WHERE tagger_id = ? AND file_id IN (%s)",
		[]any{taggerID}, fileIDs,
		func(rows *sql.Rows) error {
			var id int
			var hash string
			if err := rows.Scan(&id, &hash); err != nil {
				return err
			}
			out[id] = hash
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("load %s push ledger: %w", taggerID, err)
	}
	return out, nil
}

// UnpushedTags returns stored, non-empty tag sets whose hash misses the push
// ledger. Empty sets are never pushed, so they are never "unpushed".
//
// Without this the derive-version gate would swallow retries: a file that
// derived cleanly but whose push failed is already at the current version, so
// the next run would not re-derive it and would never notice the gap.
func (db *DB) UnpushedTags(ctx context.Context, taggerID string, fileIDs []int) (map[int]tagging.TagSet, error) {
	out := map[int]tagging.TagSet{}
	err := db.queryByIDs(ctx, `
		SELECT t.file_id, t.tags, p.tag_hash FROM tagger_tags t
		LEFT JOIN pushes p ON p.tagger_id = t.tagger_id AND p.file_id = t.file_id
		WHERE t.tagger_id = ? AND t.tags <> ? AND t.file_id IN (%s)`,
		[]any{taggerID, emptyTagsJSON}, fileIDs,
		func(rows *sql.Rows) error {
			var id int
			var raw string
			var pushed sql.NullString
			if err := rows.Scan(&id, &raw, &pushed); err != nil {
				return err
			}
			tags, err := decodeTags(raw)
			if err != nil {
				return fmt.Errorf("file %d: %w", id, err)
			}
			if pushed.String != tags.Hash() {
				out[id] = tags
			}
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("load %s unpushed tags: %w", taggerID, err)
	}
	return out, nil
}

// Record commits outcomes for one tagger in one transaction. Nil fields leave
// stored values alone, so the host can record piecemeal as a run progresses.
func (db *DB) Record(ctx context.Context, taggerID string, outcomes []tagging.Outcome) error {
	if len(outcomes) == 0 {
		return nil
	}
	now := formatTime(db.now())

	err := db.withTx(ctx, func(tx *sql.Tx) error {
		for _, o := range outcomes {
			if o.ExtractVersion != nil || o.DeriveVersion != nil {
				// COALESCE keeps whichever version this outcome does not set.
				_, err := tx.ExecContext(ctx, `
					INSERT INTO tagger_file_state (tagger_id, file_id, extract_version, derive_version)
					VALUES (?1, ?2, COALESCE(?3, 0), COALESCE(?4, 0))
					ON CONFLICT (tagger_id, file_id) DO UPDATE SET
						extract_version = COALESCE(?3, extract_version),
						derive_version  = COALESCE(?4, derive_version)`,
					taggerID, o.FileID, o.ExtractVersion, o.DeriveVersion)
				if err != nil {
					return fmt.Errorf("file %d state: %w", o.FileID, err)
				}
			}

			if o.Tags != nil {
				encoded, err := encodeTags(o.Tags.Tags())
				if err != nil {
					return err
				}
				// updated_at moves only when the tags actually change.
				_, err = tx.ExecContext(ctx, `
					INSERT INTO tagger_tags (tagger_id, file_id, tags, updated_at)
					VALUES (?1, ?2, ?3, ?4)
					ON CONFLICT (tagger_id, file_id) DO UPDATE SET
						updated_at = CASE WHEN tags = excluded.tags THEN updated_at ELSE excluded.updated_at END,
						tags       = excluded.tags`,
					taggerID, o.FileID, encoded, now)
				if err != nil {
					return fmt.Errorf("file %d tags: %w", o.FileID, err)
				}
			}

			if o.PushedHash != "" {
				_, err := tx.ExecContext(ctx, `
					INSERT INTO pushes (tagger_id, file_id, first_pushed, last_pushed, tag_hash)
					VALUES (?1, ?2, ?3, ?3, ?4)
					ON CONFLICT (tagger_id, file_id) DO UPDATE SET
						last_pushed = excluded.last_pushed,
						tag_hash    = excluded.tag_hash`,
					taggerID, o.FileID, now, o.PushedHash)
				if err != nil {
					return fmt.Errorf("file %d push: %w", o.FileID, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record %s outcomes: %w", taggerID, err)
	}
	return nil
}

// encodeTags writes tags as a JSON array of readable UTF-8, the way the
// database backfill (json_group_array) and earlier versions wrote them, so an
// unchanged set compares equal byte for byte.
func encodeTags(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(tags); err != nil {
		return "", fmt.Errorf("encode tags: %w", err)
	}
	return string(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// decodeTags reads a stored tag list back into a set.
func decodeTags(raw string) (tagging.TagSet, error) {
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return tagging.TagSet{}, fmt.Errorf("stored tags are not a JSON list: %w", err)
	}
	return tagging.NewTagSet(tags), nil
}
