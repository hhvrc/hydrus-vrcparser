package vrchat

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/hhvrc/hydrus-vrcparser/internal/store"
	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
)

// bom is written as bytes: editors and tools have been known to turn a \u
// escape for it into the literal character.
var bom = string([]byte{0xEF, 0xBB, 0xBF})

type taggerFixture struct {
	t       *testing.T
	db      *store.DB
	dataDir string
	file    tagging.FileRef
}

func newTaggerFixture(t *testing.T) *taggerFixture {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	file := tagging.FileRef{FileID: 1, Hash: "ab0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd", Ext: "png"}
	if err := db.UpsertFileRefs(context.Background(), []tagging.FileRef{file}); err != nil {
		t.Fatal(err)
	}
	return &taggerFixture{t: t, db: db, dataDir: t.TempDir(), file: file}
}

func (f *taggerFixture) tagger(dataDir string) *Tagger {
	return NewTagger(f.db, Options{DataDirectory: dataDir})
}

// writePNG writes a minimal PNG for the fixture's file under dir.
func (f *taggerFixture) writePNG(dir string, itxts ...[2]string) {
	f.t.Helper()
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'})
	for _, kv := range itxts {
		payload := kv[0] + "\x00\x00\x00\x00\x00" + kv[1] // keyword, flag, method, lang, trans, text
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
		b.Write(length[:])
		b.WriteString("iTXt")
		b.WriteString(payload)
		b.Write(make([]byte, 4)) // CRC; nothing verifies it
	}
	path, _ := f.file.PathUnder(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *taggerFixture) chunks() []store.Chunk {
	f.t.Helper()
	c, err := f.db.Chunks(context.Background(), f.file.FileID)
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *taggerFixture) derive(tg *Tagger) []string {
	f.t.Helper()
	tags, err := tg.Derive(context.Background(), &tagging.Context{File: f.file})
	if err != nil {
		f.t.Fatal(err)
	}
	return tags.Tags()
}

func TestTaggerKeepsTheLegacyVersions(t *testing.T) {
	// FILE_PARSER_VERSION and DATA_PARSER_VERSION, which the database backfill
	// carried over. Raising either redoes work already done.
	tg := NewTagger(nil, Options{})
	if tg.ID() != "vrchat" || tg.ExtractVersion() != 1 || tg.DeriveVersion() != 5 || tg.RederiveEveryRun() {
		t.Fatalf("id/versions changed: %s %d %d", tg.ID(), tg.ExtractVersion(), tg.DeriveVersion())
	}
	if !slices.Equal(tg.SelectorQuery(), DefaultSelectorQuery()) {
		t.Fatalf("selector = %v", tg.SelectorQuery())
	}
	custom := NewTagger(nil, Options{SelectorQuery: []string{"system:filetype is png"}})
	if !slices.Equal(custom.SelectorQuery(), []string{"system:filetype is png"}) {
		t.Fatalf("configured selector not used: %v", custom.SelectorQuery())
	}
}

func TestExtractCachesChunksWithTheirDetectedContentType(t *testing.T) {
	f := newTaggerFixture(t)
	f.writePNG(f.dataDir,
		[2]string{"Description", `{"application":"VRCX"}`},
		[2]string{"XML:com.adobe.xmp", "<x/>"},
		[2]string{"Comment", "created with GIMP"})

	if err := f.tagger(f.dataDir).Extract(context.Background(), f.file); err != nil {
		t.Fatal(err)
	}

	var types []string
	for _, c := range f.chunks() {
		types = append(types, c.ContentType)
	}
	if !slices.Equal(types, []string{ContentTypeJSON, ContentTypeXML, ContentTypeText}) {
		t.Fatalf("content types = %v", types)
	}
}

func TestExtractSanitizesTextBeforeDetectingItsFormat(t *testing.T) {
	// Some exporters prepend NULs or a BOM; legacy stripped them first, so
	// this payload was classified and stored as JSON, not text.
	f := newTaggerFixture(t)
	f.writePNG(f.dataDir, [2]string{"Description", "\x00" + bom + `{"application":"VRCX"}`})

	if err := f.tagger(f.dataDir).Extract(context.Background(), f.file); err != nil {
		t.Fatal(err)
	}

	c := f.chunks()
	if len(c) != 1 || c[0].ContentType != ContentTypeJSON || *c[0].Text != `{"application":"VRCX"}` {
		t.Fatalf("chunk = %+v", c)
	}
}

func TestExtractRecordsAPNGWithNoITXtAsAnAnswer(t *testing.T) {
	// "No metadata here" is cached so the next run does not re-read the file.
	f := newTaggerFixture(t)
	f.writePNG(f.dataDir)
	f.db.ReplaceChunks(context.Background(), f.file.FileID, []store.Chunk{{Seq: 0, ContentType: "text"}})

	if err := f.tagger(f.dataDir).Extract(context.Background(), f.file); err != nil {
		t.Fatal(err)
	}
	if c := f.chunks(); len(c) != 0 {
		t.Fatalf("stale chunks kept: %+v", c)
	}
}

func TestAMalformedFileIsRecordedNotRetriedForever(t *testing.T) {
	f := newTaggerFixture(t)
	path, _ := f.file.PathUnder(f.dataDir)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("definitely not a png"), 0o644)

	if err := f.tagger(f.dataDir).Extract(context.Background(), f.file); err != nil {
		t.Fatalf("malformed file should be recorded, got %v", err)
	}
	if c := f.chunks(); len(c) != 0 {
		t.Fatalf("chunks = %+v", c)
	}
}

