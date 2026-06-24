from __future__ import annotations
import json
import re
import xml.etree.ElementTree as ET
from datetime import datetime, timezone
from typing import List, Optional


class XMPParseError(ValueError):
    """Raised when XMP metadata parsing fails due to invalid format."""
    pass


# --- Date parser (provided utility) ---
def _parse_dt(value: Optional[str]) -> Optional[datetime]:
    if not value:
        return None
    s = value.strip()
    try:
        # Normalize trailing Z to +00:00, keep fractional seconds if present
        if s.endswith("Z"):
            s = s[:-1] + "+00:00"
        dt = datetime.fromisoformat(s)
        # Ensure timezone-aware (VRChat writes offset or Z)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return dt
    except Exception:
        return None


# --- Namespaces ---
X_NS = "adobe:ns:meta/"
RDF_NS = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
XML_NS = "http://www.w3.org/XML/1998/namespace"

NS_XMP = "http://ns.adobe.com/xap/1.0/"
NS_TIFF = "http://ns.adobe.com/tiff/1.0/"
NS_DC = "http://purl.org/dc/elements/1.1/"
NS_VRC = "http://ns.vrchat.com/vrc/1.0/"


# --- ID validators: usr_<uuid> and wrld_<uuid> ---
_UUID_RE = r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
USR_RE = re.compile(rf"^usr_{_UUID_RE}$")
WRLD_RE = re.compile(rf"^wrld_{_UUID_RE}$")


def _valid_user_id(s: Optional[str]) -> Optional[str]:
    return s if s and USR_RE.match(s) else None


def _valid_world_id(s: Optional[str]) -> Optional[str]:
    return s if s and WRLD_RE.match(s) else None


# --- Internal helpers ---
def _split_tag(tag: str):
    if tag and tag[0] == "{":
        ns, _, local = tag[1:].partition("}")
        return ns, local
    return None, tag


def _text(e: ET.Element) -> str:
    return (e.text or "").strip()


def _parse_xmp_outer(xml_text: str):
    """Validate and extract rdf:Description children grouped as {ns: {local: element}}.

    Raises:
        XMPParseError: if XML is invalid, root is wrong, rdf:RDF count != 1,
                       or if duplicate elements share the same {namespace, localName}.
    """
    try:
        root = ET.fromstring(xml_text)
    except ET.ParseError as e:
        raise XMPParseError(f"XML parsing failed: {e}") from e

    ns, local = _split_tag(root.tag)
    if ns != X_NS or local != "xmpmeta":
        raise XMPParseError(f"Invalid root element: expected {{{X_NS}}}xmpmeta")

    rdf_children = [c for c in root if _split_tag(c.tag) == (RDF_NS, "RDF")]
    if len(rdf_children) != 1:
        raise XMPParseError("Expected exactly one rdf:RDF child element")
    rdf = rdf_children[0]

    grouped: dict[str, dict[str, ET.Element]] = {}
    for desc in rdf:
        d_ns, d_local = _split_tag(desc.tag)
        if (d_ns, d_local) != (RDF_NS, "Description"):
            continue
        for elem in desc:
            e_ns, e_local = _split_tag(elem.tag)
            if not e_ns:
                # Skip elements without a namespace
                continue
            ns_bucket = grouped.setdefault(e_ns, {})
            if e_local in ns_bucket:
                raise XMPParseError(
                    f"Duplicate element for {{{e_ns}}}{e_local} in XMP metadata"
                )
            ns_bucket[e_local] = elem

    return grouped


