#!/usr/bin/env python3
"""
Add twitter-username:[USERNAME] tags to Hydrus files with x.com URLs.

Args:
  --api-key <KEY>             Hydrus API key (saved to config.json after first use)
  --service-name <NAME>       local tag service to push to (saved to config.json)
  --dry-run                   optional; log actions but don't change anything

Behavior:
  - search for files that have a URL with domain x.com
  - fetch metadata in batches
  - extract username from URLs (first path segment)
  - push tags 'twitter-username:[USERNAME]' to the chosen local tag service

Config:
  - api-key and service-name persist to config.json next to this script
  - CLI args override config.json values and are written back
"""

import argparse
import json
import logging
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional
from urllib.parse import urlparse

import hydrus_api as hydrus

# ---- Config ----
API_URL = "http://127.0.0.1:45869"
BATCH = 250
DOMAIN_HOSTS = {"x.com", "www.x.com", "mobile.x.com"}
CONFIG_PATH = Path(__file__).resolve().parent / "config.json"


def load_config() -> Dict[str, Any]:
    """Load config.json next to this script; return {} if missing/invalid."""
    try:
        with open(CONFIG_PATH, "r", encoding="utf-8") as f:
            data = json.load(f)
        return data if isinstance(data, dict) else {}
    except FileNotFoundError:
        return {}
    except (json.JSONDecodeError, OSError) as e:
        logging.warning(f"Could not read config {CONFIG_PATH}: {e}")
        return {}


def save_config(config: Dict[str, Any]) -> None:
    """Persist config to config.json next to this script."""
    try:
        with open(CONFIG_PATH, "w", encoding="utf-8") as f:
            json.dump(config, f, indent=2, sort_keys=True)
        logging.info(f"Saved config to {CONFIG_PATH}")
    except OSError as e:
        logging.warning(f"Could not write config {CONFIG_PATH}: {e}")


# ---- Helpers ----
def chunked(seq: List[int], n: int) -> Iterable[List[int]]:
    for i in range(0, len(seq), n):
        yield seq[i:i+n]


def _urls_from_meta_row(row: Dict[str, Any]) -> List[str]:
    urls = []
    for key in ("known_urls", "urls"):
        val = row.get(key)
        if isinstance(val, list):
            urls.extend([u for u in val if isinstance(u, str)])
    out = []
    for u in urls:
        try:
            host = urlparse(u).netloc.lower()
            if host in DOMAIN_HOSTS:
                out.append(u)
        except Exception:
            continue
    return out


def _username_from_url(u: str) -> Optional[str]:
    try:
        parsed = urlparse(u)
        path = parsed.path.strip("/")
        if not path:
            return None
        first_segment = path.split("/", 1)[0]
        if first_segment.lower() in {"home", "i", "explore", "search", "settings"}:
            return None
        return first_segment
    except Exception:
        return None


def _collect_local_tag_services(client: hydrus.Client) -> Dict[str, Dict[str, Any]]:
    """Return all local tag services (type=5), deduped by service_key."""
    svc_obj = client.get_services()
    # Dedupe by service_key: the same service appears under several keys of the
    # response (e.g. "local_tags", "services", "services_v2"), so collect into a
    # dict to avoid counting one service multiple times.
    locals_by_key: Dict[str, Dict[str, Any]] = {}

    def consider(svc, fallback_key=None):
        if not isinstance(svc, dict) or int(svc.get("type", -1)) != 5:
            return
        key = svc.get("service_key") or fallback_key
        if not key:
            return
        entry = dict(svc)
        entry.setdefault("service_key", key)
        locals_by_key[key] = entry

    def scan_list(lst):
        for s in lst:
            consider(s)

    if isinstance(svc_obj, dict):
        for k, v in svc_obj.items():
            if isinstance(v, list):
                scan_list(v)
            elif k == "services" and isinstance(v, dict):
                # modern format: dict keyed by service_key
                for sk, sv in v.items():
                    consider(sv, fallback_key=sk)
    elif isinstance(svc_obj, list):
        scan_list(svc_obj)

    return locals_by_key


