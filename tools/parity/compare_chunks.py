"""Diff the Python and C# per-chunk parse dumps, field by field."""
import json
import sys
from collections import Counter

PY, CS = sys.argv[1], sys.argv[2]
SHOW = int(sys.argv[3]) if len(sys.argv) > 3 else 8


def load(path):
    out = {}
    with open(path, encoding="utf-8") as f:
        for line in f:
            r = json.loads(line)
            out[(r["file_id"], r["seq"])] = r
    return out


def main():
    py, cs = load(PY), load(CS)

    only_py = set(py) - set(cs)
    only_cs = set(cs) - set(py)
    if only_py or only_cs:
        print(f"KEY MISMATCH: {len(only_py)} only in python, {len(only_cs)} only in C#")

    fields = [
        "effective_type", "author_id", "author_name", "world_id", "world_name",
        "instance_id", "creator_tool", "editor_software", "created", "players", "error",
    ]

    diffs = Counter()
    examples = {}
    compared = 0

    for key in sorted(set(py) & set(cs)):
        a, b = py[key], cs[key]
        compared += 1
        for field in fields:
            va, vb = a.get(field), b.get(field)
            if va != vb:
                diffs[field] += 1
                examples.setdefault(field, []).append((key, va, vb))

    print(f"compared {compared} chunks across {len(fields)} fields")

    if not diffs:
        print("\nPARSER PARITY: identical on every field of every chunk.")
        return 0

    print(f"\nMISMATCHES in {len(diffs)} field(s):")
    for field, count in diffs.most_common():
        pct = 100.0 * count / compared
        print(f"\n  {field}: {count} ({pct:.2f}%)")
        for key, va, vb in examples[field][:SHOW]:
            print(f"    file_id={key[0]} seq={key[1]}")
            print(f"      py: {va!r}")
            print(f"      cs: {vb!r}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
