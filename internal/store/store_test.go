package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.now = func() time.Time { return time.Date(2026, 10, 7, 15, 0, 42, 331129000, time.UTC) }
	return db
}

func ptr[T any](v T) *T { return &v }

func fileRef(id int) tagging.FileRef {
	return tagging.FileRef{FileID: id, Hash: string(rune('a'+id)) + "0", Ext: "png"}
}

func TestFreshDatabaseGetsTheCurrentSchema(t *testing.T) {
	db := openTemp(t)

	var version int
	if err := db.sql.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion() {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion())
	}

	tables, err := db.tableNames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"files", "data_dirs", "itxt_chunks", "tagger_file_state", "tagger_tags", "pushes"} {
		if !tables[want] {
			t.Errorf("missing table %s", want)
		}
	}
}

func TestADatabaseTheCSharpPortMigratedIsAdopted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cs.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(freshSchema+`
		CREATE TABLE "__EFMigrationsHistory" ("MigrationId" TEXT NOT NULL PRIMARY KEY, "ProductVersion" TEXT NOT NULL);
		INSERT INTO "__EFMigrationsHistory" VALUES ('20260810224658_Baseline', '10.0.10'), (?, '10.0.10');`,
		lastCSharpMigration)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	defer db.Close()

	var version int
	db.sql.QueryRow("PRAGMA user_version").Scan(&version)
	if version != schemaVersion() {
		t.Fatalf("user_version = %d after adoption, want %d", version, schemaVersion())
	}
}

func TestALegacyPythonDatabaseIsRefusedNotGuessedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, _ := sql.Open("sqlite", path)
	_, err := raw.Exec(`CREATE TABLE files (file_id INTEGER PRIMARY KEY, hash TEXT)`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Open(context.Background(), path)
	if !errors.Is(err, ErrLegacySchema) {
		t.Fatalf("err = %v, want ErrLegacySchema", err)
	}
}

