package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// Snapshot writes a consistent copy of the database at src to dst, via
// VACUUM INTO so pages still in the WAL are included.
//
// The copy is written under a temporary name and renamed into place only when
// complete, so an interrupted snapshot can never be mistaken for a good one.
func Snapshot(ctx context.Context, src, dst string) error {
	dst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)

	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(30000)")
	db, err := sql.Open("sqlite", dsn(src, q))
	if err != nil {
		return fmt.Errorf("snapshot %s: %w", src, err)
	}
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", tmp)
	closeErr := db.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("snapshot %s: %w", src, err)
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, dst)
}
