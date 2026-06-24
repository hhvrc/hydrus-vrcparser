#!/usr/bin/env python3
import json
import logging
import sqlite3
import sys
from pathlib import Path
from typing import Callable, Dict, Iterable, List, Optional, Tuple, Set, Any
import re
import xml.etree.ElementTree as ET

from core.constants import (
    ITXT_KEY_DESCRIPTION, ITXT_KEY_ADOBEXMPXML,
    FILE_PARSER_VERSION, DATA_PARSER_VERSION, BROKEN_DIR,
)
from core.utils import chunked, now_utc_iso, sanitize_itxt_text
from core.meta_line_parser import parse_meta_line
from core.meta_xmp_parser import (
    parse_xmp_meta, XMPParseError, extract_embedded_vrcx_json, extract_editor_software,
)


def hydrus_path_for_hash(data_dir: Path, h: str, ext: str = "png") -> Path:
    """Hydrus local file path for a given SHA256 hash and extension."""
    return data_dir / f"f{h[:2]}" / f"{h}.{ext.lstrip('.').lower()}"


# ─── Migration framework (name/id derived from function names) ────────────
# Table schema_migrations(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, applied_at TEXT NOT NULL)
# Function name pattern:  _001_some_migration  → id=1, name="001_some_migration"
_MIGRATION_FN_RE = re.compile(r"^_(\d{3,})_(.+)$")


def _ensure_migration_table(conn: sqlite3.Connection) -> None:
    with conn:
        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS schema_migrations(
                id         INTEGER PRIMARY KEY,
                name       TEXT NOT NULL UNIQUE,
                applied_at TEXT NOT NULL
            )
            """
        )


def _discover_migrations(namespace: dict) -> List[Tuple[int, str, Callable[[sqlite3.Connection], None]]]:
    migs: List[Tuple[int, str, Callable[[sqlite3.Connection], None]]] = []
    for fname, obj in namespace.items():
        if not callable(obj):
            continue
        m = _MIGRATION_FN_RE.match(fname)
        if not m:
            continue
        id_num = int(m.group(1))
        name = f"{m.group(1)}_{m.group(2)}"
        migs.append((id_num, name, obj))
    migs.sort(key=lambda t: t[0])
    return migs


def run_migrations(conn: sqlite3.Connection, namespace: dict) -> None:
    _ensure_migration_table(conn)
    applied_ids: Set[int] = {int(r["id"]) for r in conn.execute("SELECT id FROM schema_migrations").fetchall()}
    pending = [(mid, mname, mfunc) for mid, mname, mfunc in _discover_migrations(namespace) if mid not in applied_ids]

    for mid, mname, mfunc in pending:
        logging.info("Applying migration: %s", mname)
        try:
            # One transaction per migration
            with conn:
                mfunc(conn)
                conn.execute(
                    "INSERT INTO schema_migrations(id, name, applied_at) VALUES(?, ?, ?)",
                    (mid, mname, now_utc_iso()),
                )
            logging.info("Applied migration: %s", mname)
        except Exception:
            # Context manager rolls back this migration automatically
            logging.exception("Migration %s failed; changes rolled back.", mname)
            raise


# ─── Helpers ────────────────────────────────────────────
def _is_xmp_xml(text: str) -> bool:
    """Check if text is valid XMP XML by parsing it."""
    if not text or not text.lstrip().startswith("<"):
        return False
    try:
        root = ET.fromstring(text)
        tag = root.tag
        return tag.endswith("}xmpmeta") or tag.endswith("}RDF")
    except ET.ParseError:
        return False


def _is_parsable_xml(s: str) -> bool:
    """True if the string is well-formed XML (after trimming)."""
    if not s:
        return False
    t = s.lstrip()
    if not t.startswith("<"):
        return False
    try:
        ET.fromstring(t)
        return True
    except ET.ParseError:
        return False


# ─── SQLite (init schema directly; migrations are for future changes) ──────────
def init_db(db_path: Path) -> sqlite3.Connection:
    """
    Connect and initialize the final schema (no legacy migrations required).
    Migrations table exists for future changes.
    """
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    with conn:
        conn.execute("PRAGMA foreign_keys = ON;")
        conn.execute("PRAGMA journal_mode=WAL;")
        conn.execute("PRAGMA synchronous=NORMAL;")

        # Only guarantee the migrations ledger first (so future runs can apply new migrations)
        _ensure_migration_table(conn)

        # Final schema — created outside of migrations as requested
        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS data_dirs (
                id   INTEGER PRIMARY KEY AUTOINCREMENT,
                path TEXT NOT NULL UNIQUE
            )
            """
        )

        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS files (
                file_id              INTEGER PRIMARY KEY,   -- Hydrus file_id
                hash                 TEXT NOT NULL UNIQUE,  -- Hydrus SHA256 hex
                file_ext             TEXT NOT NULL,         -- e.g. 'png'
                data_dir_id          INTEGER NOT NULL,      -- FK to data_dirs
                created_at           TEXT NOT NULL,
                parsed_at            TEXT,
                size                 INTEGER NOT NULL,
                file_parser_version  INTEGER NOT NULL DEFAULT 0,
                data_parser_version  INTEGER NOT NULL DEFAULT 0,
                FOREIGN KEY(data_dir_id) REFERENCES data_dirs(id)
            )
            """
        )
        conn.execute("CREATE INDEX IF NOT EXISTS idx_files_hash ON files(hash)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_files_data_dir_id ON files(data_dir_id)")

        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS itxt_chunks (
                file_id            INTEGER NOT NULL,
                seq                INTEGER NOT NULL,
                keyword            TEXT,
                compression_flag   INTEGER,
                compression_method INTEGER,
                language_tag       TEXT,
                translated_keyword TEXT,
                text               TEXT,
                content_type       TEXT NOT NULL DEFAULT 'text',  -- 'text' | 'json' | 'xml' | ...
                PRIMARY KEY (file_id, seq),
                FOREIGN KEY(file_id) REFERENCES files(file_id)
            )
            """
        )

        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS hydrus_meta (
                file_id INTEGER PRIMARY KEY,
                width   INTEGER,
                height  INTEGER,
                has_transparency INTEGER NOT NULL DEFAULT 0,
                has_human_readable_embedded_metadata INTEGER NOT NULL DEFAULT 0,
                updated_at TEXT NOT NULL,
                FOREIGN KEY(file_id) REFERENCES files(file_id)
            )
            """
        )

        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS tag_mappings (
                parent TEXT NOT NULL,
                child  TEXT NOT NULL,
                PRIMARY KEY (parent, child)
            )
            """
        )

        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS hash_tags (
                file_id INTEGER NOT NULL,
                tag     TEXT NOT NULL,
                PRIMARY KEY (file_id, tag),
                FOREIGN KEY(file_id) REFERENCES files(file_id)
            )
            """
        )
        conn.execute("CREATE INDEX IF NOT EXISTS idx_hash_tags_file_id ON hash_tags(file_id)")

        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS pushes (
                file_id      INTEGER PRIMARY KEY,
                tag_hash     TEXT NOT NULL,
                first_pushed TEXT NOT NULL,
                last_pushed  TEXT NOT NULL,
                FOREIGN KEY(file_id) REFERENCES files(file_id)
            )
            """
        )

    # Apply migrations
    run_migrations(conn, globals())
    return conn


