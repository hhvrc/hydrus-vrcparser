import json
import logging
import xml.etree.ElementTree as ET
from pathlib import Path
from typing import List, Optional, Tuple

from .constants import PNG_HEADER, ITXT_KEY_DESCRIPTION, ITXT_KEY_ADOBEXMPXML
from .utils import write_text_to_folder, sanitize_itxt_text
from .meta_line_parser import parse_meta_line


def _iter_itxt_chunks(f):
    """Yield (offset, "iTXt", data) for each iTXt chunk in a PNG file."""
    # Verify PNG signature
    f.seek(0)
    sig = f.read(len(PNG_HEADER))
    if sig != PNG_HEADER:
        raise ValueError("Not a valid PNG file")

    while True:
        offset = f.tell()
        header = f.read(8)  # 4 bytes length, 4 bytes type
        if len(header) < 8:
            break

        size = int.from_bytes(header[:4], "big")
        typ = header[4:8]  # bytes, e.g. b'iTXt'

        if typ == b'iTXt':
            data = f.read(size)
            f.seek(4, 1)  # skip CRC
            yield offset, "iTXt", data
            # don't break on iTXt; there may be more
        elif typ == b'IEND':
            # skip its (empty) payload + CRC, then stop
            f.seek(size + 4, 1)
            break
        else:
            # fast-path skip: don't read the payload into Python
            f.seek(size + 4, 1)  # payload + CRC


def _parse_itxt_descriptors(data: bytes) -> Optional[Tuple[str, int, int, str, str, str]]:
    """
    Returns (keyword, compression_flag, compression_method, language_tag, translated_keyword, text) or None.
    NOTE: Compressed iTXt (compression_flag==1) is not handled here.

    iTXt binary layout (PNG spec):
      keyword \\x00 comp_flag(1 byte) comp_method(1 byte) language_tag \\x00 translated_keyword \\x00 text
    The compression flag and method are raw bytes, NOT null-terminated strings,
    so we must parse them positionally rather than splitting on null bytes.
    """
    # Find keyword (null-terminated)
    null_pos = data.find(b"\x00")
    if null_pos < 0 or null_pos + 2 >= len(data):
        return None

    keyword = data[:null_pos].decode("utf-8", errors="replace")
    comp_flag = data[null_pos + 1]
    comp_method = data[null_pos + 2]

    # Remainder after the two flag bytes: language_tag \x00 translated_keyword \x00 text
    remainder = data[null_pos + 3:]
    parts = remainder.split(b"\x00", 2)
    if len(parts) != 3:
        return None

    language_tag = parts[0].decode("utf-8", errors="replace")
    translated_keyword = parts[1].decode("utf-8", errors="replace")
    text = parts[2].decode("utf-8", errors="replace")
    return keyword, comp_flag, comp_method, language_tag, translated_keyword, text

def _is_xmp_xml(text: str) -> bool:
    """Check if text is valid XMP XML by parsing it."""
    if not text or not text.lstrip().startswith("<"):
        return False
    try:
        root = ET.fromstring(text)
        # Accept x:xmpmeta root or bare rdf:RDF
        tag = root.tag
        return tag.endswith("}xmpmeta") or tag.endswith("}RDF")
    except ET.ParseError:
        return False


def _detect_format(text: str, keyword: str = "") -> Optional[str]:
    """Detect content type of iTXt text.

    Uses the keyword as a hint when available (e.g. XML:com.adobe.xmp is always XML).
    """
    # XMP keyword is always XML regardless of content
    if keyword == ITXT_KEY_ADOBEXMPXML:
        return "xml"

    try:
        json.loads(text)
        return "json"
    except Exception:
        pass

    if _is_xmp_xml(text):
        return "xml"

    try:
        parse_meta_line(text)
        return "line"
    except Exception:
        pass

    return None

def extract_itxt_records_and_description(path: Path, id_hash: str, broken_dir: Path):
    """
    Read PNG chunks and collect iTXt descriptors.

    Returns:
      descriptors: List[(seq, keyword, comp_flag, comp_method, lang, trans, text, content_type)]
                   content_type ∈ {'text','json','xml'} (future-safe for more types)
      parsed_ok: bool     → True if we successfully parsed a Description (json/line) or XMP xml
      io_error: bool
    """
    descriptors: List[Tuple[int, Optional[str], Optional[int], Optional[int], Optional[str], Optional[str], Optional[str], str]] = []
    parsed_ok = False
    io_error = False

    try:
        with path.open("rb") as f:
            seq = 0
            for _, _, data in _iter_itxt_chunks(f):
                parsed = _parse_itxt_descriptors(data)
                if parsed is None:
                    descriptors.append((seq, None, None, None, None, None, None, "text"))
                    seq += 1
                    continue

                keyword, comp_flag, comp_method, language_tag, translated_keyword, text = parsed
                raw_text = sanitize_itxt_text(text)
                content_type = _detect_format(raw_text, keyword)
                if content_type is None:
                    logging.error(f"Could not parse iTXt keyword '{keyword}' in {path}")
                    if keyword == ITXT_KEY_DESCRIPTION:
                        write_text_to_folder(broken_dir, id_hash + ".txt", raw_text)
                    elif keyword == ITXT_KEY_ADOBEXMPXML:
                        write_text_to_folder(broken_dir, id_hash + "_xmp.txt", raw_text)

                if content_type in ("json", "line", "xml"):
                    parsed_ok = True

                # Store the SANITIZED text
                descriptors.append(
                    (seq, keyword, comp_flag, comp_method, language_tag, translated_keyword, raw_text, content_type or "text")
                )
                seq += 1

    except (FileNotFoundError, PermissionError, OSError) as e:
        logging.error(f"I/O error reading {path}: {e} (will retry later)")
        io_error = True
    except Exception as e:
        logging.error(f"Error processing {path}: {e}")

    return descriptors, parsed_ok, io_error