func TestUpsertAddsNewFilesAndLeavesLegacyRowsAlone(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	_, err := db.sql.Exec(`
		INSERT INTO data_dirs (id, path) VALUES (1, '\\host\files');
		INSERT INTO files (file_id, hash, file_ext, data_dir_id, created_at, size) VALUES (1, 'b0', 'png', 1, 'x', 42);`)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.UpsertFileRefs(ctx, []tagging.FileRef{fileRef(1), fileRef(2)}); err != nil {
		t.Fatal(err)
	}

	refs, err := db.FileRefs(ctx, []int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[2] != fileRef(2) {
		t.Fatalf("refs = %v", refs)
	}

	dir, _ := db.DataDirectory(ctx, 1)
	if dir != `\\host\files` {
		t.Errorf("legacy data dir = %q", dir)
	}
	if dir, _ := db.DataDirectory(ctx, 2); dir != "" {
		t.Errorf("new file has data dir %q, want none", dir)
	}
	var size int
	db.sql.QueryRow("SELECT size FROM files WHERE file_id = 1").Scan(&size)
	if size != 42 {
		t.Errorf("legacy size overwritten: %d", size)
	}
}

func TestRecordMergesOutcomesAndScopesThemByTagger(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	db.UpsertFileRefs(ctx, []tagging.FileRef{fileRef(1)})
	tags := tagging.NewTagSet([]string{"b", "a", "\u65e5\u672c\u8a9e"})

	must(t, db.Record(ctx, "t", []tagging.Outcome{{FileID: 1, ExtractVersion: ptr(2), DeriveVersion: ptr(3), Tags: &tags, PushedHash: tags.Hash()}}))
	must(t, db.Record(ctx, "t", []tagging.Outcome{{FileID: 1, DeriveVersion: ptr(4)}}))

	states, _ := db.FileStates(ctx, "t", []int{1})
	if states[1] != (tagging.FileState{ExtractVersion: 2, DeriveVersion: 4}) {
		t.Errorf("state = %+v", states[1])
	}
	pushed, _ := db.PushedHashes(ctx, "t", []int{1})
	if pushed[1] != tags.Hash() {
		t.Errorf("pushed hash = %q", pushed[1])
	}
	unpushed, _ := db.UnpushedTags(ctx, "t", []int{1})
	if len(unpushed) != 0 {
		t.Errorf("unpushed = %v", unpushed)
	}

	var stored string
	db.sql.QueryRow("SELECT tags FROM tagger_tags WHERE file_id = 1").Scan(&stored)
	if stored != "[\"b\",\"a\",\"\u65e5\u672c\u8a9e\"]" {
		t.Errorf("stored tags = %s, want readable UTF-8 in producer order", stored)
	}

	if other, _ := db.FileStates(ctx, "other", []int{1}); len(other) != 0 {
		t.Errorf("another tagger sees state: %v", other)
	}
}

func TestTagsWhoseHashMissesTheLedgerAreUnpushed(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	db.UpsertFileRefs(ctx, []tagging.FileRef{fileRef(1), fileRef(2)})
	x, y := tagging.NewTagSet([]string{"x"}), tagging.NewTagSet([]string{"y"})

	must(t, db.Record(ctx, "t", []tagging.Outcome{
		{FileID: 1, DeriveVersion: ptr(1), Tags: &x, PushedHash: x.Hash()},
		{FileID: 2, DeriveVersion: ptr(1), Tags: &y},
	}))

	unpushed, err := db.UnpushedTags(ctx, "t", []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := unpushed[2]; !ok || len(unpushed) != 1 {
		t.Fatalf("unpushed = %v, want only file 2", unpushed)
	}
}

func TestTimestampsUseTheLegacyFormat(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	db.UpsertFileRefs(ctx, []tagging.FileRef{fileRef(1)})

	var created string
	db.sql.QueryRow("SELECT created_at FROM files WHERE file_id = 1").Scan(&created)
	if created != "2026-10-07T15:00:42.331129+00:00" {
		t.Errorf("created_at = %q", created)
	}
}

func TestChunksRoundTripIncludingNulls(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	db.UpsertFileRefs(ctx, []tagging.FileRef{fileRef(1)})

	want := []Chunk{
		{Seq: 0, Keyword: ptr("Description"), CompressionFlag: ptr(0), CompressionMethod: ptr(0), LanguageTag: ptr(""), TranslatedKeyword: ptr(""), Text: ptr(`{"a":1}`), ContentType: "json"},
		{Seq: 1, ContentType: "text"},
	}
	must(t, db.ReplaceChunks(ctx, 1, want))
	must(t, db.ReplaceChunks(ctx, 1, want)) // replacing, not appending

	got, err := db.Chunks(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || *got[0].Text != `{"a":1}` || got[1].Keyword != nil || got[1].Text != nil {
		t.Fatalf("chunks = %+v", got)
	}

	must(t, db.ReplaceChunks(ctx, 1, nil))
	if got, _ := db.Chunks(ctx, 1); len(got) != 0 {
		t.Fatalf("empty replace left %d chunks", len(got))
	}
}

func TestSnapshotCopiesCommittedDataAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	db, err := Open(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	must(t, db.UpsertFileRefs(context.Background(), []tagging.FileRef{fileRef(1)}))

	// Snapshot while the source is still open, with data possibly in the WAL.
	dst := filepath.Join(dir, "snap.db")
	must(t, Snapshot(context.Background(), src, dst))
	db.Close()

	snap, err := OpenReadOnly(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if refs, _ := snap.FileRefs(context.Background(), []int{1}); len(refs) != 1 {
		t.Fatalf("snapshot missing data: %v", refs)
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
}

func TestStatusSummarizesFilesChunksAndEachTagger(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	must(t, db.UpsertFileRefs(ctx, []tagging.FileRef{fileRef(1), fileRef(2), fileRef(3)}))

	desc, xmp := "Description", "XML:com.adobe.xmp"
	must(t, db.ReplaceChunks(ctx, 1, []Chunk{{Seq: 0, Keyword: &desc, ContentType: "json"}}))
	must(t, db.ReplaceChunks(ctx, 2, []Chunk{{Seq: 0, Keyword: &xmp, ContentType: "text"}}))

	pushed, pending, empty := tagging.NewTagSet([]string{"a"}), tagging.NewTagSet([]string{"b"}), tagging.NewTagSet(nil)
	must(t, db.Record(ctx, "vrchat", []tagging.Outcome{
		{FileID: 1, ExtractVersion: ptr(1), DeriveVersion: ptr(5), Tags: &pushed, PushedHash: pushed.Hash()},
		{FileID: 2, ExtractVersion: ptr(1), DeriveVersion: ptr(5), Tags: &pending},
		{FileID: 3, ExtractVersion: ptr(1), DeriveVersion: ptr(5), Tags: &empty},
	}))

	s, err := db.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Files != 3 || s.Chunks != 2 || s.FilesWithChunks != 2 {
		t.Errorf("counts = %+v", s)
	}
	if len(s.UnparseableChunks) != 1 || s.UnparseableChunks[0] != (Count{Label: xmp, N: 1}) {
		t.Errorf("unparseable = %+v", s.UnparseableChunks)
	}
	if len(s.Taggers) != 1 {
		t.Fatalf("taggers = %+v", s.Taggers)
	}
	v := s.Taggers[0]
	if v.ID != "vrchat" || v.TagSets != 3 || v.EmptyTagSets != 1 || v.Pushed != 1 || v.Unpushed != 1 || v.ExtractedWithoutChunks != 1 {
		t.Errorf("vrchat status = %+v", v)
	}
	if len(v.Versions) != 1 || v.Versions[0] != (Count{Label: "1/5", N: 3}) {
		t.Errorf("versions = %+v", v.Versions)
	}
}

// withMigration installs one extra migration for the duration of a test.
func withMigration(t *testing.T, step func(ctx context.Context, tx *sql.Tx) error) {
	t.Helper()
	saved := migrations
	migrations = append(append([]func(context.Context, *sql.Tx) error(nil), saved...), step)
	t.Cleanup(func() { migrations = saved })
}

func TestAMigrationBacksUpThenUpgradesAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	must(t, db.UpsertFileRefs(context.Background(), []tagging.FileRef{fileRef(1)}))
	db.Close()

	withMigration(t, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "ALTER TABLE files ADD COLUMN note TEXT")
		return err
	})

	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer db.Close()

	var version int
	db.sql.QueryRow("PRAGMA user_version").Scan(&version)
	if version != baseVersion+1 {
		t.Fatalf("user_version = %d, want %d", version, baseVersion+1)
	}
	if _, err := db.sql.Exec("UPDATE files SET note = 'x'"); err != nil {
		t.Fatalf("migration not applied: %v", err)
	}

	backup := fmt.Sprintf("%s.pre-v%d.bak", path, baseVersion+1)
	bak, err := OpenReadOnly(context.Background(), backup)
	if err != nil {
		t.Fatalf("no pre-migration backup: %v", err)
	}
	defer bak.Close()
	if refs, _ := bak.FileRefs(context.Background(), []int{1}); len(refs) != 1 {
		t.Fatal("backup is missing data")
	}
}

func TestANewDatabaseGetsEveryMigrationWithoutABackup(t *testing.T) {
	// Creation stamps the base version, so later migrations still run.
	withMigration(t, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TABLE added_later (x INTEGER)")
		return err
	})
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if tables, _ := db.tableNames(context.Background()); !tables["added_later"] {
		t.Fatal("new database skipped the migration")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.bak")); len(matches) != 0 {
		t.Fatalf("backed up an empty database: %v", matches)
	}
}

func TestAFailedMigrationLeavesTheVersionAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fail.db")
	db, _ := Open(context.Background(), path)
	db.Close()

	withMigration(t, func(ctx context.Context, tx *sql.Tx) error { return errors.New("boom") })
	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("want the migration error")
	}

	raw, _ := sql.Open("sqlite", path)
	defer raw.Close()
	var version int
	raw.QueryRow("PRAGMA user_version").Scan(&version)
	if version != baseVersion {
		t.Fatalf("user_version = %d after a failed migration, want %d", version, baseVersion)
	}
}

func TestADatabaseFromANewerBuildIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	db, _ := Open(context.Background(), path)
	db.sql.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion()+1))
	db.Close()

	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("want an error for a newer schema")
	}
}

func TestADatabaseFromAnUnknownLaterCSharpMigrationIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cs-later.db")
	raw, _ := sql.Open("sqlite", path)
	_, err := raw.Exec(freshSchema + `
		CREATE TABLE "__EFMigrationsHistory" ("MigrationId" TEXT NOT NULL PRIMARY KEY, "ProductVersion" TEXT NOT NULL);
		INSERT INTO "__EFMigrationsHistory" VALUES ('29990101000000_Future', '10.0.10');`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("want an error for an unknown C# migration")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPathWithURISpecialCharactersOpensThatFile(t *testing.T) {
	// '#' would start a URI fragment and '%' an escape: unescaped, the first
	// silently opened a file named after the part before it.
	name := "a#b %20c"
	if runtime.GOOS != "windows" {
		name += "?d" // not allowed in Windows names
	}
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "v.db")

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	must(t, db.UpsertFileRefs(context.Background(), []tagging.FileRef{fileRef(1)}))
	db.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not at the given path: %v", err)
	}
	snap := filepath.Join(dir, "snap.db")
	must(t, Snapshot(context.Background(), path, snap))
	ro, err := OpenReadOnly(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if refs, _ := ro.FileRefs(context.Background(), []int{1}); len(refs) != 1 {
		t.Fatal("snapshot of a special-character path is missing data")
	}
}