func TestAMissingFileFailsSoItIsRetried(t *testing.T) {
	// A disconnected share must be retried, not recorded as "no metadata".
	f := newTaggerFixture(t)
	if err := f.tagger(f.dataDir).Extract(context.Background(), f.file); err == nil {
		t.Fatal("want an error for a missing file")
	}
}

func TestExtractNeedsADataDirectory(t *testing.T) {
	f := newTaggerFixture(t)
	if err := f.tagger("").Extract(context.Background(), f.file); err == nil {
		t.Fatal("want an error with no data directory")
	}
}

func TestExtractPrefersTheDirectoryRecordedAgainstTheFile(t *testing.T) {
	// Files the legacy pipeline found live where it recorded them, whatever
	// the current configuration says.
	f := newTaggerFixture(t)
	recorded := t.TempDir()
	f.writePNG(recorded, [2]string{"Description", `{"a":1}`})
	_, err := f.db.SQL().Exec(`INSERT INTO data_dirs (id, path) VALUES (1, ?); UPDATE files SET data_dir_id = 1;`, recorded)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.tagger(`X:\does-not-exist`).Extract(context.Background(), f.file); err != nil {
		t.Fatal(err)
	}
	if len(f.chunks()) != 1 {
		t.Fatal("did not read from the recorded directory")
	}
}

func TestDeriveBuildsTagsFromCachedChunks(t *testing.T) {
	f := newTaggerFixture(t)
	text := `{"author":{"id":"usr_a","displayName":"A"},"world":{"id":"wrld_b","name":"B","instanceId":""},"players":[]}`
	kw := "Description"
	f.db.ReplaceChunks(context.Background(), f.file.FileID, []store.Chunk{{Seq: 0, Keyword: &kw, Text: &text, ContentType: ContentTypeJSON}})

	tags := f.derive(f.tagger(f.dataDir))
	for _, want := range []string{"vrchat", "vrchat-author-id:usr_a", "vrchat-world-name:B"} {
		if !slices.Contains(tags, want) {
			t.Errorf("missing %q in %v", want, tags)
		}
	}
}

func TestDeriveGivesNoTagsWithoutVrchatMetadata(t *testing.T) {
	f := newTaggerFixture(t)
	if tags := f.derive(f.tagger(f.dataDir)); len(tags) != 0 {
		t.Fatalf("nothing cached, got %v", tags)
	}

	// The common case: chunks exist but are not VRChat's. An empty set means
	// the host pushes nothing.
	text, kw := "created with GIMP", "Description"
	f.db.ReplaceChunks(context.Background(), f.file.FileID, []store.Chunk{{Seq: 0, Keyword: &kw, Text: &text, ContentType: ContentTypeText}})
	if tags := f.derive(f.tagger(f.dataDir)); len(tags) != 0 {
		t.Fatalf("non-VRChat chunk, got %v", tags)
	}
}

func TestExtractThenDeriveRoundTripsThroughTheStore(t *testing.T) {
	f := newTaggerFixture(t)
	f.writePNG(f.dataDir, [2]string{"Description",
		`{"author":{"id":"usr_a","displayName":"A"},"world":{"id":"wrld_b","name":"B","instanceId":""},"players":[]}`})
	tg := f.tagger(f.dataDir)

	if err := tg.Extract(context.Background(), f.file); err != nil {
		t.Fatal(err)
	}
	if tags := f.derive(tg); !slices.Contains(tags, "vrchat-author-name:A") {
		t.Fatalf("tags = %v", tags)
	}
}

func TestFileTagsRepairsInvalidUTF8PerMaximalSubpart(t *testing.T) {
	// 0xC0 0xAF is two maximal subparts, so two U+FFFD as Python and .NET
	// gave; strings.ToValidUTF8 would collapse them into one.
	text := `{"author":{"id":"usr_a","displayName":"A` + "\xc0\xaf" + `B"},"world":{"id":"wrld_b","name":"W","instanceId":""},"players":[]}`
	kw := "Description"

	tags := FileTags([]store.Chunk{{Keyword: &kw, Text: &text, ContentType: ContentTypeJSON}}).Tags()

	replacement := string(utf8.RuneError)
	want := NSAuthorName + "A" + replacement + replacement + "B"
	if !slices.Contains(tags, want) {
		t.Fatalf("missing %q in %q", want, tags)
	}
}
