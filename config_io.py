import json
import logging
from argparse import Namespace
from pathlib import Path

def load_config(path: Path) -> dict:
    if path.exists():
        try:
            return json.loads(path.read_text(encoding="utf-8"))
        except Exception as e:
            logging.error(f"Failed to read {path}: {e}")
    return {}

def save_config(cfg: dict, path: Path) -> None:
    try:
        path.write_text(json.dumps(cfg, indent=2), encoding="utf-8")
    except Exception as e:
        logging.error(f"Failed to write {path}: {e}")

def merge_args_with_config(args) -> Namespace:
    """
    Merge CLI args with the config file (CLI wins). Also persists the result.
    """
    config_path = Path(args.config or "config.json")
    cfg = load_config(config_path)

    merged = {
        "api_key": args.api_key or cfg.get("api_key"),
        "hydrus_addr": args.hydrus_addr or cfg.get("hydrus_addr"),
        "data_dir": args.data_dir or cfg.get("data_dir"),
        "service_name": args.service_name or cfg.get("service_name"),
        "db": args.db or cfg.get("db", "./vrchat.db"),
    }

    save_config(merged, config_path)

    return Namespace(
        api_key=merged["api_key"],
        hydrus_addr=merged["hydrus_addr"],
        data_dir=Path(merged["data_dir"]) if merged.get("data_dir") else None,
        service_name=merged["service_name"],
        db=Path(merged["db"]) if merged.get("db") else Path("./vrchat.db"),
        config=str(config_path),
    )
