"""
Core utilities for PNG/iTXt extraction and tag building.
Re-exports the most-used pieces for convenient imports:

    from core import PNG_HEADER, ITXT_KEY_DESCRIPTION, BATCH_SIZE
    from core import now_utc_iso, chunked, write_text_to_folder
    from core import iter_png_chunks, parse_itxt_descriptors, extract_itxt_records_and_description
    from core import build_tag_mappings, build_file_id_to_tags
"""

__all__ = [
    # constants
    "BROKEN_DIR", "BATCH_SIZE", "PNG_HEADER", "ITXT_KEY_DESCRIPTION",
    # utils
    "now_utc_iso", "chunked", "write_text_to_folder",
    # png_itxt
    "extract_itxt_records_and_description",
    # tag_builders
    "build_tag_mappings", "build_file_id_to_tags",
    # version
    "__version__",
]

__version__ = "0.1.0"

# Re-exports
from .constants import BROKEN_DIR, BATCH_SIZE, PNG_HEADER, ITXT_KEY_DESCRIPTION
from .utils import now_utc_iso, chunked, write_text_to_folder, sanitize_itxt_text  # noqa: F401
from .png_itxt import extract_itxt_records_and_description
from .tag_builders import build_tag_mappings, build_file_id_to_tags
