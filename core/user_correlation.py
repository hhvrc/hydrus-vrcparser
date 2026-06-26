"""Infer which vrchat-user-id tags correlate with which vrchat-user-name tags.

VRChat metadata emits a player's id *and* displayName together for every
screenshot that player appears in. So a true (id, name) pair for one user
should appear on nearly the same set of files. When both sides always
co-occur (identical file sets) the pair is already trivially linked
("paired") and needs no inference. The interesting cases are ids and names
whose file sets strongly overlap but diverged -- typically because the name
(or id) failed to parse on some of that user's screenshots.

This module is pure (no Hydrus / no I/O) so it can be unit tested. The IO and
reporting live in ``correlate_users.py``.
"""
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from typing import Dict, Iterable, List, Set, Tuple

ID_PREFIX = "vrchat-user-id:"
NAME_PREFIX = "vrchat-user-name:"


@dataclass
class Suggestion:
    user_id: str
    name: str
    overlap: int          # files where both the id and the name appear
    id_files: int         # files carrying the id
    name_files: int       # files carrying the name
    jaccard: float        # overlap / union of the two file sets
    runner_up_jaccard: float  # best competing name's jaccard (ambiguity gauge)


@dataclass
class CorrelationResult:
    suggestions: List[Suggestion] = field(default_factory=list)
    paired_pairs: List[Tuple[str, str]] = field(default_factory=list)
    ambiguous_paired_ids: List[str] = field(default_factory=list)
    rejected: List[Suggestion] = field(default_factory=list)
    orphan_ids: List[Tuple[str, int]] = field(default_factory=list)
    orphan_names: List[Tuple[str, int]] = field(default_factory=list)
    n_files: int = 0
    n_ids: int = 0
    n_names: int = 0


def _jaccard(overlap: int, a_files: int, b_files: int) -> float:
    union = a_files + b_files - overlap
    return overlap / union if union else 0.0


def correlate(
    files: Iterable[Tuple[Set[str], Set[str]]],
    *,
    min_overlap: int = 2,
    min_jaccard: float = 0.5,
    max_runner_up_ratio: float = 0.6,
) -> CorrelationResult:
    """Correlate user ids with user names from per-file tag sets.

    ``files`` is an iterable of ``(ids, names)`` tuples, where each element is
    the set of raw id / name values (prefixes already stripped) on one file.

    Strict acceptance for a suggested (id, name):
      * they co-occur on at least ``min_overlap`` files,
      * their file-set Jaccard similarity is at least ``min_jaccard``,
      * they are each other's best partner among the still-unpaired tags
        (mutual best match), and
      * the id's next-best name scores no higher than
        ``max_runner_up_ratio`` x the winner (clear, unambiguous winner).
    """
    id_files: Counter = Counter()
    name_files: Counter = Counter()
    co: Dict[str, Counter] = defaultdict(Counter)        # id -> {name: overlap}
    name_co: Dict[str, Counter] = defaultdict(Counter)   # name -> {id: overlap}

    n_files = 0
    for ids, names in files:
        n_files += 1
        for i in ids:
            id_files[i] += 1
        for n in names:
            name_files[n] += 1
        for i in ids:
            row = co[i]
            for n in names:
                row[n] += 1
                name_co[n][i] += 1

    result = CorrelationResult(n_files=n_files, n_ids=len(id_files), n_names=len(name_files))

    # 1) Already-paired: an id whose full file set is identical to a name's.
    #    Only accept when the match is exclusive 1:1 on both sides -- if two ids
    #    always appear together (so both perfectly overlap the same name), or one
    #    id perfectly overlaps several names, co-occurrence cannot separate them.
    locked_ids: Set[str] = set()
    locked_names: Set[str] = set()
    perfect_names_of: Dict[str, List[str]] = {}
    perfect_ids_of: Dict[str, List[str]] = defaultdict(list)
    for i in id_files:
        perfects = [
            n for n, c in co[i].items()
            if c == id_files[i] and name_files[n] == c
        ]
        if perfects:
            perfect_names_of[i] = perfects
            for n in perfects:
                perfect_ids_of[n].append(i)

    for i, names_ in perfect_names_of.items():
        exclusive = len(names_) == 1 and len(perfect_ids_of[names_[0]]) == 1
        if exclusive:
            result.paired_pairs.append((i, names_[0]))
            locked_ids.add(i)
            locked_names.add(names_[0])
        else:
            result.ambiguous_paired_ids.append(i)
            locked_ids.add(i)
            locked_names.update(names_)

    # 2) Orphans: tags that never co-occur with the opposite kind at all.
    for i, cnt in id_files.items():
        if not co[i]:
            result.orphan_ids.append((i, cnt))
    for n, cnt in name_files.items():
        if not name_co[n]:
            result.orphan_names.append((n, cnt))

    # 3) Restricted graph over still-unpaired ids and unlocked names, then keep
    #    only mutually-best, high-confidence matches.
    def best_name_for(i: str):
        cands = [
            (n, ov, _jaccard(ov, id_files[i], name_files[n]))
            for n, ov in co[i].items() if n not in locked_names
        ]
        cands.sort(key=lambda t: (t[2], t[1]), reverse=True)
        return cands

    def best_id_for(n: str):
        best_i, best_ov, best_jac = None, 0, -1.0
        for i, ov in name_co[n].items():
            if i in locked_ids:
                continue
            jac = _jaccard(ov, id_files[i], name_files[n])
            if (jac, ov) > (best_jac, best_ov):
                best_i, best_ov, best_jac = i, ov, jac
        return best_i

    for i in id_files:
        if i in locked_ids:
            continue
        cands = best_name_for(i)
        if not cands:
            continue
        name, overlap, jac = cands[0]
        runner_up = cands[1][2] if len(cands) > 1 else 0.0
        sugg = Suggestion(
            user_id=i, name=name, overlap=overlap,
            id_files=id_files[i], name_files=name_files[name],
            jaccard=round(jac, 4), runner_up_jaccard=round(runner_up, 4),
        )
        accepted = (
            overlap >= min_overlap
            and jac >= min_jaccard
            and best_id_for(name) == i
            and runner_up <= jac * max_runner_up_ratio
        )
        if accepted:
            result.suggestions.append(sugg)
        else:
            result.rejected.append(sugg)

    result.suggestions.sort(key=lambda s: (s.jaccard, s.overlap), reverse=True)
    result.rejected.sort(key=lambda s: (s.jaccard, s.overlap), reverse=True)
    return result
