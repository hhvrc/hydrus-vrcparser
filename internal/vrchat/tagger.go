package vrchat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/hhvrc/hydrus-vrcparser/internal/png"
	"github.com/hhvrc/hydrus-vrcparser/internal/store"
	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
)

// TaggerID scopes this tagger's rows in the database.
const TaggerID = "vrchat"

// DefaultSelectorQuery selects candidate screenshots: the same query the
// legacy pipeline ran.
func DefaultSelectorQuery() []string {
	return []string{"system:filetype is png", "system:has embedded metadata"}
}

// ChunkStore is the tagger's cache of iTXt chunks. Implemented by *store.DB.
type ChunkStore interface {
	Chunks(ctx context.Context, fileID int) ([]store.Chunk, error)
	ReplaceChunks(ctx context.Context, fileID int, chunks []store.Chunk) error
	DataDirectory(ctx context.Context, fileID int) (string, error)
}

// Options configures the tagger.
type Options struct {
	// DataDirectory is the Hydrus files directory, used only for files with
	// no directory recorded against them.
	DataDirectory string

	// SelectorQuery overrides DefaultSelectorQuery when non-empty.
	SelectorQuery []string

	Logger *slog.Logger
}

// Tagger reads VRChat metadata out of PNG iTXt chunks and turns it into tags.
//
// Split across both stages the host offers: extraction reads bytes off a
// network share and is versioned separately for that reason; deriving works
// entirely from the cached chunks, so improving the parsers costs nothing but
// CPU.
type Tagger struct {
	store ChunkStore
	opts  Options
	log   *slog.Logger
}

var (
	_ tagging.FileTagger    = (*Tagger)(nil)
	_ tagging.FileExtractor = (*Tagger)(nil)
)

// NewTagger builds the tagger over a chunk store.
func NewTagger(s ChunkStore, opts Options) *Tagger {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Tagger{store: s, opts: opts, log: log}
}

func (t *Tagger) ID() string { return TaggerID }

// ExtractVersion matches the legacy FILE_PARSER_VERSION, which the database
// backfill carried over, so files already read are not re-read off the share.
func (t *Tagger) ExtractVersion() int { return 1 }

// DeriveVersion matches the legacy DATA_PARSER_VERSION, which the database
// backfill carried over. v5
// recovered VRCX JSON embedded in the dc:description of Adobe-edited
// screenshots.
func (t *Tagger) DeriveVersion() int { return 5 }

func (t *Tagger) RederiveEveryRun() bool { return false }

func (t *Tagger) SelectorQuery() []string {
	if len(t.opts.SelectorQuery) > 0 {
		return append([]string(nil), t.opts.SelectorQuery...)
	}
	return DefaultSelectorQuery()
}

// Extract reads the file's iTXt chunks into the cache.
func (t *Tagger) Extract(ctx context.Context, file tagging.FileRef) error {
	dir, err := t.store.DataDirectory(ctx, file.FileID)
	if err != nil {
		return err
	}
	if dir == "" {
		dir = t.opts.DataDirectory
	}
	if strings.TrimSpace(dir) == "" {
		return errors.New("no data directory configured")
	}

	path, err := file.PathUnder(dir)
	if err != nil {
		return err
	}
	result := png.ReadFile(path)

	if result.IOError {
		// Environmental -- a disconnected share, a file Hydrus moved. Leaving
		// the file unrecorded retries it next run.
		return fmt.Errorf("read %s: %w", path, result.Err)
	}
	if result.Err != nil {
		// A malformed file stays malformed. Record what was read, as the
		// legacy pipeline did, rather than re-reading it every run forever.
		t.log.Warn("malformed PNG", "path", path, "err", result.Err)
	}

	chunks := make([]store.Chunk, 0, len(result.Records))
	for _, rec := range result.Records {
		if rec.Unparseable {
			continue
		}

		// Sanitized before detection and storage, as the original Python did: some
		// exporters prepend a BOM or NULs that would defeat format sniffing.
		text := SanitizeITXt(rec.Text)
		contentType, ok := DetectContentType(text, rec.Keyword)
		if !ok {
			contentType = ContentTypeText
		}

		chunks = append(chunks, store.Chunk{
			Seq:               rec.Seq,
			Keyword:           &rec.Keyword,
			CompressionFlag:   &rec.CompressionFlag,
			CompressionMethod: &rec.CompressionMethod,
			LanguageTag:       &rec.LanguageTag,
			TranslatedKeyword: &rec.TranslatedKeyword,
			Text:              &text,
			ContentType:       contentType,
		})
	}

	// Written even when empty: that is a real answer -- this PNG carries no
	// iTXt -- and recording it is what stops the next run re-reading it.
	return t.store.ReplaceChunks(ctx, file.FileID, chunks)
}

// Derive turns the cached chunks into tags. No chunk yielding VRChat metadata
// is common and not an error: most cached chunks are Adobe XMP packets from
// images that were never VRChat screenshots.
func (t *Tagger) Derive(ctx context.Context, c *tagging.Context) (tagging.TagSet, error) {
	stored, err := t.store.Chunks(ctx, c.File.FileID)
	if err != nil {
		return tagging.TagSet{}, err
	}
	return FileTags(stored), nil
}

// FileTags derives one file's tags from its cached chunks: the whole
// chunk-to-tags pipeline, shared by Derive and the parity dump so the two
// cannot drift. The set is empty exactly when no chunk yielded VRChat
// metadata; otherwise it holds at least the "vrchat" tag.
func FileTags(stored []store.Chunk) tagging.TagSet {
	if len(stored) == 0 {
		return tagging.NewTagSet(nil)
	}

	chunks := make([]Chunk, len(stored))
	for i, s := range stored {
		chunks[i] = Chunk{
			Keyword:     strOrEmpty(s.Keyword),
			Text:        validUTF8(strOrEmpty(s.Text)),
			ContentType: s.ContentType,
		}
	}

	meta := Load(chunks)
	if meta == nil {
		return tagging.NewTagSet(nil)
	}
	return tagging.NewTagSet(BuildFileTags(meta))
}

// validUTF8 repairs text that is not valid UTF-8 the way the chunk reader
// decodes it (one U+FFFD per maximal subpart, as Python and .NET did), so a
// tag never carries bytes the original tools never produced.
func validUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return png.DecodeUTF8([]byte(s))
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
