from datetime import datetime, timezone
from pathlib import Path
import logging
from typing import List, Optional


def now_utc_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def chunked(seq: List, n: int):
    for i in range(0, len(seq), n):
        yield seq[i:i + n]


def write_text_to_folder(folder_path: Path, file_name: str, text: str) -> None:
    try:
        folder_path.mkdir(parents=True, exist_ok=True)
        (folder_path / file_name).write_text(text, encoding="utf-8")
    except Exception as e:
        logging.error(f"Failed to write {folder_path / file_name}: {e}")


def sanitize_itxt_text(text: Optional[str]) -> str:
    """
    Clean up raw iTXt text from PNG chunks.

    - Remove any leading NUL bytes (\x00), which some exporters prepend.
    - Remove a UTF-8 BOM if present.
    - Ensure we always return a str (never None).
    """
    if not text:
        return ""
    t = text.lstrip("\x00")
    if t.startswith("\ufeff"):
        t = t.lstrip("\ufeff")
    return t.strip()
