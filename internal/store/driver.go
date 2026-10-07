// Package store persists tagger state in the SQLite cache database
// (vrchat.db): file identity, per-tagger versions, derived tags, the push
// ledger, and the VRChat tagger's cached iTXt chunks.
//
// The schema is the one the C# hydrus-tagger's EF migrations produced
// (Baseline, AddTaggerScope, BackfillVrchatTaggerState), so both tools can
// share a database during the switch-over. From here on the Go tool owns the
// schema and versions it with PRAGMA user_version.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	// Pure-Go SQLite: no cgo, so the tool builds to one static exe.
	_ "modernc.org/sqlite"
)

// maxIDsPerQuery keeps IN lists under SQLite's historical 999-variable limit,
// as the legacy code did.
const maxIDsPerQuery = 900

// timestampLayout matches what the legacy Python and the C# port wrote:
// microseconds, explicit +00:00 offset, always UTC.
const timestampLayout = "2006-01-02T15:04:05.000000-07:00"

func formatTime(t time.Time) string { return t.UTC().Format(timestampLayout) }

// DB is an open cache database.
type DB struct {
	sql  *sql.DB
	path string

	// SQLite takes one writer at a time. Serializing writes here keeps
	// concurrent extraction from queueing on busy_timeout; the parallelism
	// that matters is the network read, not the local insert.
	writeMu sync.Mutex

	now func() time.Time
}

// Open opens (creating if needed) the database at path and brings its schema
// to the current version.
func Open(ctx context.Context, path string) (*DB, error) {
	return open(ctx, path, false)
}

// OpenReadOnly opens an existing database without changing anything,
// including its schema. Used by the parity dumpers and diagnostics.
func OpenReadOnly(ctx context.Context, path string) (*DB, error) {
	return open(ctx, path, true)
}

func open(ctx context.Context, path string, readOnly bool) (*DB, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(30000)")
	if readOnly {
		q.Set("mode", "ro")
	} else {
		// WAL keeps readers unblocked while a long run writes; NORMAL is the
		// usual WAL pairing. Same pragmas the legacy Python set.
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
	}

	sqlDB, err := sql.Open("sqlite", dsn(path, q))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	db := &DB{sql: sqlDB, path: path, now: time.Now}
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	if !readOnly {
		if err := db.ensureSchema(ctx); err != nil {
			sqlDB.Close()
			return nil, err
		}
	}
	return db, nil
}

// Close closes the database.
func (db *DB) Close() error { return db.sql.Close() }

// SQL exposes the underlying handle for read-only tooling (parity dumps).
func (db *DB) SQL() *sql.DB { return db.sql }

// uriPathEscaper escapes what a SQLite URI filename treats specially: '%'
// starts an escape, '?' the query and '#' a fragment. Unescaped, a '#' in a
// directory name silently opens a different file.
var uriPathEscaper = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

// dsn builds the driver's URI for a database file. The one place a path
// becomes a URI, so every opener escapes it the same way.
func dsn(path string, q url.Values) string {
	return "file:" + uriPathEscaper.Replace(filepath.ToSlash(path)) + "?" + q.Encode()
}

// inChunks runs fn over ids in slices small enough for one IN list.
func inChunks(ids []int, fn func(chunk []int) error) error {
	for start := 0; start < len(ids); start += maxIDsPerQuery {
		end := min(start+maxIDsPerQuery, len(ids))
		if err := fn(ids[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// placeholders returns "?,?,?" with one mark per id, and ids as query arguments.
func placeholders(ids []int) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// withTx runs fn in a write transaction, serialized with other writers.
func (db *DB) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	db.writeMu.Lock()
	defer db.writeMu.Unlock()

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}