# ─── Migration: add content_type, backfill, validate XML, then DROP is_json ───
def _001_add_content_type_to_itxt_chunks(conn: sqlite3.Connection) -> None:
    """
    Steps:
      1) Ensure content_type column exists.
      2) Backfill from legacy is_json → 'json' or 'text'.
      3) For non-JSON rows, set to 'xml' ONLY if sanitized text parses as XML.
      4) Persist sanitized text for XML rows.
      5) DROP legacy is_json column.
    """
    cols = {row[1] for row in conn.execute("PRAGMA table_info(itxt_chunks)").fetchall()}

    # 1) Ensure content_type
    if "content_type" not in cols:
        with conn:
            conn.execute("ALTER TABLE itxt_chunks ADD COLUMN content_type TEXT NOT NULL DEFAULT 'text'")

    # 2) Backfill from is_json if present
    if "is_json" in cols:
        with conn:
            conn.execute("""
                UPDATE itxt_chunks
                   SET content_type = CASE WHEN ifnull(is_json,0)=1 THEN 'json' ELSE 'text' END
            """)

    # 3–4) Validate XML for non-JSON rows; sanitize + persist
    with conn:
        rows = conn.execute("SELECT file_id, seq, text, content_type FROM itxt_chunks").fetchall()
        for r in rows:
            ctype = (r["content_type"] or "text").lower()
            if ctype == "json":
                continue
            raw = sanitize_itxt_text(r["text"])
            if _is_parsable_xml(raw):
                conn.execute(
                    "UPDATE itxt_chunks SET content_type = 'xml', text = ? WHERE file_id = ? AND seq = ?",
                    (raw, int(r["file_id"]), int(r["seq"])),
                )

    # 5) Drop legacy column if it exists
    if "is_json" in cols:
        with conn:
            conn.execute("ALTER TABLE itxt_chunks DROP COLUMN is_json")


