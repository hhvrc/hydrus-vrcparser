# Parity check

Catches parser regressions against the real cached data in `vrchat.db` rather
than synthetic fixtures. Unit tests cover the cases we thought of; this covers
the ones we did not.

The Go parsers were verified this way against the C# port and the original
Python (identical on all 13,408 chunks and 12,437 files). Both are gone; use the
same procedure to compare the tool before and after any change to parsing or
tag building.

Always work on a **copy** of the database. The dumps contain VRChat user names
and ids; keep them out of the repository (`*.jsonl` is ignored).

```powershell
sqlite3 vrchat.db "VACUUM INTO 'C:/temp/parity.db'"

# Before the change (e.g. a build of the previous commit)
hydrus-tagger parity chunks C:/temp/parity.db C:/temp/before_chunks.jsonl
hydrus-tagger parity tags   C:/temp/parity.db C:/temp/before_tags.jsonl

# After the change
go run ./cmd/hydrus-tagger parity chunks C:/temp/parity.db C:/temp/after_chunks.jsonl
go run ./cmd/hydrus-tagger parity tags   C:/temp/parity.db C:/temp/after_tags.jsonl

python tools/parity/compare_chunks.py C:/temp/before_chunks.jsonl C:/temp/after_chunks.jsonl
python tools/parity/compare_tags.py   C:/temp/before_tags.jsonl   C:/temp/after_tags.jsonl
```

## Per-chunk comparison

Parses every cached iTXt chunk and diffs the normalized fields:
`effective_type`, `author_id`, `author_name`, `world_id`, `world_name`,
`instance_id`, `creator_tool`, `editor_software`, `created`, `players` and
`error`. Deliberately per chunk rather than per file, so a difference points at
a specific parser instead of at the priority contest.

About three quarters of chunks legitimately end in `MetaParseError`: Adobe XMP
packets from images that were never VRChat screenshots. Those failures must not
change either, which is why `error` is compared.

## Per-file tag comparison

The hard gate. Runs the whole derive path -- priority contest, editor
provenance, tag building -- and diffs the tags that would reach Hydrus, plus the
hash used for change detection.

A difference here is either a regression or an intended change. If intended,
bump the VRChat tagger's `DeriveVersion` so existing files are re-derived, and
expect the next run to push the changed files.

As a cross-check, every `tag_hash` in the dump should equal the one stored in
`pushes` for the `vrchat` tagger, unless tags were meant to change.
