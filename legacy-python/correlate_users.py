#!/usr/bin/env python3
"""Correlate vrchat-user-id tags with vrchat-user-name tags from Hydrus.

Reads every file carrying a ``vrchat-user-id:*`` or ``vrchat-user-name:*`` tag
straight from the live Hydrus client API, excludes id/name pairs that are
already trivially linked (identical file sets), and infers which of the
remaining, unpaired ids and names belong to the same user based on how their
file sets overlap. Read-only: it never writes tags back to Hydrus.

Output is a ranked table on the console plus a CSV for review.

Usage:
    python correlate_users.py
    python correlate_users.py --api-key <KEY> --hydrus-addr http://localhost:45869 \
        --service-name "my tags" --out user_correlations.csv
"""
import argparse
import csv
import logging
import sys
from collections import defaultdict
from pathlib import Path
from typing import Dict, List, Set, Tuple
from urllib.parse import urlparse

import hydrus_api as hydrus

from config_io import load_config
from hydrus_io import get_service_key_by_name
from core.constants import BATCH_SIZE
from core.utils import chunked
from core.user_correlation import correlate, ID_PREFIX, NAME_PREFIX, Suggestion


def parse_args():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--api-key", help="Hydrus API key (defaults to config.json)")
    p.add_argument("--hydrus-addr", help="Hydrus client address, e.g. http://localhost:45869")
    p.add_argument("--service-name", help="Hydrus tag service to read from (defaults to config.json)")
    p.add_argument("--config", default="config.json", help="Path to config file (default: config.json)")
    p.add_argument("--out", default="user_correlations.csv", help="CSV output path (default: user_correlations.csv)")
    p.add_argument("--min-overlap", type=int, default=2,
                   help="Min files an id and name must share to be suggested (default: 2)")
    p.add_argument("--min-jaccard", type=float, default=0.5,
                   help="Min file-set Jaccard similarity to suggest a pair (default: 0.5)")
    p.add_argument("--max-runner-up-ratio", type=float, default=0.6,
                   help="Reject if the 2nd-best name scores above this fraction of the winner (default: 0.6)")
    p.add_argument("--show", type=int, default=50, help="Max suggestion rows to print (default: 50)")
    return p.parse_args()


def resolve_settings(args) -> Tuple[str, str, str]:
    """Merge CLI args over config.json (CLI wins). Does not persist."""
    cfg = load_config(Path(args.config))
    api_key = args.api_key or cfg.get("api_key")
    hydrus_addr = args.hydrus_addr or cfg.get("hydrus_addr")
    service_name = args.service_name or cfg.get("service_name")

    if not api_key or not str(api_key).strip():
        sys.exit("Error: missing api_key. Provide --api-key or set it in config.json.")
    parsed = urlparse(hydrus_addr or "")
    if parsed.scheme not in ("http", "https") or not parsed.netloc:
        sys.exit(f"Error: invalid hydrus address (CLI or config): {hydrus_addr}")
    return api_key, hydrus_addr, service_name


def search_tagged_file_ids(client: hydrus.Client, tag_service_key: str) -> List[int]:
    """Return file ids carrying any vrchat-user-id or vrchat-user-name tag."""
    ids: Set[int] = set()
    for wildcard in (f"{ID_PREFIX}*", f"{NAME_PREFIX}*"):
        found = client.search_files(
            [wildcard], tag_service_key=tag_service_key, return_file_ids=True
        ).get("file_ids", [])
        ids.update(found)
        logging.info(f"  {wildcard}: {len(found)} files")
    return sorted(ids)


def extract_id_name(meta_row: dict) -> Tuple[Set[str], Set[str]]:
    """Pull the set of current user-id and user-name values from one metadata row.

    Aggregates current tags across all tag services so manually-added tags count
    too. Supports both the modern ``tags`` object and the legacy
    ``service_keys_to_statuses_to_tags`` shape.
    """
    ids: Set[str] = set()
    names: Set[str] = set()

    def consume(current_tags):
        for t in current_tags or []:
            if t.startswith(ID_PREFIX):
                ids.add(t[len(ID_PREFIX):])
            elif t.startswith(NAME_PREFIX):
                names.add(t[len(NAME_PREFIX):])

    tags = meta_row.get("tags")
    if isinstance(tags, dict):
        for svc in tags.values():
            consume((svc.get("storage_tags") or {}).get("0"))
    legacy = meta_row.get("service_keys_to_statuses_to_tags")
    if isinstance(legacy, dict):
        for svc in legacy.values():
            consume((svc or {}).get("0"))

    return ids, names


