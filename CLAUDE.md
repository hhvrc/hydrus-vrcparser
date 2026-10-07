# CLAUDE.md

Guidance for coding agents working in this repository.

## Project Overview

`hydrus-tagger` is a Go CLI that tags files in a [Hydrus](https://hydrusnetwork.github.io/hydrus/) client: VRChat screenshot metadata from PNG iTXt chunks (three formats: VRCX JSON, VRChat XMP, legacy pipe-delimited "line"), and Twitter/X account handles from known URLs. It keeps its state in a SQLite cache (`vrchat.db`) and only ever adds tags.

History: it began as a Python tool, was ported to C#, then to Go. The VRChat parsers were verified byte-for-byte against both predecessors over the real cache (13,408 chunks, 12,437 files). The C# and Python are gone; git history has them (the last C# revision is commit 2088b29).

## Commands

```bash
go build -o dist/hydrus-tagger.exe ./cmd/hydrus-tagger
go test ./...
go vet ./...
gofmt -l .        # must print nothing

hydrus-tagger run [--dry-run] [--only vrchat,twitter]
hydrus-tagger status
hydrus-tagger correlate
hydrus-tagger parity chunks|tags <db> <out.jsonl>
```

Settings: `%APPDATA%\hydrus-tagger\config.json` (`api_key`, `service_name`, `database`, `data_directory`, ...), then `HYDRUS_API_KEY` / `HYDRUS_ADDRESS`, then flags. Unknown config keys are errors. There is no default `data_directory` (it is machine-specific). The API key is never written anywhere by the tool and must never be committed.

## Layout

| Package | Role |
|---|---|
| `cmd/hydrus-tagger` | CLI: config loading, subcommands, report printing |
| `internal/tagging` | `Tagger` / `FileTagger` / `FileExtractor` interfaces, `TagSet` (+ change-detection hash), and `Host`, which runs taggers |
| `internal/store` | SQLite: schema versioning, tagger state store, VRChat chunk cache, snapshots, `status` queries |
| `internal/hydrus` | Hydrus client API: search (with `a OR b` terms), metadata, add_tags; retries and typed errors |
| `internal/png` | PNG iTXt reader |
| `internal/vrchat` | Content-type detection, line/XMP/JSON parsers, normalizer, loader (format priority contest), tag builder, and the VRChat tagger |
| `internal/twitter` | URL-to-handle parsing and the Twitter tagger |
| `internal/correlate` | Infers which `vrchat-user-id` and `vrchat-user-name` tags belong together |
| `internal/parity` | JSONL dumps of parser output, for regression comparison (`tools/parity`) |

## How a run works

`tagging.Host.Run` per tagger: discover (Hydrus search) -> resolve file identity (cached in `files`, metadata fetched only for unseen files) -> extract (`FileExtractor` only; files below `ExtractVersion`) -> derive (files below `DeriveVersion`, or all if `RederiveEveryRun`) -> push (tag sets whose hash differs from `pushes`, grouped by identical set).

Rules the host relies on -- keep them:
- **Progress is saved incrementally**: after each extract chunk (500 files), after derive, after each accepted push batch, and on the way out of a failed or cancelled run (`context.WithoutCancel`). A cancelled run must never redo or re-push what it already did.
- **Only real cancellation stops a run.** A per-file or per-batch error is counted and reported; check `ctx.Err()` to tell the two apart. The Hydrus client never returns a timeout as `context.Canceled`.
- **The tag service is resolved once.** The CLI resolves it in a preflight and passes the key in (`RunOptions.TagServiceKey`); without one, the host resolves it lazily, only when there is something to push.
- **Nothing is touched before a run can succeed**: the API key, `--only` names, Hydrus reachability and the tag service are all checked before the database is opened.
- **Two-tier versioning**: `ExtractVersion` covers the expensive disk read; `DeriveVersion` covers parsing from cached chunks. Bump the derive version when tag output could change; bump the extract version only when the on-disk read changes (it costs a full re-read of the share). The VRChat tagger's versions (1 and 5) match the legacy Python's, which the database carries.

## Database

SQLite, WAL, foreign keys on. Schema versioned with `PRAGMA user_version`; `baseVersion` (1) is the schema the C# port's last EF migration left behind. Add a schema change by appending a step to `migrations` in `internal/store/schema.go`; `Open` snapshots the database to `<db>.pre-v<N>.bak` first. A new database is created at the base version and migrated forward.

Tables in use: `files` (identity; `data_dir_id` set only for files the legacy pipeline found on disk), `data_dirs`, `itxt_chunks` (VRChat cache; `content_type` json/xml/line/text), `tagger_file_state`, `tagger_tags` (JSON list of tags as derived), `pushes` (per tagger, file: hash last pushed). `hash_tags`, `tag_mappings`, `hydrus_meta`, `schema_migrations` and the EF history tables are legacy and unused.

## Compatibility constraints

- **Tag hash**: `sha256(join("\n", sorted(tags)))`, duplicates kept, bytewise sort (= Python's code point order). Changing it re-pushes every file.
- **Stored formats**: timestamps `2006-01-02T15:04:05.000000+00:00` UTC; tag JSON as readable UTF-8.
- **Parser parity**: before and after any parser change, run `hydrus-tagger parity chunks|tags` on a copy of the database and diff with `tools/parity/compare_*.py`. Unexplained differences are regressions.

## Conventions

- Comments explain why, not what; many record legacy quirks the parsers deliberately preserve.
- Console output is ASCII only (Windows consoles have crashed on emoji).
- Go source is ASCII and LF (`.gitattributes`); CI enforces ASCII. The file-writing tool has turned `\uXXXX` escapes into literal characters before -- grep for non-ASCII bytes after editing, and remember escapes do nothing inside backtick strings.
- CI runs `go test -race`. cgo may be off on a dev machine, so races may only show up there.
- Keep IN lists under 900 ids (`inChunks`).
- Hydrus file paths: `<data_dir>/f<hash[:2]>/<hash>.<ext>` (`FileRef.PathUnder`).

## Separate tool

`image_renamer/` is an independent Go utility for renaming VRChat screenshots by date; not part of `hydrus-tagger`.