def _002_add_parser_version_to_files(conn: sqlite3.Connection) -> None:
    """Add parser_version column to files table for automatic retry on parser updates."""
    cols = {row[1] for row in conn.execute("PRAGMA table_info(files)").fetchall()}
    if "parser_version" not in cols:
        conn.execute("ALTER TABLE files ADD COLUMN parser_version INTEGER NOT NULL DEFAULT 0")


def _003_split_parser_versions(conn: sqlite3.Connection) -> None:
    """
    Split parser versioning into two concerns:
    - file_parser_version: Tracks iTXt extraction from PNG files (expensive I/O, rarely changes)
    - data_parser_version: Tracks metadata parsing from cached iTXt chunks (changes frequently)

    Backfill strategy:
    - file_parser_version = 1 for all successfully extracted files (fresh concept, new starting point)
    - data_parser_version = old parser_version value (preserves parse history; typically 0 or 2)
    """
    cols = {row[1] for row in conn.execute("PRAGMA table_info(files)").fetchall()}

    if "file_parser_version" not in cols:
        conn.execute("ALTER TABLE files ADD COLUMN file_parser_version INTEGER NOT NULL DEFAULT 0")
    if "data_parser_version" not in cols:
        conn.execute("ALTER TABLE files ADD COLUMN data_parser_version INTEGER NOT NULL DEFAULT 0")

    # Backfill: Preserve old parser_version as data_parser_version (only if column exists)
    if "parser_version" in cols:
        with conn:
            conn.execute("""
                UPDATE files
                SET file_parser_version = CASE WHEN parser_version > 0 THEN 1 ELSE 0 END,
                    data_parser_version = parser_version
                WHERE file_parser_version = 0 AND data_parser_version = 0
            """)


def _004_reclassify_line_content_type(conn: sqlite3.Connection) -> None:
    """
    Reclassify content_type='text' Description chunks that are valid line-format → 'line'.
    This distinguishes parseable VRC line metadata from truly unrecognized text.
    """
    rows = conn.execute(
        "SELECT file_id, seq, text FROM itxt_chunks WHERE content_type = 'text' AND keyword = ?",
        (ITXT_KEY_DESCRIPTION,),
    ).fetchall()

    for r in rows:
        raw = sanitize_itxt_text(r["text"])
        try:
            parse_meta_line(raw)
            conn.execute(
                "UPDATE itxt_chunks SET content_type = 'line' WHERE file_id = ? AND seq = ?",
                (int(r["file_id"]), int(r["seq"])),
            )
        except Exception:
            pass  # Truly unparseable, leave as 'text'


def _005_drop_legacy_file_columns(conn: sqlite3.Connection) -> None:
    """
    Drop legacy columns from files table that are superseded by two-tier versioning:
    - processed: always 1, unused
    - parse_ok: never updated beyond initial 0
    - parser_version: superseded by file_parser_version/data_parser_version
    """
    cols = {row[1] for row in conn.execute("PRAGMA table_info(files)").fetchall()}
    for col in ("processed", "parse_ok", "parser_version"):
        if col in cols:
            conn.execute(f"ALTER TABLE files DROP COLUMN {col}")


# ─── Files table helpers (conflict-checked) ────────────────────────────────────
def _get_data_dir_path(conn: sqlite3.Connection, data_dir_id: int) -> Optional[Path]:
    row = conn.execute("SELECT path FROM data_dirs WHERE id = ?", (data_dir_id,)).fetchone()
    return Path(row["path"]) if row else None


def ensure_file_record(
    conn: sqlite3.Connection,
    file_id: int,
    h: str,
    file_ext: str,
    data_dir_id: int,
) -> None:
    """
    Ensure a consistent `files` row exists for (file_id, hash, ext, data_dir_id).
    - If row exists and (hash/ext) mismatch → hard exit.
    - If row exists and matches → update data_dir_id to provided value.
    - If absent → insert, computing size from disk (NOT NULL).
    """
    row = conn.execute(
        "SELECT file_id, hash, file_ext, data_dir_id FROM files WHERE file_id = ?",
        (file_id,),
    ).fetchone()
    if row:
        if row["hash"] != h or (row["file_ext"] or "").lower() != (file_ext or "").lower():
            sys.exit(
                f"File ID conflict: existing (file_id={row['file_id']}, hash={row['hash']}, ext={row['file_ext']}) "
                f"!= new (file_id={file_id}, hash={h}, ext={file_ext}). Exiting."
            )
        if row["data_dir_id"] != data_dir_id:
            with conn:
                conn.execute("UPDATE files SET data_dir_id = ? WHERE file_id = ?", (data_dir_id, file_id))
        return

    # Check if this hash already belongs to a different file_id.
    row_by_hash = conn.execute("SELECT file_id FROM files WHERE hash = ?", (h,)).fetchone()
    if row_by_hash and row_by_hash["file_id"] != file_id:
        sys.exit(
            f"Hash conflict: hash {h} already assigned to file_id "
            f"{row_by_hash['file_id']} (incoming file_id {file_id}). Exiting."
        )

    # Compute size from disk (must not be NULL)
    data_root = _get_data_dir_path(conn, data_dir_id)
    if not data_root:
        sys.exit(f"Unknown data_dir_id {data_dir_id} for file_id {file_id}")
    ext = (file_ext or "").lower()
    p = hydrus_path_for_hash(data_root, h, ext or "png")
    try:
        size_val = int(p.stat().st_size)
    except (FileNotFoundError, OSError) as e:
        logging.error(f"Cannot stat file for insert (file_id={file_id}, path={p}): {e}")
        # Do not insert a row without a verified size
        return

    with conn:
        conn.execute(
            """
            INSERT INTO files(file_id, hash, file_ext, data_dir_id, created_at, size)
            VALUES(?, ?, ?, ?, ?, ?)
            """,
            (file_id, h, ext or "png", data_dir_id, now_utc_iso(), size_val),
        )


