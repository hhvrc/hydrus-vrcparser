import argparse
import sys
from pathlib import Path
from urllib.parse import urlparse

def parse_args():
    p = argparse.ArgumentParser(
        description=(
            "Extract embedded PNG metadata from Hydrus files and push tags back to Hydrus, "
            "with a lean SQLite cache and a function-name–based migration ledger."
        )
    )
    p.add_argument("--api-key", help="Hydrus API key (string)")
    p.add_argument("--hydrus-addr", help="Hydrus client address (e.g. http://localhost:45869)")
    p.add_argument("--data-dir", help="Path to the Hydrus data directory containing files")
    p.add_argument("--service-name", default=None, help="Hydrus tag service name")
    p.add_argument("--db", help="SQLite database path (default: ./vrchat.db)")
    p.add_argument("--config", default="config.json", help="Path to config file (default: config.json)")
    return p.parse_args()

def validate_args(args):
    if not args.api_key or not args.api_key.strip():
        sys.exit("Error: missing api_key. Provide --api-key or set it in config.json.")

    parsed = urlparse(args.hydrus_addr or "")
    if parsed.scheme not in ("http", "https") or not parsed.netloc:
        sys.exit(f"Error: invalid --hydrus-addr URL (from CLI or config): {args.hydrus_addr}")

    if not args.data_dir:
        sys.exit("Error: missing data_dir. Provide --data-dir or set it in config.json.")
    data_dir = Path(args.data_dir)
    if not data_dir.exists() or not data_dir.is_dir():
        sys.exit(f"Error: data directory does not exist or is not a directory: {data_dir}")

    args.db.parent.mkdir(parents=True, exist_ok=True)
