#!/usr/bin/env python3
import logging
from pathlib import Path
from typing import Dict

from hydrus_api import (
    Client as HydrusClient,
    ConnectionError as HydrusConnectionError,
)

from cli import parse_args, validate_args
from config_io import merge_args_with_config
from db_logic import (
    init_db,
    db_get_or_create_data_dir_id,
    db_get_hashes_for_ids,
    db_existing_processed_file_ids,
    db_file_has_itxt_chunks,
    db_mark_processed_failure,
    db_mark_processed_success,
    db_mark_data_parsed,
    db_replace_itxt_chunks,
    db_load_all_parsed_meta,
    db_bulk_replace_tag_mappings,
    db_recover_broken_metadata,
    db_print_diagnostic_report,
    db_find_inconsistent_versions,
    db_reset_inconsistent_versions,
    hydrus_path_for_hash,
)
from hydrus_io import get_exif_vrchat_file_rows, push_tags_batched_if_changed
from core.constants import BROKEN_DIR, BATCH_SIZE
from core.utils import chunked
from core.png_itxt import extract_itxt_records_and_description
from core.tag_builders import build_tag_mappings, build_file_id_to_tags

def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )

    args = merge_args_with_config(parse_args())
    validate_args(args)

    conn = init_db(args.db)
    data_dir_id = db_get_or_create_data_dir_id(conn, args.data_dir)

    client = HydrusClient(args.api_key, args.hydrus_addr)

    try:
        # 1) Discover candidates & cache Hydrus metadata (and ensure files rows)
        meta_rows = get_exif_vrchat_file_rows(client, conn, data_dir_id)
    except HydrusConnectionError:
        logging.error("Could not connect to Hydrus at %s", args.hydrus_addr)
        conn.close()
        return

    if not meta_rows:
        logging.info("No candidates found; nothing to do.")
        conn.close()
        return

    all_file_ids = [int(row["file_id"]) for row in meta_rows if row.get("file_id") is not None]
    
    # Fix inconsistent versions before proceeding
    inconsistent = db_find_inconsistent_versions(conn)
    if inconsistent:
        inconsistent_ids = [row["file_id"] for row in inconsistent]
        logging.warning(f"Found {len(inconsistent_ids)} files with inconsistent versions (file_v=0, data_v>0)")
        for row in inconsistent[:5]:
            logging.debug(f"  file_id={row['file_id']}, hash={row['hash'][:8]}..., file_v={row['file_parser_version']}, data_v={row['data_parser_version']}")
        reset_count = db_reset_inconsistent_versions(conn, inconsistent_ids)
        logging.info(f"Reset {reset_count} inconsistent files for re-parsing")
        all_file_ids.extend(inconsistent_ids)
        all_file_ids = list(set(all_file_ids))
    
    already_extracted = db_existing_processed_file_ids(conn)
    to_extract_ids = [fid for fid in all_file_ids if fid not in already_extracted]
    logging.info(f"{len(all_file_ids)} total; {len(already_extracted)} already extracted; {len(to_extract_ids)} to extract")

    # cache id->ext where available, fallback to 'png'
    id_to_ext: Dict[int, str] = {int(r["file_id"]): r.get("ext", "png").lstrip(".").lower() for r in meta_rows if r.get("file_id") is not None and r.get("ext")}

    # 2) Parse iTXt in batches, skipping files that already have cached chunks
    total_ok = 0
    total_fail = 0
    total_skipped = 0
    total_io_err = 0
    total_cached = 0
    batch_num = 0
    num_batches = (len(to_extract_ids) + BATCH_SIZE - 1) // BATCH_SIZE

    for batch_ids in chunked(to_extract_ids, BATCH_SIZE):
        batch_num += 1
        batch_ok = 0
        batch_fail = 0
        batch_skipped = 0
        batch_io_err = 0
        batch_cached = 0

        id_to_hash = db_get_hashes_for_ids(conn, batch_ids)
        for file_id in batch_ids:
            h = id_to_hash.get(file_id)
            if not h:
                logging.error(f"No hash for file_id {file_id}; skipping extract")
                continue

            # Check if this file already has iTXt chunks cached
            if db_file_has_itxt_chunks(conn, file_id):
                db_mark_processed_success(conn, file_id)
                batch_cached += 1
                continue

            ext = id_to_ext.get(file_id, "png")
            row = conn.execute(
                "SELECT d.path AS data_dir FROM files f JOIN data_dirs d ON d.id=f.data_dir_id WHERE f.file_id=?",
                (file_id,),
            ).fetchone()
            data_dir = Path(row["data_dir"]) if row else args.data_dir
            path = hydrus_path_for_hash(data_dir, h, ext)

            descriptors, parsed_ok_flag, io_error = extract_itxt_records_and_description(path, h, BROKEN_DIR)
            db_replace_itxt_chunks(conn, file_id, descriptors)

            if io_error:
                batch_io_err += 1
                continue
            if parsed_ok_flag:
                db_mark_processed_success(conn, file_id)
                batch_ok += 1
            elif not descriptors:
                # No iTXt chunks — not a failure, just nothing to extract
                db_mark_processed_success(conn, file_id)
                batch_skipped += 1
            else:
                db_mark_processed_failure(conn, file_id)
                batch_fail += 1

        total_ok += batch_ok
        total_fail += batch_fail
        total_skipped += batch_skipped
        total_io_err += batch_io_err
        total_cached += batch_cached
        logging.info(
            f"Batch {batch_num}/{num_batches}: ok={batch_ok} skipped={batch_skipped} "
            f"cached={batch_cached} failed={batch_fail} io_err={batch_io_err}"
        )

    logging.info(
        f"Extract complete: {total_ok} ok, {total_skipped} skipped (no iTXt), {total_cached} cached, "
        f"{total_fail} failed, {total_io_err} I/O errors"
    )

    # 2.5) Attempt recovery of previously broken metadata with lenient parsing
    logging.info("Attempting recovery of broken metadata files...")
    recovered = db_recover_broken_metadata(conn, BROKEN_DIR)
    if recovered > 0:
        logging.info(f"Successfully recovered {recovered} broken metadata files")

    # 3) Build tags from ALL successfully parsed metadata present in DB
    all_meta = db_load_all_parsed_meta(conn)
    if not all_meta:
        logging.info("No parsed metadata in DB; nothing to build/push.")
        conn.close()
        return

    tag_mappings = build_tag_mappings(all_meta)
    file_id_to_tags = build_file_id_to_tags(all_meta)

    db_bulk_replace_tag_mappings(conn, tag_mappings)

    # Mark all files with parsed metadata as having completed data parsing
    db_mark_data_parsed(conn, list(file_id_to_tags.keys()))

    # Cache flattened tags
    flat_pairs = [(fid, t) for fid, tags in file_id_to_tags.items() for t in tags]
    with conn:
        conn.execute("DELETE FROM hash_tags")
        if flat_pairs:
            conn.executemany("INSERT OR IGNORE INTO hash_tags(file_id, tag) VALUES(?, ?)", flat_pairs)

    logging.info(f"Cached {len(tag_mappings)} tag mappings and {len(flat_pairs)} file_id→tag pairs")

    # 4) Push tags only when changed
    logging.info(f"Evaluating tag changes for {len(file_id_to_tags)} files")
    push_tags_batched_if_changed(conn, client, args.service_name, file_id_to_tags, batch_size=BATCH_SIZE)
    
    # 5) Final diagnostic report
    logging.info("Final database state:")
    db_print_diagnostic_report(conn)
    
    logging.info("Done.")
    conn.close()



if __name__ == "__main__":
    main()
