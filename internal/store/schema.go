package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// baseVersion is the schema the C# port's last migration
// (BackfillVrchatTaggerState) left behind, and what freshSchema creates.
const baseVersion = 1

// schemaVersion is the PRAGMA user_version this build expects: the base plus
// one per migration. Add a step to migrations for every schema change.
func schemaVersion() int { return baseVersion + len(migrations) }

// lastCSharpMigration is the EF migration that produced schema version 1.
const lastCSharpMigration = "20261007142200_BackfillVrchatTaggerState"

// migrations[i] takes the schema from version baseVersion+i to
// baseVersion+i+1. Empty for now.
//
// Each step runs in one transaction with foreign keys enforced. A step that
// rebuilds a table (create new, copy, drop old, rename) must begin with
// PRAGMA defer_foreign_keys = ON and end with PRAGMA foreign_key_check, since
// foreign_keys itself cannot be switched off inside a transaction.
var migrations []func(ctx context.Context, tx *sql.Tx) error

// ErrLegacySchema means the database was last written by the original Python
// pipeline and never got the tagger-scoped schema (the C# port's
// AddTaggerScope migration, which this tool does not carry).
var ErrLegacySchema = errors.New("database uses the legacy Python schema")

func (db *DB) ensureSchema(ctx context.Context) error {
	var version int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	created := false
	if version == 0 {
		var err error
		if created, err = db.adoptOrCreate(ctx); err != nil {
			return err
		}
		version = baseVersion
	}

	target := schemaVersion()
	if version > target {
		return fmt.Errorf("database schema version %d is newer than this build supports (%d); update hydrus-tagger", version, target)
	}

	if version < target && !created {
		// Keep the pre-migration database to fall back on: a step can still
		// fail halfway in ways a rollback does not undo (a full disk, a
		// crash before the WAL is checkpointed).
		backup := fmt.Sprintf("%s.pre-v%d.bak", db.path, target)
		if err := Snapshot(ctx, db.path, backup); err != nil {
			return fmt.Errorf("back up before migrating: %w", err)
		}
	}

	// A new database starts at the base schema too, so it gets every
	// migration rather than skipping them.
	for v := version; v < target; v++ {
		step := migrations[v-baseVersion]
		err := db.withTx(ctx, func(tx *sql.Tx) error {
			if err := step(ctx, tx); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1))
			return err
		})
		if err != nil {
			return fmt.Errorf("migrate schema %d -> %d: %w", v, v+1, err)
		}
	}
	return nil
}

// adoptOrCreate handles a database with no Go schema version, leaving it at
// baseVersion: an empty file gets the base schema (created reports this); one
// the C# port fully migrated is adopted; anything else is refused rather than
// guessed at.
func (db *DB) adoptOrCreate(ctx context.Context) (created bool, err error) {
	tables, err := db.tableNames(ctx)
	if err != nil {
		return false, err
	}

	switch {
	case len(tables) == 0:
		err := db.withTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, freshSchema); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", baseVersion))
			return err
		})
		if err != nil {
			return false, fmt.Errorf("create schema: %w", err)
		}
		return true, nil

	case tables["tagger_file_state"] && tables["__EFMigrationsHistory"]:
		var last string
		err := db.sql.QueryRowContext(ctx,
			`SELECT MigrationId FROM "__EFMigrationsHistory" ORDER BY MigrationId DESC LIMIT 1`).Scan(&last)
		if err != nil {
			return false, fmt.Errorf("read C# migration history: %w", err)
		}
		if last != lastCSharpMigration {
			return false, fmt.Errorf("database was migrated by the C# hydrus-tagger to %s, but this build only knows up to %s", last, lastCSharpMigration)
		}
		if _, err := db.sql.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", baseVersion)); err != nil {
			return false, fmt.Errorf("adopt schema: %w", err)
		}
		return false, nil

	case tables["files"]:
		// The C# port that migrated these is gone; its last revision is in
		// git history (commit 2088b29) if one ever needs converting.
		return false, fmt.Errorf("%w: it predates the tagger-scoped schema and cannot be used", ErrLegacySchema)

	default:
		return false, errors.New("database has unrecognized tables; refusing to modify it")
	}
}

func (db *DB) tableNames(ctx context.Context) (map[string]bool, error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}

// freshSchema is schema version 1 for a new database: the tables this tool
// uses, as the C# migrations left them. The legacy-only tables (hash_tags,
// tag_mappings, hydrus_meta, schema_migrations) are not created.
const freshSchema = `
CREATE TABLE data_dirs (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL UNIQUE
);

CREATE TABLE files (
    file_id             INTEGER NOT NULL PRIMARY KEY,
    created_at          TEXT NOT NULL,
    data_dir_id         INTEGER NULL REFERENCES data_dirs (id),
    data_parser_version INTEGER NOT NULL DEFAULT 0,
    file_ext            TEXT NOT NULL,
    file_parser_version INTEGER NOT NULL DEFAULT 0,
    hash                TEXT NOT NULL UNIQUE,
    parsed_at           TEXT NULL,
    size                INTEGER NOT NULL
);
CREATE INDEX idx_files_data_dir_id ON files (data_dir_id);
CREATE INDEX idx_files_hash ON files (hash);

CREATE TABLE itxt_chunks (
    file_id            INTEGER NOT NULL REFERENCES files (file_id),
    seq                INTEGER NOT NULL,
    keyword            TEXT,
    compression_flag   INTEGER,
    compression_method INTEGER,
    language_tag       TEXT,
    translated_keyword TEXT,
    text               TEXT,
    content_type       TEXT NOT NULL DEFAULT 'text',
    PRIMARY KEY (file_id, seq)
);

CREATE TABLE tagger_file_state (
    tagger_id       TEXT NOT NULL,
    file_id         INTEGER NOT NULL REFERENCES files (file_id),
    extract_version INTEGER NOT NULL DEFAULT 0,
    derive_version  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tagger_id, file_id)
);
CREATE INDEX idx_tagger_file_state_file_id ON tagger_file_state (file_id);

CREATE TABLE tagger_tags (
    tagger_id  TEXT NOT NULL,
    file_id    INTEGER NOT NULL REFERENCES files (file_id),
    tags       TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (tagger_id, file_id)
);
CREATE INDEX idx_tagger_tags_file_id ON tagger_tags (file_id);

CREATE TABLE pushes (
    tagger_id    TEXT NOT NULL,
    file_id      INTEGER NOT NULL REFERENCES files (file_id),
    first_pushed TEXT NOT NULL,
    last_pushed  TEXT NOT NULL,
    tag_hash     TEXT NOT NULL,
    PRIMARY KEY (tagger_id, file_id)
);
CREATE INDEX idx_pushes_file_id ON pushes (file_id);
`