def db_get_hashes_for_ids(conn: sqlite3.Connection, file_ids: List[int], chunk_size: int = 900) -> Dict[int, str]:
    """Return {file_id: hash} for a list of ids, chunked to avoid SQLite's 999-variable limit."""
    result: Dict[int, str] = {}
    if not file_ids:
        return result
    for chunk in chunked(file_ids, chunk_size):
        qmarks = ",".join(["?"] * len(chunk))
        rows = conn.execute(
            f"SELECT file_id, hash FROM files WHERE file_id IN ({qmarks})",
            chunk,
        ).fetchall()
        result.update({int(r["file_id"]): r["hash"] for r in rows})
    return result


def db_existing_processed_file_ids(conn: sqlite3.Connection) -> Set[int]:
    """File IDs where iTXt extraction is at current FILE_PARSER_VERSION.

    Files with iTXt chunks extracted at older versions are NOT considered processed
    for extraction, so they will be re-extracted when FILE_PARSER_VERSION is bumped.

    This does NOT check data_parser_version; that's handled separately.
    """
    return {
        int(row["file_id"])
        for row in conn.execute(
            "SELECT file_id FROM files WHERE file_parser_version >= ?",
            (FILE_PARSER_VERSION,),
        )
    }


def db_file_has_itxt_chunks(conn: sqlite3.Connection, file_id: int) -> bool:
    """Check if a file has any cached iTXt chunks in the database."""
    row = conn.execute(
        "SELECT 1 FROM itxt_chunks WHERE file_id = ? LIMIT 1",
        (file_id,),
    ).fetchone()
    return row is not None


def db_mark_processed_success(conn: sqlite3.Connection, file_id: int) -> None:
    """Mark a file as having iTXt chunks successfully extracted.

    Sets file_parser_version to current version; data_parser_version is set independently.
    """
    with conn:
        conn.execute(
            "UPDATE files SET file_parser_version = ?, parsed_at = ? WHERE file_id = ?",
            (FILE_PARSER_VERSION, now_utc_iso(), file_id),
        )


def db_mark_processed_failure(conn: sqlite3.Connection, file_id: int) -> None:
    """Mark a file as having failed iTXt extraction (e.g., malformed PNG).

    file_parser_version is NOT updated, so the file will be retried if FILE_PARSER_VERSION bumps.
    No data_parser_version is set since iTXt couldn't be extracted.
    """
    # Don't update file_parser_version on failure; let it remain 0 for retry
    with conn:
        conn.execute(
            "UPDATE files SET parsed_at = ? WHERE file_id = ?",
            (now_utc_iso(), file_id),
        )


def db_mark_data_parsed(conn: sqlite3.Connection, file_ids: List[int]) -> None:
    """Mark files as having been processed for metadata normalization.

    Called after data parsing phase (tag building) completes successfully.
    Updates data_parser_version to current version for all provided file_ids.
    """
    if not file_ids:
        return
    with conn:
        for chunk in chunked(file_ids, 900):
            qmarks = ",".join(["?"] * len(chunk))
            conn.execute(
                f"UPDATE files SET data_parser_version = ? WHERE file_id IN ({qmarks})",
                [DATA_PARSER_VERSION] + list(chunk),
            )


def db_get_or_create_data_dir_id(conn: sqlite3.Connection, data_dir: Path) -> int:
    row = conn.execute("SELECT id FROM data_dirs WHERE path = ?", (str(data_dir),)).fetchone()
    if row:
        return int(row["id"])
    with conn:
        cur = conn.execute("INSERT INTO data_dirs(path) VALUES(?)", (str(data_dir),))
        return int(cur.lastrowid)


