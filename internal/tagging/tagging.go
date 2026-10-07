// Package tagging holds the tagger abstractions and the host that runs them.
//
// A tagger answers two questions -- which files are mine, and what tags does
// this file get -- and the host owns everything else: discovery, identity
// caching, version gating, change detection, batching and reporting.
package tagging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
)

// TagSet is the tags one tagger derived for one file, plus the hash used for
// change detection.
//
// The hash is sha256(join("\n", sorted(tags))) over UTF-8, reproducing the
// original Python tool's tags_hash exactly; the pushes table holds hashes computed
// that way. Go sorts strings bytewise, and UTF-8 byte order is code point
// order, which is what Python's sort used.
//
// Duplicates are kept: the legacy builder could emit one tag twice and its
// hash counted both.
type TagSet struct {
	tags   []string
	sorted []string
	hash   string
}

// NewTagSet copies tags, so a caller reusing its slice cannot alter a set whose
// hash has been recorded.
func NewTagSet(tags []string) TagSet {
	own := append([]string(nil), tags...)
	sorted := append([]string(nil), tags...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return TagSet{tags: own, sorted: sorted, hash: hex.EncodeToString(sum[:])}
}

// Tags returns the tags in the order the tagger produced them.
func (t TagSet) Tags() []string { return t.tags }

// Sorted returns the tags in hash order.
func (t TagSet) Sorted() []string { return t.sorted }

// Hash is the lowercase hex SHA-256 of the newline-joined sorted tags.
func (t TagSet) Hash() string {
	if t.hash == "" {
		// Zero value: hash of the empty set.
		return NewTagSet(nil).hash
	}
	return t.hash
}

// Empty reports whether the set has no tags.
func (t TagSet) Empty() bool { return len(t.tags) == 0 }

// FileRef is a Hydrus file's identity: enough to find it on disk and address it
// in the API without carrying its full metadata.
type FileRef struct {
	FileID int
	Hash   string
	Ext    string
}

// RelativePath is the file's path under a Hydrus data directory, following its
// f<hash[:2]>/<hash>.<ext> layout.
func (f FileRef) RelativePath() (string, error) {
	if len(f.Hash) < 2 {
		return "", fmt.Errorf("file %d has hash %q, too short to form a Hydrus path", f.FileID, f.Hash)
	}
	return filepath.Join("f"+f.Hash[:2], f.Hash+"."+f.Ext), nil
}

// PathUnder is the file's absolute path under dataDir.
func (f FileRef) PathUnder(dataDir string) (string, error) {
	rel, err := f.RelativePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, rel), nil
}

// Tagger is the common shape of every tagger. The host runs FileTaggers; a
// tagger that reads files off disk also implements FileExtractor.
type Tagger interface {
	// ID is a stable identifier, e.g. "vrchat". It scopes the tagger's rows in
	// the database, so renaming one orphans its state.
	ID() string

	// DeriveVersion is bumped whenever the tag output could change; files
	// below the current version are re-derived. Cheap -- no disk I/O.
	DeriveVersion() int

	// SelectorQuery is the Hydrus search selecting candidates. Never empty.
	SelectorQuery() []string

	// RederiveEveryRun is for taggers whose input is live Hydrus metadata: a
	// file's URLs can change after it was first derived, and no version bump
	// would notice. The push ledger still suppresses unchanged sets.
	RederiveEveryRun() bool
}

// FileTagger decides one file's tags from that file alone, plus cached
// artifacts. Never touches disk.
type FileTagger interface {
	Tagger
	Derive(ctx context.Context, c *Context) (TagSet, error)
}

// FileExtractor is a tagger that must read file bytes off disk. Separately
// versioned because that read is the expensive part -- tens of thousands of
// files on a network share -- and should not be repeated just because parsing
// improved.
type FileExtractor interface {
	Tagger

	// ExtractVersion is bumped only when the on-disk read itself changes;
	// every bump costs a full re-read of the corpus.
	ExtractVersion() int

	// Extract reads file and caches whatever Derive will need. Returning an
	// error leaves the file unrecorded, so it is retried next run; use that
	// for environmental failures only.
	Extract(ctx context.Context, file FileRef) error
}

// Context is everything a tagger may consult about one file.
type Context struct {
	File     FileRef
	Metadata *hydrus.FileMetadata
}

// FileState is where one file stands with respect to one tagger. A file with no
// row has never been processed and is treated as version 0.
type FileState struct {
	ExtractVersion int
	DeriveVersion  int
}

// Outcome is what a run did to one file, for one tagger. Nil / empty fields
// leave the stored value unchanged, so outcomes can be recorded piecemeal.
type Outcome struct {
	FileID         int
	ExtractVersion *int
	DeriveVersion  *int
	Tags           *TagSet

	// PushedHash is the tag hash successfully pushed, or "" if nothing was.
	// Kept apart from Tags so a failed push does not record a hash that would
	// suppress the retry.
	PushedHash string
}

// Store is everything the host persists between runs.
type Store interface {
	// FileRefs returns known identity for the given ids; unknown ids are absent.
	FileRefs(ctx context.Context, fileIDs []int) (map[int]FileRef, error)

	// UpsertFileRefs records identity for files seen for the first time. Rows
	// that already exist are left alone.
	UpsertFileRefs(ctx context.Context, files []FileRef) error

	// FileStates returns per-file versions for one tagger.
	FileStates(ctx context.Context, taggerID string, fileIDs []int) (map[int]FileState, error)

	// PushedHashes returns the hash of the tag set last pushed, per file.
	PushedHashes(ctx context.Context, taggerID string, fileIDs []int) (map[int]string, error)

	// UnpushedTags returns stored tags whose hash does not match the push
	// ledger: never pushed, or pushed and then failed.
	UnpushedTags(ctx context.Context, taggerID string, fileIDs []int) (map[int]TagSet, error)

	// Record commits outcomes for one tagger atomically.
	Record(ctx context.Context, taggerID string, outcomes []Outcome) error
}
