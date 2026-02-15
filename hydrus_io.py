import hashlib
import logging
from typing import Dict, List, Tuple

import hydrus_api as hydrus

from db_logic import (
    ensure_file_record,
    db_get_cached_hydrus_meta,
    db_upsert_hydrus_meta,
    db_get_push_info,
    db_get_hashes_for_ids,
    db_upsert_push_info,
)
from core.constants import BATCH_SIZE

def get_service_key_by_name(client: hydrus.Client, desired_name: str | None = None) -> str:
    services = client.get_services().get("local_tags")

    if not isinstance(services, list) or len(services) == 0:
        raise SystemExit("ERROR: get_services() returned no usable local tag services")

    if desired_name:
        for svc in services:
            if svc.get("name") == desired_name:
                key = svc.get("service_key")
                if not key:
                    raise SystemExit(f"ERROR: service '{desired_name}' has no service_key")
                logging.info(f"Using local tag service by name: {desired_name} ({key})")
                return key
        available = ", ".join(s.get("name") or "?" for s in services)
        raise SystemExit(f"ERROR: service not found by name: {desired_name}\nAvailable: {available}")

    if len(services) > 1:
        names = [f"{s.get('name')} (key={s.get('service_key')})" for s in services]
        raise SystemExit("ERROR: multiple local tag services found:\n  - " + "\n  - ".join(names))

    svc = services[0]
    key = svc.get("service_key")
    name = svc.get("name")
    if not key:
        raise SystemExit("ERROR: local tag service has no service_key")
    logging.info(f"Using local tag service: {name} ({key})")
    return key

def _fetch_and_store_metadata(client: hydrus.Client, conn, file_ids: List[int], data_dir_id: int) -> List[dict]:
    """Fetch Hydrus metadata for given file IDs, ensure file records and cache metadata."""
    rows = client.get_file_metadata(file_ids=file_ids).get("metadata", [])
    for row in rows:
        fid = row.get("file_id")
        h = row.get("hash")
        if fid is None or not h:
            continue
        ext = (row.get("ext") or "png").lstrip(".").lower()
        ensure_file_record(conn, int(fid), h, ext, data_dir_id)
        db_upsert_hydrus_meta(conn, int(fid), row)
    return rows


def get_exif_vrchat_file_rows(client: hydrus.Client, conn, data_dir_id: int) -> List[dict]:
    """Search Hydrus for candidate PNGs with embedded metadata.

    Ensures `files` rows and caches Hydrus metadata.
    """
    query = ["system:filetype is png", "system:has embedded metadata"]
    file_ids = client.search_files(query).get("file_ids", [])
    if not file_ids:
        logging.warning("Didn't find any PNGs with embedded metadata.")
        return []

    logging.info(f"Found {len(file_ids)} candidate files")

    cached_map = db_get_cached_hydrus_meta(conn, file_ids)
    cached_ids = set(cached_map.keys())

    to_fetch = [fid for fid in file_ids if fid not in cached_ids]
    fetched: List[dict] = []
    if to_fetch:
        logging.info(f"Fetching metadata for {len(to_fetch)} uncached files")
        fetched = _fetch_and_store_metadata(client, conn, to_fetch, data_dir_id)

    # Combine cached + fetched
    fetched_ids = {r.get("file_id") for r in fetched if r.get("file_id") is not None}
    combined = list(fetched)
    for fid, meta in cached_map.items():
        if fid not in fetched_ids:
            combined.append(meta)

    # Ensure files rows for cached-only entries missing from files table
    missing_files = [
        int(r["file_id"]) for r in combined
        if r.get("file_id") is not None
        and not conn.execute("SELECT 1 FROM files WHERE file_id=?", (r["file_id"],)).fetchone()
    ]
    if missing_files:
        _fetch_and_store_metadata(client, conn, missing_files, data_dir_id)

    # Filter to human-readable embedded metadata
    filtered = [f for f in combined if f.get("has_human_readable_embedded_metadata") in (True, 1)]
    logging.info(f"{len(filtered)} files with human-readable embedded metadata ({len(cached_ids)} cached, {len(to_fetch)} fetched)")
    return filtered

def tags_hash(tags: List[str]) -> str:
    joined = "\n".join(sorted(tags))
    return hashlib.sha256(joined.encode("utf-8")).hexdigest()

def push_tags_batched_if_changed(conn, client: hydrus.Client, service_name: str, file_id_to_tags: Dict[int, List[str]], batch_size: int = BATCH_SIZE):
    try:
        service_key = get_service_key_by_name(client, service_name)
    except SystemExit as e:
        logging.error(f"Cannot push tags: {e}")
        return
    except (hydrus.HydrusAPIException, AttributeError) as e:
        logging.error(f"Unexpected error getting service key: {e}")
        return

    to_push: List[Tuple[int, List[str], str]] = []
    for file_id, tags in file_id_to_tags.items():
        th = tags_hash(tags)
        row = db_get_push_info(conn, file_id)
        if row and row["tag_hash"] == th:
            continue
        to_push.append((file_id, tags, th))

    logging.info(f"{len(to_push)} files need tag updates (out of {len(file_id_to_tags)})")

    # chunk & push
    for i in range(0, len(to_push), batch_size):
        batch = to_push[i : i + batch_size]
        batch_ids = [fid for (fid, _, _) in batch]
        hashes_map = db_get_hashes_for_ids(conn, batch_ids)

        for file_id, tags, th in batch:
            if not tags:
                continue
            h = hashes_map.get(file_id)
            if not h:
                logging.error(f"Missing hash for file_id {file_id}; skipping tag push.")
                continue
            try:
                client.add_tags(hashes=[h], service_keys_to_tags={service_key: tags})
                db_upsert_push_info(conn, file_id, th)
            except (hydrus.HydrusAPIException, TypeError) as e:
                logging.error(f"Failed to push tags for file_id {file_id}: {e}")
                continue
        logging.info(f"Pushed tags for {len(batch)} files")
