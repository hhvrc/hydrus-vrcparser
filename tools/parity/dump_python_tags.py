"""Dump per-file tags using the LEGACY Python pipeline.

Runs the production path -- db_load_all_parsed_meta then
build_file_id_to_tags -- so the C# port can be diffed against the output that
actually reaches Hydrus, not just against intermediate parse results.
"""
import hashlib
import json
import sqlite3
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "legacy-python"))

from db_logic import db_load_all_parsed_meta          # noqa: E402
from core.tag_builders import build_file_id_to_tags   # noqa: E402

DB = sys.argv[1]
OUT = sys.argv[2]


def tags_hash(tags):
    """Same function as hydrus_io.tags_hash, inlined to avoid importing hydrus_api."""
    return hashlib.sha256("\n".join(sorted(tags)).encode("utf-8")).hexdigest()


def main():
    conn = sqlite3.connect(f"file:{DB}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row

    all_meta = db_load_all_parsed_meta(conn)
    file_id_to_tags = build_file_id_to_tags(all_meta)

    # Every file that has chunks at all, so the two dumps agree on keys even
    # where no metadata could be recovered.
    file_ids = [r[0] for r in conn.execute(
        "SELECT DISTINCT file_id FROM itxt_chunks ORDER BY file_id")]

    tagged = 0
    with open(OUT, "w", encoding="utf-8") as f:
        for fid in file_ids:
            tags = file_id_to_tags.get(fid)
            if tags is None:
                record = {"file_id": fid, "tags": None, "tag_hash": None}
            else:
                tagged += 1
                record = {
                    "file_id": fid,
                    "tags": sorted(tags),
                    "tag_hash": tags_hash(tags),
                }
            f.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + "\n")

    print(f"wrote {len(file_ids)} file records ({tagged} with tags) to {OUT}")


if __name__ == "__main__":
    main()
