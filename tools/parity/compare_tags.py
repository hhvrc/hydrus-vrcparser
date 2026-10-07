"""Diff two per-file tag dumps (before/after a change).

This is the hard gate: anything other than "identical for every file" means the
change would push different tags than before.
"""
import json
import sys
from collections import Counter

BEFORE_PATH, AFTER_PATH = sys.argv[1], sys.argv[2]
SHOW = int(sys.argv[3]) if len(sys.argv) > 3 else 8


def load(path):
    out = {}
    with open(path, encoding="utf-8") as f:
        for line in f:
            r = json.loads(line)
            out[r["file_id"]] = r
    return out


def main():
    before, after = load(BEFORE_PATH), load(AFTER_PATH)

    only_before = sorted(set(before) - set(after))
    only_after = sorted(set(after) - set(before))
    shared = sorted(set(before) & set(after))

    if only_before or only_after:
        print(f"KEY MISMATCH: {len(only_before)} only in BEFORE, {len(only_after)} only in AFTER")
        for fid in only_before[:SHOW]:
            print(f"    only BEFORE: file_id={fid}")
        for fid in only_after[:SHOW]:
            print(f"    only AFTER:  file_id={fid}")

    kinds = Counter()
    examples = {}
    tagged = 0

    for fid in shared:
        a, b = before[fid], after[fid]
        if a["tags"] is not None:
            tagged += 1

        if (a["tags"] is None) != (b["tags"] is None):
            kind = "one side produced no metadata"
        elif a["tags"] != b["tags"]:
            kind = "tag sets differ"
        elif a["tag_hash"] != b["tag_hash"]:
            # Would mean the two sides sorted or encoded identical tags
            # differently -- the code point ordering trap.
            kind = "same tags, different hash"
        else:
            continue

        kinds[kind] += 1
        examples.setdefault(kind, []).append((fid, a, b))

    print(f"compared {len(shared)} files ({tagged} with tags in BEFORE)")

    if not kinds and not only_before and not only_after:
        print("\nTAG PARITY: identical for every file.")
        return 0

    for kind, count in kinds.most_common():
        print(f"\n  {kind}: {count}")
        for fid, a, b in examples[kind][:SHOW]:
            print(f"    file_id={fid}")
            if a["tags"] != b["tags"]:
                missing = sorted(set(a["tags"] or []) - set(b["tags"] or []))
                extra = sorted(set(b["tags"] or []) - set(a["tags"] or []))
                print(f"      only in BEFORE: {missing!r}")
                print(f"      only in AFTER:  {extra!r}")
            else:
                print(f"      before hash: {a['tag_hash']}")
                print(f"      after hash:  {b['tag_hash']}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