def db_upsert_hydrus_meta(conn: sqlite3.Connection, file_id: int, meta: dict) -> None:
    """Store width/height/has_transparency/has_human_readable_embedded_metadata."""
    width = meta.get("width")
    height = meta.get("height")
    has_transparency = 1 if meta.get("has_transparency") else 0
    has_hrem = 1 if meta.get("has_human_readable_embedded_metadata") else 0
    with conn:
        conn.execute(
            """
            INSERT INTO hydrus_meta(
                file_id, width, height, has_transparency,
                has_human_readable_embedded_metadata, updated_at)
            VALUES(?, ?, ?, ?, ?, ?)
            ON CONFLICT(file_id) DO UPDATE SET
                width = excluded.width,
                height = excluded.height,
                has_transparency = excluded.has_transparency,
                has_human_readable_embedded_metadata = excluded.has_human_readable_embedded_metadata,
                updated_at = excluded.updated_at
            """,
            (file_id, width, height, has_transparency, has_hrem, now_utc_iso()),
        )


def db_get_cached_hydrus_meta(conn: sqlite3.Connection, file_ids: List[int], chunk_size: int = 900) -> Dict[int, dict]:
    """Return {file_id: minimal_meta_dict} for any file_ids already cached in hydrus_meta."""
    cached: Dict[int, dict] = {}
    if not file_ids:
        return cached
    for chunk in chunked(file_ids, chunk_size):
        qmarks = ",".join(["?"] * len(chunk))
        rows = conn.execute(
            f"""
            SELECT file_id, width, height, has_transparency, has_human_readable_embedded_metadata
            FROM hydrus_meta WHERE file_id IN ({qmarks})
            """,
            chunk,
        ).fetchall()
        for r in rows:
            cached[int(r["file_id"])] = {
                "file_id": int(r["file_id"]),
                "width": r["width"],
                "height": r["height"],
                "has_transparency": bool(r["has_transparency"]),
                "has_human_readable_embedded_metadata": bool(r["has_human_readable_embedded_metadata"]),
            }
    return cached


# ─── Meta parsing helpers ──────────────────────────────────────────────────────
def _normalize_meta(meta: Dict[str, Any], raw_text: str) -> Dict[str, Any]:
    """Normalize various meta shapes (legacy pipe, XMP, JSON) to a common schema."""
    norm: Dict[str, Any] = {
        'raw_text': raw_text,
        'type': meta.get('type'),
        'index': meta.get('index'),
        'creator_tool': meta.get('creator_tool'),
        'author': {'id': '', 'displayName': ''},
        'world': {'id': '', 'instanceId': '', 'name': ''},
        'position': {'x': 0.0, 'y': 0.0, 'z': 0.0},
        'rq': 0,
        'players': [],
        'created': meta.get('created'),
        'editor_software': meta.get('editor_software') or [],
    }

    a = meta.get('author') or {}
    norm['author']['id'] = a.get('id', '') or ''
    norm['author']['displayName'] = a.get('displayName') or a.get('name') or ''

    w = meta.get('world') or {}
    norm['world']['id'] = w.get('id', '') or ''
    norm['world']['instanceId'] = w.get('instanceId', '') or ''
    norm['world']['name'] = w.get('name', '') or ''

    pos = meta.get('position') or {}
    for k in ('x', 'y', 'z'):
        try:
            norm['position'][k] = float(pos.get(k, 0.0))
        except (TypeError, ValueError):
            pass

    try:
        norm['rq'] = int(meta.get('rq', 0))
    except (TypeError, ValueError):
        pass

    if isinstance(meta.get('players'), list):
        norm['players'] = meta['players']

    return norm