# --- Main public API ---
def parse_xmp_meta(xml_text: str):
    """
    Parse a VRChat XMP metadata XML string.

    Returns:
        dict: {
            "raw_xml": str,
            "type": "xmp",
            "author": {"id": str, "displayName": str | None},
            "created": datetime | None,
            "modified": datetime | None,
            "tiff_datetime": datetime | None,
            "world": {"id": str, "name": str | None},
        }

    Rules:
      - Normal form (preferred):
          vrc:WorldID, vrc:WorldDisplayName, vrc:AuthorID
          => xmp:Author is treated as a human-readable name.
      - Compact form:
          vrc:World (contains an ID)
          => xmp:Author MAY contain a user ID (usr_...), which is parsed into authorId.
             If xmp:Author is not a usr_..., it remains a name.
      - Author is ONLY treated as an ID when compact form is detected.

    Raises:
        XMPParseError for structural errors or if no valid identifiers are present.
    """
    grouped = _parse_xmp_outer(xml_text)

    # Stash fields
    author_name: Optional[str] = None
    author_id: Optional[str] = None
    xmp_create: Optional[datetime] = None
    xmp_modify: Optional[datetime] = None
    tiff_dt: Optional[datetime] = None
    world_id: Optional[str] = None
    world_name: Optional[str] = None
    compact_world_present = False  # True if <vrc:World> exists

    def _text_of(ns: str, local: str) -> Optional[str]:
        e = grouped.get(ns, {}).get(local)
        return _text(e) if e is not None else None

    # --- XMP core ---
    creator_tool = _text_of(NS_XMP, "CreatorTool")

    raw_author = _text_of(NS_XMP, "Author")
    xmp_create = _parse_dt(_text_of(NS_XMP, "CreateDate"))
    xmp_modify = _parse_dt(_text_of(NS_XMP, "ModifyDate"))

    # --- TIFF ---
    tiff_dt = _parse_dt(_text_of(NS_TIFF, "DateTime"))

    # --- VRC ---
    wid_val = _text_of(NS_VRC, "WorldID")
    if wid_val:
        world_id = _valid_world_id(wid_val) or world_id

    wname_val = _text_of(NS_VRC, "WorldDisplayName")
    if wname_val:
        world_name = wname_val or world_name

    aid_val = _text_of(NS_VRC, "AuthorID")
    if aid_val:
        author_id = _valid_user_id(aid_val) or author_id

    world_compact_val = _text_of(NS_VRC, "World")
    if world_compact_val is not None:
        compact_world_present = True
        world_id = _valid_world_id(world_compact_val) or world_id

    # --- Interpret xmp:Author based on compact/normal form ---
    if compact_world_present:
        # Only in compact form can Author be a user ID
        if raw_author and _valid_user_id(raw_author):
            if not author_id:  # don't override explicit vrc:AuthorID if it exists
                author_id = raw_author
            author_name = None
        else:
            author_name = raw_author
    else:
        # Normal form: Author is a name, never treated as ID
        author_name = raw_author

    # --- Require at least one VRC namespace identifier ---
    if not world_id and not author_id:
        raise XMPParseError("Missing or invalid VRChat identifiers in XMP metadata")

    return {
        "raw_xml": xml_text,
        "type": "xmp",
        "creator_tool": creator_tool,
        "author": {"id": author_id, "displayName": author_name},
        "created": xmp_create,
        "modified": xmp_modify,
        "tiff_datetime": tiff_dt,
        "world": {"id": world_id, "name": world_name},
    }


def extract_embedded_vrcx_json(xml_text: str) -> Optional[dict]:
    """Extract VRCX JSON metadata embedded inside an XMP packet.

    VRChat screenshots that are later opened and re-saved by Adobe apps
    (Photoshop Express, Lightroom, etc.) preserve the original VRCX JSON by
    stuffing it into the Dublin Core ``dc:description`` field, typically nested
    as ``<dc:description><rdf:Alt><rdf:li>{...VRCX JSON...}</rdf:li></rdf:Alt>``.

    The native VRChat XMP namespace (``vrc:``) is absent in these files, so
    ``parse_xmp_meta`` rejects them. This recovers the embedded JSON so it can be
    parsed by the standard VRCX JSON path.

    Returns the decoded JSON dict (containing at least ``world`` or ``author``),
    or ``None`` if no embedded VRCX JSON is present or the XML is unparseable.
    """
    try:
        root = ET.fromstring(xml_text)
    except ET.ParseError:
        return None

    for elem in root.iter():
        txt = (elem.text or "").strip()
        # Cheap pre-filter before attempting a JSON decode on every text node
        if not txt or txt[0] != "{":
            continue
        if '"world"' not in txt and '"author"' not in txt:
            continue
        try:
            data = json.loads(txt)
        except (ValueError, TypeError):
            continue
        if isinstance(data, dict) and (data.get("world") or data.get("author")):
            return data
    return None


def extract_editor_software(xml_text: str) -> List[str]:
    """Return the names of software that created/edited the image, per its XMP.

    Collects ``xmp:CreatorTool`` plus every ``stEvt:softwareAgent`` recorded in
    the ``xmpMM:History`` edit log. Both can appear either as elements (native
    VRChat XMP) or as attributes (Adobe's compact RDF form), so we match by
    local name across both. Order-preserving and de-duplicated.

    Used to tag images by the app they were opened/re-saved in (Adobe, GIMP,
    etc.). Returns an empty list if the XML is unparseable or no agent is named.
    """
    try:
        root = ET.fromstring(xml_text)
    except ET.ParseError:
        return []

    found: List[str] = []

    def _add(value: Optional[str]) -> None:
        v = (value or "").strip()
        if v and v not in found:
            found.append(v)

    for elem in root.iter():
        _, local = _split_tag(elem.tag)
        if local in ("CreatorTool", "softwareAgent"):
            _add(elem.text)
        for attr_key, attr_val in elem.attrib.items():
            _, attr_local = _split_tag(attr_key)
            if attr_local in ("CreatorTool", "softwareAgent"):
                _add(attr_val)

    return found


__all__ = [
    "XMPParseError",
    "parse_xmp_meta",
    "extract_embedded_vrcx_json",
    "extract_editor_software",
]
