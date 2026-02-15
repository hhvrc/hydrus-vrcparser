from datetime import datetime
from typing import Dict, List, Tuple

def build_tag_mappings(metadata: Dict[int, dict]) -> List[Tuple[str, str]]:
    """Build parent→child tag mappings for authors/worlds/players."""
    mappings: Dict[str, set] = {}

    def add(parent: str, child: str):
        mappings.setdefault(parent, set()).add(child)

    for _, meta in metadata.items():
        author = (meta.get("author") or {})
        author_id = (author.get("id") or "").strip()
        author_name = (author.get("displayName") or "").strip()
        if author_id and author_name:
            add(f"vrchat-user-id:{author_id}", f"vrchat-user-name:{author_name}")
            add(f"vrchat-author-id:{author_id}", f"vrchat-author-name:{author_name}")

        world = (meta.get("world") or {})
        world_id = (world.get("id") or "").strip()
        world_name = (world.get("name") or "").strip()
        if world_id and world_name:
            add(f"vrchat-world-id:{world_id}", f"vrchat-world-name:{world_name}")

        for player in (meta.get("players") or []):
            player_id = (player.get("id") or "").strip()
            player_name = (player.get("displayName") or "").strip()
            if player_id and player_name:
                add(f"vrchat-user-id:{player_id}", f"vrchat-user-name:{player_name}")

    return [(p, c) for p, children in mappings.items() for c in children]

def build_file_id_to_tags(all_meta: Dict[int, dict], existing: Dict[int, List[str]] | None = None) -> Dict[int, List[str]]:
    """Return {file_id: [tags...]} derived from parsed metadata.
    If existing entries are provided, append new tags without duplicates.
    """
    m: Dict[int, List[str]] = existing.copy() if existing else {}

    for file_id, meta in all_meta.items():
        tags: List[str] = ["vrchat"]

        author = meta.get("author") or {}
        author_id = (author.get("id") or "").strip()
        author_name = (author.get("displayName") or "").strip()
        if author_id:
            tags.append(f"vrchat-author-id:{author_id}")
        if author_name:
            tags.append(f"vrchat-author-name:{author_name}")

        world = meta.get("world") or {}
        world_id = (world.get("id") or "").strip()
        world_name = (world.get("name") or "").strip()
        instance_id = (world.get("instanceId") or "").strip()
        if world_id:
            tags.append(f"vrchat-world-id:{world_id}")
        if world_name:
            tags.append(f"vrchat-world-name:{world_name}")
        if instance_id:
            tags.append(f"vrchat-world-instanceId:{instance_id}")

        for player in meta.get("players") or []:
            player_id = (player.get("id") or "").strip()
            player_name = (player.get("displayName") or "").strip()
            if player_id:
                tags.append(f"vrchat-user-id:{player_id}")
            if player_name:
                tags.append(f"vrchat-user-name:{player_name}")

        created = meta.get("created")
        if isinstance(created, datetime):
            tags.append(f"vrchat-date:{created.strftime('%Y-%m-%d')}")

        # merge with existing tags if present
        if file_id in m:
            existing_tags = set(m[file_id])
            existing_tags.update(tags)
            m[file_id] = sorted(existing_tags)
        else:
            m[file_id] = tags

    return m