def fetch_files(
    client: hydrus.Client, file_ids: List[int]
) -> Tuple[List[Tuple[Set[str], Set[str]]], Dict[str, Set[str]], Dict[str, Set[str]]]:
    """Fetch metadata and build per-file (ids, names) plus id/name -> file-hash maps.

    The hash maps let us cite example screenshots for each suggested pair.
    """
    files: List[Tuple[Set[str], Set[str]]] = []
    id_hashes: Dict[str, Set[str]] = defaultdict(set)
    name_hashes: Dict[str, Set[str]] = defaultdict(set)

    num_batches = (len(file_ids) + BATCH_SIZE - 1) // BATCH_SIZE
    for batch_num, batch in enumerate(chunked(file_ids, BATCH_SIZE), start=1):
        rows = client.get_file_metadata(file_ids=batch).get("metadata", [])
        for row in rows:
            ids, names = extract_id_name(row)
            if not ids and not names:
                continue
            files.append((ids, names))
            h = row.get("hash") or ""
            for i in ids:
                id_hashes[i].add(h)
            for n in names:
                name_hashes[n].add(h)
        logging.info(f"  fetched metadata batch {batch_num}/{num_batches}")

    return files, id_hashes, name_hashes


def example_hashes(
    sugg: Suggestion, id_hashes: Dict[str, Set[str]], name_hashes: Dict[str, Set[str]], limit: int = 3
) -> List[str]:
    shared = sorted(id_hashes.get(sugg.user_id, set()) & name_hashes.get(sugg.name, set()))
    return shared[:limit]


def write_csv(path: Path, suggestions: List[Suggestion],
              id_hashes: Dict[str, Set[str]], name_hashes: Dict[str, Set[str]]) -> None:
    with path.open("w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow([
            "user_id", "name", "overlap", "id_files", "name_files",
            "jaccard", "runner_up_jaccard", "example_hashes",
        ])
        for s in suggestions:
            w.writerow([
                s.user_id, s.name, s.overlap, s.id_files, s.name_files,
                s.jaccard, s.runner_up_jaccard,
                " ".join(example_hashes(s, id_hashes, name_hashes)),
            ])


def print_report(result, args, csv_path: Path) -> None:
    print("\n" + "=" * 78)
    print("VRCHAT USER ID <-> NAME CORRELATION")
    print("=" * 78)
    print(f"Files scanned ................ {result.n_files}")
    print(f"Distinct user-ids ............ {result.n_ids}")
    print(f"Distinct user-names .......... {result.n_names}")
    print(f"Already paired (excluded) .... {len(result.paired_pairs)}")
    print(f"Ambiguous already-paired ..... {len(result.ambiguous_paired_ids)}")
    print(f"Orphan ids (no name seen) .... {len(result.orphan_ids)}")
    print(f"Orphan names (no id seen) .... {len(result.orphan_names)}")
    print(f"Strict suggestions ........... {len(result.suggestions)}")
    print(f"Near-misses (rejected) ....... {len(result.rejected)}")

    if result.suggestions:
        print("\n" + "-" * 78)
        print(f"SUGGESTED PAIRS (top {min(args.show, len(result.suggestions))} by confidence)")
        print("-" * 78)
        print(f"{'jaccard':>7}  {'ovlp':>4}  {'idf':>4}  {'namf':>4}  user-id / name")
        for s in result.suggestions[:args.show]:
            print(f"{s.jaccard:>7.3f}  {s.overlap:>4}  {s.id_files:>4}  {s.name_files:>4}  "
                  f"{s.user_id}  ->  {s.name}")

    print("\n" + "-" * 78)
    print(f"Full results written to: {csv_path}")
    print("-" * 78 + "\n")


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s",
                        datefmt="%Y-%m-%d %H:%M:%S")
    args = parse_args()
    api_key, hydrus_addr, service_name = resolve_settings(args)

    client = hydrus.Client(api_key, hydrus_addr)
    try:
        tag_service_key = get_service_key_by_name(client, service_name)
    except hydrus.ConnectionError:
        sys.exit(f"Error: could not connect to Hydrus at {hydrus_addr}")

    logging.info("Searching Hydrus for user-id / user-name tagged files...")
    try:
        file_ids = search_tagged_file_ids(client, tag_service_key)
    except hydrus.ConnectionError:
        sys.exit(f"Error: could not connect to Hydrus at {hydrus_addr}")

    if not file_ids:
        print("No files with vrchat-user-id:* or vrchat-user-name:* tags found.")
        return

    logging.info(f"Fetching metadata for {len(file_ids)} files...")
    files, id_hashes, name_hashes = fetch_files(client, file_ids)

    logging.info("Correlating...")
    result = correlate(
        files,
        min_overlap=args.min_overlap,
        min_jaccard=args.min_jaccard,
        max_runner_up_ratio=args.max_runner_up_ratio,
    )

    csv_path = Path(args.out)
    write_csv(csv_path, result.suggestions, id_hashes, name_hashes)
    print_report(result, args, csv_path)


if __name__ == "__main__":
    main()
