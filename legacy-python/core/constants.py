from pathlib import Path

# Paths / folders
BROKEN_DIR = Path("broken_metadata")

# Batch sizes
BATCH_SIZE = 256

# Parser versions
# FILE_PARSER_VERSION: Tracks iTXt extraction from PNG files (expensive disk I/O, rarely changes)
# DATA_PARSER_VERSION: Tracks metadata normalization from cached iTXt chunks (changes with parse logic)
FILE_PARSER_VERSION = 1
# v5: recover VRCX JSON embedded in dc:description of Adobe-edited screenshots
DATA_PARSER_VERSION = 5

# PNG / iTXt constants
PNG_HEADER = b"\x89PNG\r\n\x1a\n"
ITXT_KEY_DESCRIPTION = "Description"
ITXT_KEY_ADOBEXMPXML = "XML:com.adobe.xmp"