def db_replace_itxt_chunks(
    conn: sqlite3.Connection,
    file_id: int,
    descriptors: List[
        Tuple[int, Optional[str], Optional[int], Optional[int], Optional[str], Optional[str], Optional[str], str]
    ],
) -> None:
    """
    Each descriptor:
      (seq, keyword, comp_flag, comp_method, lang, trans, text, content_type)
    where content_type ∈ {'text','json','xml'} (future-safe for more).
    """
    with conn:
        conn.execute("DELETE FROM itxt_chunks WHERE file_id = ?", (file_id,))
        if descriptors:
            conn.executemany(
                """
                INSERT INTO itxt_chunks(
                    file_id, seq, keyword, compression_flag, compression_method,
                    language_tag, translated_keyword, text, content_type
                ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                [(file_id, *d) for d in descriptors],
            )


def db_load_all_parsed_meta(conn: sqlite3.Connection) -> Dict[int, dict]:
    """
    Build {file_id: parsed_meta_dict} by reading Description rows from itxt_chunks.
    Priority order per file_id:
        JSON > XML > Legacy line parser
    If content_type is empty/null, a lightweight XML heuristic is applied.
    All outputs are normalized to the common schema.
    """
    result: Dict[int, dict] = {}

    # Define priority mapping for quick comparison
    PRIORITY = {"json": 3, "xml": 2, "line": 1}

    rows = conn.execute(
        "SELECT file_id, text, content_type FROM itxt_chunks WHERE keyword IN (?, ?)",
        (ITXT_KEY_DESCRIPTION, ITXT_KEY_ADOBEXMPXML),
    ).fetchall()

    # Editor provenance is orthogonal to the metadata-priority contest: a file's
    # VRCX JSON may win priority while the editor software lives in a separate
    # XMP chunk. Collect it per-file independently, then merge in at the end.
    editor_by_fid: Dict[int, List[str]] = {}

    for r in rows:
        fid = int(r["file_id"])
        raw_text = sanitize_itxt_text(r["text"])
        ctype = (r["content_type"] or "").lower()

        if not ctype and _is_xmp_xml(raw_text):
            ctype = "xml"

        # Normalize ctype to one of the three categories
        if ctype not in ("json", "xml"):
            ctype = "line"

        try:
            if ctype == "json":
                meta = json.loads(raw_text)
            elif ctype == "xml":
                try:
                    meta = parse_xmp_meta(raw_text)
                except XMPParseError:
                    # Adobe-edited VRChat screenshots wrap the original VRCX JSON
                    # inside the XMP's dc:description; recover it if present.
                    embedded = extract_embedded_vrcx_json(raw_text)
                    if embedded is not None:
                        meta = embedded
                        ctype = "json"  # full VRCX payload → JSON priority
                    else:
                        meta = parse_meta_line(raw_text)
                        ctype = "line"  # fallback actually became 'line'
                # Record which app(s) created/edited the image (Adobe, GIMP, ...).
                # Tracked per-file so it survives even if this XMP chunk loses
                # the priority contest to a separate JSON chunk on the same file.
                for sw in extract_editor_software(raw_text):
                    bucket = editor_by_fid.setdefault(fid, [])
                    if sw not in bucket:
                        bucket.append(sw)
            else:
                meta = parse_meta_line(raw_text)

            # Check if we already have a record for this file_id
            if fid in result:
                existing_type = result[fid]["_parsed_type"]
                if PRIORITY[ctype] <= PRIORITY[existing_type]:
                    # Existing has higher or equal priority → skip
                    continue

            result[fid] = _normalize_meta(meta, raw_text)
            result[fid]["_parsed_type"] = ctype  # mark for internal priority tracking

        except (json.JSONDecodeError, ValueError, KeyError):
            # Skip rows that are irreparably broken (JSON parse errors, value errors, missing keys)
            continue

    # Merge editor provenance back in (independent of metadata priority)
    for fid, software in editor_by_fid.items():
        if fid in result:
            result[fid]["editor_software"] = software

    # Remove internal _parsed_type markers before returning
    for meta in result.values():
        meta.pop("_parsed_type", None)

    return result

# ─── Push info helpers ────────────────────────────────────────────────────────


def db_get_push_info(conn: sqlite3.Connection, file_id: int) -> Optional[sqlite3.Row]:
    return conn.execute(
        "SELECT file_id, tag_hash, first_pushed, last_pushed FROM pushes WHERE file_id = ?",
        (file_id,),
    ).fetchone()


def db_upsert_push_info(conn: sqlite3.Connection, file_id: int, tag_hash: str) -> None:
    ts = now_utc_iso()
    with conn:
        conn.execute(
            """
            INSERT INTO pushes(file_id, tag_hash, first_pushed, last_pushed)
            VALUES(?, ?, ?, ?)
            ON CONFLICT(file_id) DO UPDATE SET
                tag_hash = excluded.tag_hash,
                last_pushed = excluded.last_pushed
            """,
            (file_id, tag_hash, ts, ts),
        )

# ─── Tag caching helper ────────────────────────────────────────────────────────


def db_bulk_replace_tag_mappings(conn: sqlite3.Connection, mappings: Iterable[Tuple[str, str]]) -> None:
    with conn:
        conn.execute("DELETE FROM tag_mappings")
        if mappings:
            conn.executemany("INSERT OR IGNORE INTO tag_mappings(parent, child) VALUES(?, ?)", mappings)


# ─── Analysis and Diagnostics ─────────────────────────────────────────────────────
def db_recover_broken_metadata(conn: sqlite3.Connection, broken_dir: Path = BROKEN_DIR) -> int:
    """
    Attempt to recover broken metadata files from broken_metadata/ folder.
    Returns number of successfully recovered files.

    This reads files that were previously unparseable and attempts to extract
    metadata fields that can be salvaged (author, world, etc.).
    """
    if not broken_dir.exists():
        return 0

    recovered = 0
    for txt_file in broken_dir.glob("*.txt"):
        try:
            raw_text = txt_file.read_text(encoding="utf-8", errors="replace").strip()
            if not raw_text:
                continue

            # Try to extract file hash from filename
            hash_guess = txt_file.stem  # removes extension
            if len(hash_guess) < 8:  # Not a valid hash
                continue

            # Try with lenient parsing: this will skip malformed fields instead of failing
            try:
                from core.meta_line_parser import parse_meta_line
                parse_meta_line(raw_text)
                # Successfully recovered at least partial metadata
                logging.info(f"Recovered partial metadata from {txt_file.name}")
                recovered += 1
            except (ValueError, KeyError):
                # Still can't parse, leave in broken_metadata
                pass
        except Exception as e:
            logging.warning(f"Error reading {txt_file}: {e}")

    return recovered


def db_get_state_summary(conn: sqlite3.Connection) -> Dict[str, Any]:
    """Get comprehensive database state summary for diagnostics."""
    summary: Dict[str, Any] = {}

    try:
        # File counts by version
        cursor = conn.execute("""
            SELECT file_parser_version, data_parser_version, COUNT(*) as cnt
            FROM files
            GROUP BY file_parser_version, data_parser_version
        """)
        summary["files_by_version"] = {
            f"file_v{r['file_parser_version']}_data_v{r['data_parser_version']}": r['cnt']
            for r in cursor.fetchall()
        }

        # Total file count
        total_files = conn.execute("SELECT COUNT(*) FROM files").fetchone()[0]
        summary["total_files"] = total_files

        # iTXt chunk stats
        summary["total_itxt_chunks"] = conn.execute("SELECT COUNT(*) FROM itxt_chunks").fetchone()[0]

        cursor = conn.execute("""
            SELECT content_type, COUNT(*) as cnt
            FROM itxt_chunks
            GROUP BY content_type
        """)
        summary["itxt_by_type"] = {r['content_type'] or 'NULL': r['cnt'] for r in cursor.fetchall()}

        # Files with parseable metadata
        summary["files_with_metadata"] = conn.execute("""
            SELECT COUNT(DISTINCT file_id) FROM itxt_chunks
            WHERE keyword IN (?, ?)
        """, (ITXT_KEY_DESCRIPTION, ITXT_KEY_ADOBEXMPXML)).fetchone()[0]

        # Tag stats
        summary["tag_mappings"] = conn.execute("SELECT COUNT(*) FROM tag_mappings").fetchone()[0]
        summary["hash_tags"] = conn.execute("SELECT COUNT(*) FROM hash_tags").fetchone()[0]
        summary["pushes_tracked"] = conn.execute("SELECT COUNT(*) FROM pushes").fetchone()[0]

    except sqlite3.OperationalError as e:
        summary["error"] = f"Database error: {str(e)}"
    except sqlite3.DatabaseError as e:
        summary["error"] = f"Database corruption error: {str(e)}"

    return summary


def db_find_inconsistent_versions(conn: sqlite3.Connection) -> List[Dict[str, Any]]:
    """Find files with inconsistent version states: file_parser_version=0 but data_parser_version>0.

    These files should have been extracted (file_parser_version>0) but weren't.
    They need re-parsing to fix the inconsistency.
    """
    rows = conn.execute("""
        SELECT file_id, hash, file_parser_version, data_parser_version
        FROM files
        WHERE file_parser_version = 0 AND data_parser_version > 0
    """).fetchall()

    return [dict(r) for r in rows]


def db_reset_inconsistent_versions(conn: sqlite3.Connection, file_ids: List[int]) -> int:
    """Reset inconsistent files by cleaning up orphaned data and re-queuing for extraction.

    For files with file_parser_version=0 but data_parser_version>0:
    1. Delete iTXt chunks (they're orphaned since extraction failed)
    2. Delete hash_tags for these files (they're derived from invalid chunks)
    3. Reset both versions to 0 for re-extraction

    Returns: number of files reset.
    """
    if not file_ids:
        return 0

    count = 0
    with conn:
        for chunk in chunked(file_ids, 900):
            qmarks = ",".join(["?"] * len(chunk))

            # Delete iTXt chunks (they shouldn't exist if file_parser_version=0)
            conn.execute(f"DELETE FROM itxt_chunks WHERE file_id IN ({qmarks})", chunk)

            # Delete hash_tags for these files (derived from invalid chunks)
            conn.execute(f"DELETE FROM hash_tags WHERE file_id IN ({qmarks})", chunk)

            # Reset both versions to 0 for fresh extraction
            cursor = conn.execute(
                f"UPDATE files SET file_parser_version = 0, data_parser_version = 0 WHERE file_id IN ({qmarks})",
                chunk,
            )
            count += cursor.rowcount

    return count


def db_find_files_without_metadata(conn: sqlite3.Connection) -> List[Dict[str, Any]]:
    """Find processed files (file_parser_version > 0) that have no iTXt chunks.

    These are files successfully scanned for iTXt but containing no VRC metadata
    (e.g. plain PNGs, screenshots without embedded data).
    """
    rows = conn.execute("""
        SELECT f.file_id, f.hash, f.file_parser_version, f.data_parser_version, COUNT(ic.file_id) as chunk_count
        FROM files f
        LEFT JOIN itxt_chunks ic ON f.file_id = ic.file_id
        WHERE f.file_parser_version > 0
        GROUP BY f.file_id
        HAVING chunk_count = 0
    """).fetchall()

    return [dict(r) for r in rows]


def db_find_unparseable_chunks(conn: sqlite3.Connection) -> List[Dict[str, Any]]:
    """Find iTXt chunks that couldn't be parsed as VRC metadata.

    Excludes:
    - content_type='line' (valid VRC line-format metadata)
    - Non-VRC keywords: Comment, Microsoft.GameDVR.*, parameters, NULL
    """
    rows = conn.execute("""
        SELECT keyword, content_type, COUNT(*) as cnt
        FROM itxt_chunks
        WHERE (content_type = 'text' OR content_type IS NULL)
          AND keyword IS NOT NULL
          AND keyword NOT IN ('Comment', 'parameters')
          AND keyword NOT LIKE 'Microsoft.GameDVR.%'
        GROUP BY keyword, content_type
    """).fetchall()

    return [dict(r) for r in rows]


def db_get_migration_status(conn: sqlite3.Connection) -> Dict[str, Any]:
    """Get status of all applied migrations."""
    rows = conn.execute("""
        SELECT id, name, applied_at FROM schema_migrations ORDER BY id
    """).fetchall()

    return {r['name']: r['applied_at'] for r in rows}


def db_print_diagnostic_report(conn: sqlite3.Connection) -> None:
    """Print a comprehensive diagnostic report to stdout."""
    print("\n" + "="*70)
    print("DATABASE DIAGNOSTIC REPORT")
    print("="*70)

    # State summary
    summary = db_get_state_summary(conn)
    if "error" in summary:
        print(f"\n[ERROR] {summary['error']}")
        return

    print(f"\n[FILES] {summary.get('total_files', 0)}")
    versions = summary.get("files_by_version", {})
    if versions:
        for k, v in versions.items():
            print(f"   {k}: {v}")

    print(f"\n[ITXT CHUNKS] {summary.get('total_itxt_chunks', 0)}")
    types = summary.get("itxt_by_type", {})
    if types:
        for t, c in types.items():
            print(f"   {t}: {c}")

    print(f"\n[METADATA] {summary.get('files_with_metadata', 0)} files")
    print(f"[TAG_MAPPINGS] {summary.get('tag_mappings', 0)}")
    print(f"[HASH_TAGS] {summary.get('hash_tags', 0)}")
    print(f"[PUSHES] {summary.get('pushes_tracked', 0)}")

    # Inconsistent versions
    inconsistent = db_find_inconsistent_versions(conn)
    if inconsistent:
        print(f"\n[WARN] INCONSISTENT VERSIONS ({len(inconsistent)}):")
        print("   Files with file_v=0 but data_v>0 (should be re-parsed):")
        for row in inconsistent[:5]:
            print(
                f"   file_id={row['file_id']}, hash={row['hash'][:8]}..., "
                f"file_v={row['file_parser_version']}, data_v={row['data_parser_version']}"
            )
        if len(inconsistent) > 5:
            print(f"   ... and {len(inconsistent) - 5} more")

    # Files processed but containing no VRC metadata
    no_metadata = db_find_files_without_metadata(conn)
    if no_metadata:
        print(f"\n[INFO] PROCESSED, NO VRC METADATA ({len(no_metadata)}):")
        print(f"   {len(no_metadata)} files scanned but contained no iTXt chunks")

    # Unparseable chunks
    unparseable = db_find_unparseable_chunks(conn)
    if unparseable:
        print(f"\n[WARN] UNPARSEABLE CHUNKS ({len(unparseable)} types):")
        for row in unparseable:
            print(f"   {row['keyword']}/{row['content_type']}: {row['cnt']}")

    # Migrations
    migs = db_get_migration_status(conn)
    if migs:
        print(f"\n[OK] MIGRATIONS ({len(migs)}):")
        for name, timestamp in migs.items():
            print(f"   {name}")

    print("\n" + "="*70 + "\n")
