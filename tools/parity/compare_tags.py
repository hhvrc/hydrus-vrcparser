"""Diff the Python and C# per-file tag dumps.

This is the hard gate: anything other than "identical for every file" means the
port would push different tags than the implementation it replaces.
"""
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
            out[r["file_id"]] = r
    return out


def main():
    py, cs = load(PY), load(CS)

    only_py = sorted(set(py) - set(cs))
    only_cs = sorted(set(cs) - set(py))
    shared = sorted(set(py) & set(cs))

    if only_py or only_cs:
        print(f"KEY MISMATCH: {len(only_py)} only in python, {len(only_cs)} only in C#")
        for fid in only_py[:SHOW]:
            print(f"    only python: file_id={fid}")
        for fid in only_cs[:SHOW]:
            print(f"    only C#:     file_id={fid}")

    kinds = Counter()
    examples = {}
    tagged = 0

    for fid in shared:
        a, b = py[fid], cs[fid]
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

    print(f"compared {len(shared)} files ({tagged} with tags on the python side)")

    if not kinds and not only_py and not only_cs:
        print("\nTAG PARITY: identical for every file.")
        return 0

    for kind, count in kinds.most_common():
        print(f"\n  {kind}: {count}")
        for fid, a, b in examples[kind][:SHOW]:
            print(f"    file_id={fid}")
            if a["tags"] != b["tags"]:
                missing = sorted(set(a["tags"] or []) - set(b["tags"] or []))
                extra = sorted(set(b["tags"] or []) - set(a["tags"] or []))
                print(f"      only in python: {missing!r}")
                print(f"      only in C#:     {extra!r}")
            else:
                print(f"      py hash: {a['tag_hash']}")
                print(f"      cs hash: {b['tag_hash']}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
