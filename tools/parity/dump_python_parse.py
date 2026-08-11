"""Dump per-chunk normalized metadata using the LEGACY Python parsers.

One JSON object per itxt_chunk row, so the C# port can be diffed against it
chunk-by-chunk, isolating parser behaviour from the file-level priority contest.
"""
import json
import sqlite3
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "legacy-python"))

from core.utils import sanitize_itxt_text                      # noqa: E402
from core.meta_line_parser import parse_meta_line              # noqa: E402
from core.meta_xmp_parser import (                             # noqa: E402
    parse_xmp_meta, XMPParseError, extract_embedded_vrcx_json, extract_editor_software,
)
from db_logic import _normalize_meta                           # noqa: E402

DB = sys.argv[1]
OUT = sys.argv[2]


def parse_chunk(raw_text, ctype):
    """Mirror the per-chunk dispatch inside db_load_all_parsed_meta."""
    editor = []
    effective = ctype

    if ctype == "json":
        meta = json.loads(raw_text)
    elif ctype == "xml":
        try:
            meta = parse_xmp_meta(raw_text)
        except XMPParseError:
            embedded = extract_embedded_vrcx_json(raw_text)
            if embedded is not None:
                meta = embedded
                effective = "json"
            else:
                meta = parse_meta_line(raw_text)
                effective = "line"
        editor = extract_editor_software(raw_text)
    else:
        meta = parse_meta_line(raw_text)
        effective = "line"

    norm = _normalize_meta(meta, raw_text)
    return norm, editor, effective


def main():
    conn = sqlite3.connect(f"file:{DB}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row

    rows = conn.execute(
        "SELECT file_id, seq, keyword, text, content_type FROM itxt_chunks ORDER BY file_id, seq"
    ).fetchall()

    written = 0
    with open(OUT, "w", encoding="utf-8") as f:
        for r in rows:
            raw_text = sanitize_itxt_text(r["text"])
            ctype = (r["content_type"] or "").lower()
            # Match db_load_all_parsed_meta: anything not json/xml is tried as line.
            if ctype not in ("json", "xml"):
                ctype = "line"

            record = {
                "file_id": r["file_id"],
                "seq": r["seq"],
                "keyword": r["keyword"],
                "stored_type": r["content_type"],
            }

            try:
                norm, editor, effective = parse_chunk(raw_text, ctype)
            except Exception as e:
                record["error"] = type(e).__name__
                f.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + "\n")
                written += 1
                continue

            created = norm.get("created")
            record.update({
                "effective_type": effective,
                "author_id": norm["author"]["id"],
                "author_name": norm["author"]["displayName"],
                "world_id": norm["world"]["id"],
                "world_name": norm["world"]["name"],
                "instance_id": norm["world"]["instanceId"],
                "creator_tool": norm.get("creator_tool"),
                "editor_software": editor,
                "created": created.isoformat() if hasattr(created, "isoformat") else None,
                "players": [
                    [p.get("id") if isinstance(p, dict) else None,
                     p.get("displayName") if isinstance(p, dict) else None]
                    for p in (norm.get("players") or [])
                ],
            })
            f.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + "\n")
            written += 1

    print(f"wrote {written} chunk records to {OUT}")


if __name__ == "__main__":
    main()
