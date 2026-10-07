"""Diff two per-chunk parse dumps (before/after a change), field by field."""
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
            out[(r["file_id"], r["seq"])] = r
    return out


def main():
    before, after = load(BEFORE_PATH), load(AFTER_PATH)

    only_before = set(before) - set(after)
    only_after = set(after) - set(before)
    if only_before or only_after:
        print(f"KEY MISMATCH: {len(only_before)} only in BEFORE, {len(only_after)} only in AFTER")

    fields = [
        "effective_type", "author_id", "author_name", "world_id", "world_name",
        "instance_id", "creator_tool", "editor_software", "created", "players", "error",
    ]

    diffs = Counter()
    examples = {}
    compared = 0

    for key in sorted(set(before) & set(after)):
        a, b = before[key], after[key]
        compared += 1
        for field in fields:
            va, vb = a.get(field), b.get(field)
            if va != vb:
                diffs[field] += 1
                examples.setdefault(field, []).append((key, va, vb))

    print(f"compared {compared} chunks across {len(fields)} fields")

    if not diffs and not only_before and not only_after:
        print("\nPARSER PARITY: identical on every field of every chunk.")
        return 0

    if only_before or only_after:
        # A chunk missing from one dump fails the check even when every shared
        # chunk matches: the two dumps did not cover the same data.
        for key in sorted(only_before)[:SHOW]:
            print(f"    only BEFORE: file_id={key[0]} seq={key[1]}")
        for key in sorted(only_after)[:SHOW]:
            print(f"    only AFTER:  file_id={key[0]} seq={key[1]}")
    if not diffs:
        return 1

    print(f"\nMISMATCHES in {len(diffs)} field(s):")
    for field, count in diffs.most_common():
        pct = 100.0 * count / compared
        print(f"\n  {field}: {count} ({pct:.2f}%)")
        for key, va, vb in examples[field][:SHOW]:
            print(f"    file_id={key[0]} seq={key[1]}")
            print(f"      before: {va!r}")
            print(f"      after:  {vb!r}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