def resolve_local_tag_service_key(client: hydrus.Client, service_name: Optional[str] = None) -> str:
    """Resolve a local tag service (type=5) to its key.

    If service_name is given, pick the matching service by name. Otherwise
    require exactly one local tag service.
    """
    locals_by_key = _collect_local_tag_services(client)
    locals_found = list(locals_by_key.values())

    if not locals_found:
        raise SystemExit("ERROR: no local tag service (type=5) found")

    if service_name:
        matches = [s for s in locals_found if s.get("name") == service_name]
        if not matches:
            available = [f"{s.get('name')} (key={s.get('service_key')})" for s in locals_found]
            raise SystemExit(
                f"ERROR: no local tag service named '{service_name}'. Available:\n  - "
                + "\n  - ".join(available)
            )
        if len(matches) > 1:
            raise SystemExit(f"ERROR: multiple local tag services named '{service_name}'")
        svc = matches[0]
    else:
        if len(locals_found) > 1:
            names = [f"{s.get('name')} (key={s.get('service_key')})" for s in locals_found]
            raise SystemExit(
                "ERROR: multiple local tag services found; pass --service-name to choose:\n  - "
                + "\n  - ".join(names)
            )
        svc = locals_found[0]

    key = svc.get("service_key")
    name = svc.get("name")
    if not key:
        raise SystemExit("ERROR: local tag service has no service_key")
    logging.info(f"Using local tag service: {name} ({key})")
    return key

# ---- Main ----


def main():
    ap = argparse.ArgumentParser(description="Tag x.com URLs with twitter-username:[USERNAME].")
    ap.add_argument("--api-key", help="Hydrus API key (saved to config.json)")
    ap.add_argument("--service-name", help="Local tag service to push to (saved to config.json)")
    ap.add_argument("--dry-run", action="store_true", help="Log actions without making changes")
    args = ap.parse_args()

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )

    # Merge CLI args over config.json; CLI values persist back.
    config = load_config()
    api_key = args.api_key or config.get("api_key")
    service_name = args.service_name or config.get("service_name")

    if not api_key:
        raise SystemExit("ERROR: no API key; pass --api-key once to save it to config.json")

    new_config = dict(config)
    new_config["api_key"] = api_key
    if service_name:
        new_config["service_name"] = service_name
    if new_config != config:
        save_config(new_config)

    client = hydrus.Client(api_key, API_URL)

    service_key = resolve_local_tag_service_key(client, service_name)

    # 1) Find files that have a URL with domain x.com
    try:
        res = client.search_files(["system:has url with domain x.com"])
    except Exception as e:
        logging.error(f"search_files failed: {e}")
        raise SystemExit(2)

    file_ids: List[int] = res.get("file_ids", []) or []
    if not file_ids:
        logging.info("Found 0 files with URLs on x.com; nothing to tag.")
        return
    logging.info(f"Found {len(file_ids)} files with URLs on x.com")

    # 2) Fetch metadata in batches and push username tags
    total_tags = 0
    for batch in chunked(file_ids, BATCH):
        try:
            meta_rows = client.get_file_metadata(file_ids=batch).get("metadata", [])
        except Exception as e:
            logging.error(f"get_file_metadata failed for batch: {e}")
            continue

        for row in meta_rows:
            h = row.get("hash")
            if not h:
                continue
            urls = _urls_from_meta_row(row)
            for u in urls:
                user = _username_from_url(u)
                if not user:
                    continue
                tag = f"twitter-username:{user}"
                if args.dry_run:
                    logging.info(f"[DRY] add_tags {h[:8]}… -> {tag}")
                    total_tags += 1
                else:
                    try:
                        client.add_tags(hashes=[h], service_keys_to_tags={service_key: [tag]})
                        total_tags += 1
                    except Exception as e:
                        logging.error(f"add_tags failed for {h[:8]}…: {e}")

    logging.info(f"Done. Tags added: {total_tags}{' (dry run)' if args.dry_run else ''}.")


if __name__ == "__main__":
    main()