func TestInChunksCoversEveryIDExactlyOnce(t *testing.T) {
	for _, n := range []int{0, 1, maxIDsPerQuery, maxIDsPerQuery + 1, 2*maxIDsPerQuery + 1} {
		ids := make([]int, n)
		for i := range ids {
			ids[i] = i
		}
		var seen []int
		must(t, inChunks(ids, func(chunk []int) error {
			if len(chunk) == 0 || len(chunk) > maxIDsPerQuery {
				t.Fatalf("n=%d: chunk of %d", n, len(chunk))
			}
			seen = append(seen, chunk...)
			return nil
		}))
		if len(seen) != n {
			t.Fatalf("n=%d: saw %d ids", n, len(seen))
		}
		for i, id := range seen {
			if id != i {
				t.Fatalf("n=%d: id %d at %d", n, id, i)
			}
		}
	}
}

func TestQueriesSpanMoreIDsThanOneINList(t *testing.T) {
	// Every real run asks about thousands of files at once.
	db := openTemp(t)
	ctx := context.Background()
	n := 2*maxIDsPerQuery + 1
	refs := make([]tagging.FileRef, n)
	ids := make([]int, n)
	outcomes := make([]tagging.Outcome, n)
	tags := tagging.NewTagSet([]string{"x"})
	for i := range refs {
		refs[i] = tagging.FileRef{FileID: i + 1, Hash: fmt.Sprintf("%064x", i+1), Ext: "png"}
		ids[i] = i + 1
		outcomes[i] = tagging.Outcome{FileID: i + 1, DeriveVersion: ptr(1), Tags: &tags}
	}
	must(t, db.UpsertFileRefs(ctx, refs))
	must(t, db.Record(ctx, "t", outcomes))

	got, err := db.FileRefs(ctx, ids)
	must(t, err)
	states, err := db.FileStates(ctx, "t", ids)
	must(t, err)
	unpushed, err := db.UnpushedTags(ctx, "t", ids)
	must(t, err)
	if len(got) != n || len(states) != n || len(unpushed) != n {
		t.Fatalf("refs=%d states=%d unpushed=%d, want %d each", len(got), len(states), len(unpushed), n)
	}
}

func TestRecordKeepsStoredFormatsStable(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	must(t, db.UpsertFileRefs(ctx, []tagging.FileRef{fileRef(1)}))
	at := func(min int) { db.now = func() time.Time { return time.Date(2026, 1, 1, 0, min, 0, 0, time.UTC) } }
	read := func(query string) string {
		var v string
		must(t, db.sql.QueryRow(query).Scan(&v))
		return v
	}

	tags := tagging.NewTagSet([]string{"a&b<c>"})
	at(0)
	must(t, db.Record(ctx, "t", []tagging.Outcome{{FileID: 1, Tags: &tags, PushedHash: tags.Hash()}}))
	if got := read("SELECT tags FROM tagger_tags"); got != `["a&b<c>"]` {
		t.Errorf("tags stored as %s; HTML characters must stay literal", got)
	}

	// Same tags again: updated_at stays. A re-push keeps first_pushed.
	at(5)
	must(t, db.Record(ctx, "t", []tagging.Outcome{{FileID: 1, Tags: &tags, PushedHash: tags.Hash()}}))
	if got := read("SELECT updated_at FROM tagger_tags"); got != "2026-01-01T00:00:00.000000+00:00" {
		t.Errorf("updated_at moved for unchanged tags: %s", got)
	}
	if got := read("SELECT first_pushed FROM pushes"); got != "2026-01-01T00:00:00.000000+00:00" {
		t.Errorf("first_pushed moved: %s", got)
	}
	if got := read("SELECT last_pushed FROM pushes"); got != "2026-01-01T00:05:00.000000+00:00" {
		t.Errorf("last_pushed not updated: %s", got)
	}

	changed := tagging.NewTagSet([]string{"z"})
	at(9)
	must(t, db.Record(ctx, "t", []tagging.Outcome{{FileID: 1, Tags: &changed}}))
	if got := read("SELECT updated_at FROM tagger_tags"); got != "2026-01-01T00:09:00.000000+00:00" {
		t.Errorf("updated_at did not move for changed tags: %s", got)
	}
	if got, _ := encodeTags(nil); got != emptyTagsJSON {
		t.Errorf("empty set encodes as %s, queries expect %s", got, emptyTagsJSON)
	}
}

func TestAHashStoredUnderAnotherFileIDIsReportedNotHidden(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	must(t, db.UpsertFileRefs(ctx, []tagging.FileRef{{FileID: 1, Hash: "aa", Ext: "png"}}))

	err := db.UpsertFileRefs(ctx, []tagging.FileRef{{FileID: 2, Hash: "aa", Ext: "png"}})
	if err == nil || !strings.Contains(err.Error(), "file 1") || !strings.Contains(err.Error(), "file 2") {
		t.Fatalf("err = %v, want both file ids named", err)
	}
	// Re-recording the same identity is fine.
	must(t, db.UpsertFileRefs(ctx, []tagging.FileRef{{FileID: 1, Hash: "aa", Ext: "png"}}))
}
