# CLAUDE.md

This file provides guidance to Claude Code and the GitHub Copilot CLI when working with code in this repository.

## Project Overview

VRChat metadata extraction tool for [Hydrus](https://hydrusnetwork.github.io/hydrus/) image management. Extracts embedded PNG metadata (iTXt chunks) from VRChat screenshots stored in a Hydrus database, parses them across three formats (JSON, XMP/XML, legacy pipe-delimited), normalizes to a common schema, and pushes standardized tags back to Hydrus.

## Running

```bash
# Activate the virtual environment
.venv\Scripts\activate

# Full pipeline with diagnostics
python hydrus-vrcparser.py

# With CLI arguments (overrides config.json, persists back)
python hydrus-vrcparser.py --api-key <KEY> --hydrus-addr http://localhost:45869 --data-dir <PATH> --service-name <NAME>

# Check database state and diagnostics
python check_db.py

# Analyze failures and recovery opportunities
python analyze_failures.py
```

## Building

```bash
# Build Windows executable via PyInstaller
pyinstaller hydrus-vrcparser.spec
# Output: dist/hydrus-vrcparser.exe
```

## Testing

```bash
# Full test suite
python -m unittest discover -s tests -v
```

## Linting

```bash
# flake8 (config in .flake8, max-line-length=120)
flake8
```

## Dependencies

Only external dependency is `hydrus_api` (pinned in `requirements.txt`). Everything else uses Python stdlib (`sqlite3`, `json`, `xml.etree.ElementTree`, `pathlib`, `logging`).

## Architecture

### Seven-Stage Pipeline

The main pipeline in `hydrus-vrcparser.py` runs seven sequential stages:

1. **Discover & Cache** -- `hydrus_io.get_exif_vrchat_file_rows()` queries Hydrus API for PNGs with embedded metadata; caches results in SQLite to avoid redundant API calls

2. **Extract iTXt** -- `core/png_itxt.py` extracts PNG iTXt chunks from files on disk; checks if chunks already cached and skips disk I/O if present; raw chunks stored in `itxt_chunks` table with content type discriminator

3. **Recover Broken Metadata** -- Scans `broken_metadata/` directory for previously failed files; retries with lenient parsing (skips malformed fields instead of failing completely)

4. **Parse & Normalize** -- `db_logic.db_load_all_parsed_meta()` reads chunks from database; dispatches to correct parser by content type (priority: JSON > XML > legacy line); normalizes all output via `_normalize_meta()` in `db_logic.py` to a common schema; field-level error handling allows partial recovery

5. **Build Tags** -- `core/tag_builders.py` generates Hydrus tag mappings from normalized metadata; creates per-file tag sets; stores in `tag_mappings` and `hash_tags` tables

6. **Push Tags** -- `hydrus_io.push_tags_batched_if_changed()` compares tags against SHA256 hashes in `pushes` table; only pushes changed tags; batches pushes to respect Hydrus API limits; graceful error handling prevents single failures from blocking batch

7. **Diagnostics** -- `db_logic.db_print_diagnostic_report()` prints comprehensive database state summary: file counts by parser version, iTXt chunks by type, unprocessed files, unparseable chunks

### Key Modules

| Module | Role |
|---|---|
| `cli.py` | Argument parsing and validation |
| `config_io.py` | JSON config load/save with CLI merge; None-safe path handling |
| `db_logic.py` | All SQLite operations: schema, migrations, queries, metadata normalization, analysis/recovery functions |
| `hydrus_io.py` | Hydrus API interactions (search, metadata fetch, tag push) |
| `core/png_itxt.py` | PNG binary parsing -- reads iTXt chunks from file bytes; content type detection |
| `core/meta_xmp_parser.py` | XMP/XML metadata parser (VRChat-specific RDF structure) |
| `core/meta_line_parser.py` | Legacy pipe-delimited format parser; lenient parsing (skips bad fields); handles bare `wrld_` segments |
| `core/tag_builders.py` | Generates tag mappings and per-file tag sets from normalized metadata |
| `core/constants.py` | Shared constants: FILE_PARSER_VERSION, DATA_PARSER_VERSION, batch sizes, PNG header bytes, iTXt keywords |
| `core/utils.py` | Utilities: UTC timestamps, list chunking, text sanitization |

### Database

SQLite with WAL mode and PRAGMA foreign_keys=ON. Schema created inline in `init_db()`. Migrations use function-name convention: functions named `_NNN_description` in `db_logic.py` are auto-discovered and applied via `schema_migrations` table.

**Key tables:**
- `files` -- Hydrus file records with `file_parser_version`, `data_parser_version`, and `parsed_at` for independent retry
- `itxt_chunks` -- Raw metadata chunks with `content_type` discriminator (`json`/`xml`/`line`/`text`)
- `hydrus_meta` -- Cached Hydrus metadata (hash, file_id, size, dimensions)
- `tag_mappings` / `hash_tags` -- Computed tag hierarchies and file-to-tag mappings
- `pushes` -- SHA256 hashes of pushed tag sets (change-detection)
- `data_dirs` -- Directory path mappings for file location
- `schema_migrations` -- Applied migration ledger

**Migrations:**
- `_001_add_content_type_to_itxt_chunks` -- Added content type discrimination; backfill from legacy `is_json`; validate XML; drop `is_json`
- `_002_add_parser_version_to_files` -- Added `parser_version` column
- `_003_split_parser_versions` -- Split into `file_parser_version` and `data_parser_version`
- `_004_reclassify_line_content_type` -- Reclassify `content_type='text'` Description chunks that parse as line format to `'line'`
- `_005_drop_legacy_file_columns` -- Drop `processed`, `parse_ok`, `parser_version` (superseded by two-tier versioning)

### Content Type System

The `content_type` column in `itxt_chunks` distinguishes metadata formats:

| content_type | Meaning |
|---|---|
| `json` | Valid JSON (VRCX format) |
| `xml` | Valid XMP/XML |
| `line` | Valid legacy pipe-delimited (screenshotmanager/lfs) |
| `text` | Unrecognized or non-VRC content (e.g. GIMP comments, Adobe XMP without VRC data) |

`_detect_format()` in `core/png_itxt.py` classifies new chunks. Migration 004 reclassified existing `text` chunks that are actually valid line format. The `db_find_unparseable_chunks()` query filters out non-VRC keywords (`Comment`, `Microsoft.GameDVR.*`, `parameters`) and null-keyword chunks.

### Two-Tier Parser Versioning

**FILE_PARSER_VERSION** (core/constants.py, currently 1):
- Tracks iTXt extraction state from PNG files (expensive disk I/O)
- Rarely changes; bump only for PNG parsing logic changes
- Files with FILE_PARSER_VERSION < current are re-extracted from disk
- Automatic retry on version bump

**DATA_PARSER_VERSION** (core/constants.py, currently 3):
- Tracks metadata normalization from cached iTXt chunks (CPU-bound)
- Changes frequently as parse logic improves
- Files with DATA_PARSER_VERSION < current are re-parsed from database
- **No disk I/O required** when bumped -- key optimization

**Why split?** Extraction (I/O) and parsing (CPU) are separate concerns. Can improve data parsing without re-extracting from slow disk. Caching iTXt chunks avoids redundant disk I/O on subsequent runs.

### Metadata Normalization

All three parsers (JSON, XMP, legacy line) produce dicts that are normalized by `_normalize_meta()` in `db_logic.py` to a common schema:

```python
{
    "author": {"id": str, "displayName": str},
    "world": {"id": str, "instanceId": str, "name": str},
    "position": {"x": float, "y": float, "z": float},
    "rq": int,  # render quality
    "players": [list of player dicts],  # Malformed entries skipped
    "type": str,
    "index": int,
    "created": str,  # ISO 8601 (XMP only)
}
```

Field-level error handling: if parsing author fails, other fields still recover. Lenient player parsing: skips malformed entries instead of failing entire metadata. The line parser handles bare `wrld_` segments (older screenshotmanager format that omits the `world:` prefix).

### Utility Scripts

**check_db.py** -- Prints diagnostic summary (file counts by version, iTXt chunks by type, migration status)

**analyze_failures.py** -- Analyzes failed files and unparseable chunks; suggests recovery opportunities

## Conventions

- Large query parameter lists are chunked to stay under SQLite's 999-variable limit (use `core/utils.chunked()`)
- Hydrus file paths follow pattern: `<data_dir>/f<hash[:2]>/<hash>.<ext>` (construct with `hydrus_path_for_hash()`)
- Broken/unparseable metadata saved to `broken_metadata/` directory for manual inspection; recovery process retries with lenient parsing
- Config merging: CLI args override `config.json` values; merged result persists back to config.json
- Error handling: Try/except around field parsers, API calls, service lookups; partial data better than complete loss
- Diagnostic output uses ASCII only (no emoji) to avoid Windows console encoding crashes

## Database Analysis Functions

```python
from db_logic import (
    db_print_diagnostic_report,      # Print formatted state summary
    db_get_state_summary,             # Return dict of counts/stats
    db_find_files_without_metadata,   # Find processed files with no VRC metadata
    db_find_unparseable_chunks,       # Find truly unparseable VRC chunks
    db_get_migration_status,          # Get applied migrations
    db_recover_broken_metadata,       # Retry recovery from broken_metadata/
)
```

## Separate Tool

`image_renamer.go` is an independent Go utility for renaming/organizing VRChat screenshots by date -- not part of the Python pipeline.
