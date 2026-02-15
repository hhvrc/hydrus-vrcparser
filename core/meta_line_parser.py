from typing import Any, Dict, List
import logging


class MetaParseError(ValueError):
    """Raised when metadata parsing fails due to invalid format."""
    pass


def parse_meta_line(line: str) -> Dict[str, Any]:
    """
    Parse a metadata line from 'screenshotmanager' or 'lfs' format.

    Returns a dict with keys:
      - raw_text: str
      - type: 'screenshotmanager' or 'lfs'
      - index: int
      - author: {'id': str, 'displayName': str}
      - world: {'id': str, 'instanceId': str, 'name': str}
      - position: {'x': float, 'y': float, 'z': float}
      - rq: int
      - players: List[{'id': str, 'position': {'x':float,'y':float,'z':float}, 'displayName': str}]
    """
    # initialize defaults
    meta: Dict[str, Any] = {
        'raw_text': line,
        'type': None,
        'index': None,
        'author': {'id': '', 'displayName': ''},
        'world': {'id': '', 'instanceId': '', 'name': ''},
        'position': {'x': 0.0, 'y': 0.0, 'z': 0.0},
        'rq': 0,
        'players': [],
    }

    parts = [p.strip() for p in line.split('|')]
    if len(parts) < 2:
        raise MetaParseError(f"Line must have at least type and index: {line!r}")

    # type & index
    meta_type = parts[0]
    if meta_type not in ('screenshotmanager', 'lfs'):
        raise MetaParseError(f"Unknown meta type: {meta_type!r}")
    meta['type'] = meta_type

    try:
        meta['index'] = int(parts[1])
    except ValueError:
        raise MetaParseError(f"Invalid index (must be int): {parts[1]!r}")

    # mapping for field parsers
    field_parsers = {
        'author': _parse_author,
        'world': _parse_world,
        'pos': _parse_pos,
        'rq': _parse_request,
        'players': _parse_players,
    }

    # parse remaining segments
    for seg in parts[2:]:
        # Handle bare world segments (e.g. "wrld_xxx,instanceId,worldName")
        # from older screenshotmanager format that omits the "world:" prefix
        if seg.startswith('wrld_'):
            try:
                _parse_world(seg, meta)
            except MetaParseError as e:
                logging.warning(f"Failed to parse bare world segment: {e}")
            continue

        if ':' not in seg:
            continue
        key, val = seg.split(':', 1)
        parser = field_parsers.get(key)
        if parser:
            try:
                parser(val, meta)
            except MetaParseError as e:
                logging.warning(f"Failed to parse {key}: {e}")
                # Continue parsing other fields even if one fails
        # unknown keys are ignored

    return meta


def _parse_author(val: str, meta: Dict[str, Any]) -> None:
    try:
        auth_id, auth_name = val.split(',', 1)
    except ValueError:
        raise MetaParseError(f"Invalid author format: {val!r}")
    meta['author'] = {'id': auth_id, 'displayName': auth_name}


def _parse_world(val: str, meta: Dict[str, Any]) -> None:
    try:
        w_id, w_inst, w_name = val.split(',', 2)
    except ValueError:
        raise MetaParseError(f"Invalid world format: {val!r}")
    meta['world'] = {'id': w_id, 'instanceId': w_id + ':' + w_inst, 'name': w_name}


def _parse_pos(val: str, meta: Dict[str, Any]) -> None:
    coords = val.split(',')
    if len(coords) != 3:
        raise MetaParseError(f"Invalid pos format, needs 3 floats: {val!r}")
    try:
        x, y, z = (float(c) for c in coords)
    except ValueError:
        raise MetaParseError(f"Non-numeric pos values: {val!r}")
    meta['position'] = {'x': x, 'y': y, 'z': z}


def _parse_request(val: str, meta: Dict[str, Any]) -> None:
    try:
        meta['rq'] = int(val)
    except ValueError:
        raise MetaParseError(f"Invalid rq value (must be int): {val!r}")


def _parse_players(val: str, meta: Dict[str, Any]) -> None:
    """Parse players list, skipping malformed entries instead of failing completely."""
    players: List[Dict[str, Any]] = []
    for entry in val.split(';'):
        parts_p = entry.split(',', 4)
        if len(parts_p) != 5:
            logging.warning(f"Skipping malformed player entry: {entry!r}")
            continue
        pid, sx, sy, sz, name = parts_p
        try:
            px, py, pz = float(sx), float(sy), float(sz)
        except ValueError:
            logging.warning(f"Skipping player with invalid coords: {entry!r}")
            continue
        players.append({
            'id': pid,
            'position': {'x': px, 'y': py, 'z': pz},
            'displayName': name
        })
    meta['players'] = players


__all__ = ['MetaParseError', 'parse_meta_line']